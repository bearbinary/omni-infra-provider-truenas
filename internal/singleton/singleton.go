// Package singleton prevents multiple instances of the TrueNAS infra provider
// from running concurrently with the same PROVIDER_ID. It uses metadata
// annotations on the infra.ProviderStatus resource as a distributed lease
// backed by Omni's optimistic-concurrency state.
//
// The Omni SDK's ProvisionController has no built-in leader election: every
// process that registers with the same provider ID receives every
// MachineRequest and races on the provisioning side-effects (VM create, zvol
// create, ISO upload). The singleton lease is how we keep "exactly one
// active provisioner per provider ID".
package singleton

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/google/uuid"
	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"go.uber.org/zap"
)

// Annotation keys written on infra.ProviderStatus metadata.
const (
	AnnotationInstanceID = "bearbinary.com/singleton-instance-id"
	AnnotationHeartbeat  = "bearbinary.com/singleton-heartbeat"
)

// Default tuning values.
const (
	DefaultRefreshInterval = 15 * time.Second
	DefaultStaleAfter      = 45 * time.Second

	// maxConsecutiveRefreshErrors is how many refresh failures in a row will
	// trigger the loss signal. Kept small so a persistently broken gRPC
	// connection surfaces quickly rather than masking duplicate-instance bugs.
	maxConsecutiveRefreshErrors = 3
)

// ErrLeaseHeld indicates another instance currently holds the lease with a
// fresh heartbeat. Callers should wrap or inspect via errors.As for details.
var ErrLeaseHeld = errors.New("another provider instance holds the singleton lease")

// LeaseHeldError is returned when another instance holds a fresh lease.
type LeaseHeldError struct {
	ProviderID      string
	OtherInstanceID string
	HeartbeatAt     time.Time
	HeartbeatAge    time.Duration
}

func (e *LeaseHeldError) Error() string {
	return fmt.Sprintf(
		"provider %q singleton lease is held by instance %q (heartbeat %s ago at %s) — "+
			"either stop the other instance, wait for its heartbeat to go stale, or set PROVIDER_SINGLETON_ENABLED=false to bypass",
		e.ProviderID, e.OtherInstanceID, e.HeartbeatAge.Round(time.Second), e.HeartbeatAt.Format(time.RFC3339),
	)
}

func (e *LeaseHeldError) Is(target error) bool {
	return target == ErrLeaseHeld
}

// Clock is the time source used by a Lease. Tests override this.
type Clock interface {
	Now() time.Time
}

type wallclock struct{}

func (wallclock) Now() time.Time { return time.Now().UTC() }

// Config configures a Lease.
type Config struct {
	// ProviderID is the infra provider ID (shared across all instances with
	// the same role).
	ProviderID string
	// InstanceID uniquely identifies this process. If empty, a UUID v7 is
	// generated.
	InstanceID string
	// RefreshInterval is how often the heartbeat is rewritten. If zero, falls
	// back to DefaultRefreshInterval.
	RefreshInterval time.Duration
	// StaleAfter is the age at which an existing lease is considered expired
	// and eligible for takeover. Must be >= 2 * RefreshInterval to tolerate
	// one missed tick plus clock skew.
	StaleAfter time.Duration
	// Clock is the time source. If nil, wallclock is used.
	Clock Clock
	// OnRefreshError is called each time a heartbeat refresh fails. Optional.
	OnRefreshError func()
}

// Lease manages an exclusive lease over infra.ProviderStatus metadata
// annotations for a given ProviderID.
type Lease struct {
	cfg    Config
	st     state.State
	logger *zap.Logger

	lostCh      chan struct{}
	lostOnce    sync.Once
	wasTakeover bool // set when lease was acquired from a stale/unclaimed holder
}

// New builds a Lease. It does not contact the state — call Acquire first.
func New(st state.State, cfg Config, logger *zap.Logger) (*Lease, error) {
	if cfg.ProviderID == "" {
		return nil, errors.New("ProviderID is required")
	}

	if cfg.InstanceID == "" {
		cfg.InstanceID = uuid.Must(uuid.NewV7()).String()
	}

	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = DefaultRefreshInterval
	}

	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = DefaultStaleAfter
	}

	if cfg.StaleAfter < 2*cfg.RefreshInterval {
		return nil, fmt.Errorf("StaleAfter (%s) must be >= 2 * RefreshInterval (%s)", cfg.StaleAfter, cfg.RefreshInterval)
	}

	if cfg.Clock == nil {
		cfg.Clock = wallclock{}
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &Lease{
		cfg:    cfg,
		st:     st,
		logger: logger.With(zap.String("singleton_instance_id", cfg.InstanceID)),
		lostCh: make(chan struct{}),
	}, nil
}

// InstanceID returns this lease holder's unique instance ID.
func (l *Lease) InstanceID() string { return l.cfg.InstanceID }

// WasTakeover returns true if this lease was acquired by taking over from a
// stale or unclaimed previous holder (not a fresh create or re-entrant acquire).
func (l *Lease) WasTakeover() bool { return l.wasTakeover }

// Lost returns a channel that is closed once the lease is definitively lost
// (stolen, abandoned, or unreachable). Callers should cancel their root
// context to initiate shutdown when this channel closes.
func (l *Lease) Lost() <-chan struct{} { return l.lostCh }

// Acquire attempts to claim the lease once. Returns a *LeaseHeldError (wraps
// ErrLeaseHeld) if another instance currently owns it with a fresh heartbeat.
//
// The call is idempotent: acquiring a lease we already own bumps the
// heartbeat and returns nil.
func (l *Lease) Acquire(ctx context.Context) error {
	// Small retry loop to handle the narrow race between a "not found" Get
	// and a subsequent racing Create from another instance.
	const maxAttempts = 3

	for attempt := 0; attempt < maxAttempts; attempt++ {
		_, err := l.st.Get(ctx, infra.NewProviderStatus(l.cfg.ProviderID).Metadata())

		switch {
		case state.IsNotFoundError(err):
			created, createErr := l.tryCreate(ctx)
			if createErr != nil {
				return createErr
			}

			if created {
				return nil
			}
			// Raced with another Create — fall through to CAS-update path.
		case err != nil:
			return fmt.Errorf("failed to read ProviderStatus: %w", err)
		}

		heldErr, updateErr := l.casAcquire(ctx)
		if heldErr != nil {
			return heldErr
		}

		if updateErr == nil {
			return nil
		}

		if state.IsNotFoundError(updateErr) {
			// Deleted between Get and Update — loop and re-create.
			continue
		}

		return fmt.Errorf("failed to acquire lease: %w", updateErr)
	}

	return fmt.Errorf("failed to acquire singleton lease after %d attempts", maxAttempts)
}

// tryCreate attempts to Create the ProviderStatus resource with our lease
// annotations. Returns (true, nil) on success, (false, nil) if another process
// raced us and Created first, or (false, err) on any other failure.
func (l *Lease) tryCreate(ctx context.Context) (bool, error) {
	status := infra.NewProviderStatus(l.cfg.ProviderID)
	now := l.cfg.Clock.Now().UTC()
	status.Metadata().Annotations().Set(AnnotationInstanceID, l.cfg.InstanceID)
	status.Metadata().Annotations().Set(AnnotationHeartbeat, now.Format(time.RFC3339Nano))

	if err := l.st.Create(ctx, status); err != nil {
		if state.IsConflictError(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to create ProviderStatus: %w", err)
	}

	l.logger.Info("acquired singleton lease (created ProviderStatus)",
		zap.String("provider_id", l.cfg.ProviderID),
	)

	return true, nil
}

// casAcquire runs a CAS-update against an existing ProviderStatus to claim or
// refresh the lease. Returns (heldErr, nil) when another instance holds a
// fresh lease, (nil, err) for any other failure, or (nil, nil) on success.
func (l *Lease) casAcquire(ctx context.Context) (*LeaseHeldError, error) {
	var heldErr *LeaseHeldError

	_, err := safe.StateUpdateWithConflicts(ctx, l.st,
		infra.NewProviderStatus(l.cfg.ProviderID).Metadata(),
		func(res *infra.ProviderStatus) error {
			ownerID, hasOwner := res.Metadata().Annotations().Get(AnnotationInstanceID)
			heartbeatStr, hasHeartbeat := res.Metadata().Annotations().Get(AnnotationHeartbeat)
			now := l.cfg.Clock.Now().UTC()

			switch {
			case !hasOwner || !hasHeartbeat:
				l.wasTakeover = true

				l.logger.Info("acquired singleton lease (previously unclaimed)",
					zap.String("provider_id", l.cfg.ProviderID),
				)
			case ownerID == l.cfg.InstanceID:
				// Re-entrant acquire (or refresh). No log spam.
			default:
				heartbeat, parseErr := time.Parse(time.RFC3339Nano, heartbeatStr)
				if parseErr != nil {
					l.wasTakeover = true

					l.logger.Warn("existing lease heartbeat is malformed, taking over",
						zap.String("prior_instance_id", ownerID),
						zap.String("heartbeat", heartbeatStr),
						zap.Error(parseErr),
					)

					break
				}

				age := now.Sub(heartbeat)
				if age < l.cfg.StaleAfter {
					heldErr = &LeaseHeldError{
						ProviderID:      l.cfg.ProviderID,
						OtherInstanceID: ownerID,
						HeartbeatAt:     heartbeat,
						HeartbeatAge:    age,
					}

					return heldErr
				}

				l.wasTakeover = true

				l.logger.Warn("taking over stale singleton lease",
					zap.String("prior_instance_id", ownerID),
					zap.Duration("heartbeat_age", age),
				)
			}

			res.Metadata().Annotations().Set(AnnotationInstanceID, l.cfg.InstanceID)
			res.Metadata().Annotations().Set(AnnotationHeartbeat, now.Format(time.RFC3339Nano))

			return nil
		})

	if heldErr != nil {
		return heldErr, nil
	}

	return nil, err
}

// Run drives the heartbeat refresh loop. It blocks until ctx is done or the
// lease is lost, at which point the Lost() channel is closed and Run returns.
//
// A lost lease can happen three ways:
//  1. Another instance stole the lease (different instance-id observed on read-back).
//  2. maxConsecutiveRefreshErrors refresh calls failed in a row.
//  3. The caller cancelled ctx (clean shutdown, not a loss — Lost() is NOT closed).
func (l *Lease) Run(ctx context.Context) error {
	ticker := time.NewTicker(l.cfg.RefreshInterval)
	defer ticker.Stop()

	var consecutiveErrors int

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		stolen, err := l.refresh(ctx)
		if stolen {
			l.logger.Error("singleton lease stolen by another instance, initiating shutdown")
			l.markLost()

			return ErrLeaseHeld
		}

		if err != nil {
			consecutiveErrors++

			if l.cfg.OnRefreshError != nil {
				l.cfg.OnRefreshError()
			}

			l.logger.Warn("singleton lease refresh failed",
				zap.Int("consecutive_errors", consecutiveErrors),
				zap.Int("threshold", maxConsecutiveRefreshErrors),
				zap.Error(err),
			)

			if consecutiveErrors >= maxConsecutiveRefreshErrors {
				l.logger.Error("singleton lease refresh failed repeatedly, initiating shutdown")
				l.markLost()

				return fmt.Errorf("singleton lease refresh failed after %d attempts: %w", consecutiveErrors, err)
			}

			continue
		}

		consecutiveErrors = 0
	}
}

// refresh writes a fresh heartbeat and checks that we still own the lease.
// Returns (stolen=true, nil) if another instance-id is on the resource,
// (false, err) on any other failure, or (false, nil) on success.
func (l *Lease) refresh(ctx context.Context) (bool, error) {
	var stolen bool

	_, err := safe.StateUpdateWithConflicts(ctx, l.st,
		infra.NewProviderStatus(l.cfg.ProviderID).Metadata(),
		func(res *infra.ProviderStatus) error {
			ownerID, hasOwner := res.Metadata().Annotations().Get(AnnotationInstanceID)
			if hasOwner && ownerID != l.cfg.InstanceID {
				stolen = true

				return fmt.Errorf("lease stolen by instance %q", ownerID)
			}

			now := l.cfg.Clock.Now().UTC()
			res.Metadata().Annotations().Set(AnnotationInstanceID, l.cfg.InstanceID)
			res.Metadata().Annotations().Set(AnnotationHeartbeat, now.Format(time.RFC3339Nano))

			return nil
		})

	if stolen {
		return true, nil
	}

	if err != nil {
		return false, err
	}

	return false, nil
}

// Release clears our lease annotations so a successor can take over without
// waiting for the heartbeat to go stale. Best-effort: safe to call on an
// already-shutdown state and logs (but does not fail) if the resource no
// longer exists or someone else has already taken over.
func (l *Lease) Release(ctx context.Context) {
	_, err := safe.StateUpdateWithConflicts(ctx, l.st,
		infra.NewProviderStatus(l.cfg.ProviderID).Metadata(),
		func(res *infra.ProviderStatus) error {
			ownerID, ok := res.Metadata().Annotations().Get(AnnotationInstanceID)
			if !ok || ownerID != l.cfg.InstanceID {
				// Not ours — leave it alone.
				return nil
			}

			res.Metadata().Annotations().Delete(AnnotationInstanceID)
			res.Metadata().Annotations().Delete(AnnotationHeartbeat)

			return nil
		})
	if err != nil {
		if state.IsNotFoundError(err) {
			return
		}

		l.logger.Warn("failed to release singleton lease on shutdown — successor will wait for staleAfter",
			zap.Error(err),
		)

		return
	}

	l.logger.Info("released singleton lease")
}

func (l *Lease) markLost() {
	l.lostOnce.Do(func() { close(l.lostCh) })
}
