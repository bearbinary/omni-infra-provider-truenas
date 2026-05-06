package provisioner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

// tofuResult is the decision outcome of comparing a freshly-downloaded ISO's
// hash against the trust-on-first-use value stored as a ZFS user property.
type tofuResult int

const (
	// tofuFirstUse: no stored hash yet. The caller should record the
	// downloaded hash and proceed.
	tofuFirstUse tofuResult = iota

	// tofuMatch: stored hash equals downloaded hash. Proceed normally.
	tofuMatch

	// tofuMismatch: stored hash disagrees with downloaded hash. The caller
	// should POISON-mark the stored value and fail the provision.
	tofuMismatch

	// tofuPoisoned: the stored hash was already POISON-marked by a prior
	// mismatch. Fail immediately; do not upload.
	tofuPoisoned
)

// poisonedPrefix marks a stored hash as tainted by a prior mismatch.
// Kept as a package-level const so tests can reference the exact shape.
const poisonedPrefix = "POISONED-"

// classifyTOFU maps a stored/downloaded pair to the TOFU decision outcome.
// Extracted from stepUploadISO so the decision table is unit-testable
// without a working HTTP server and TrueNAS mock.
func classifyTOFU(storedHash, downloadedHash string) tofuResult {
	if strings.HasPrefix(storedHash, poisonedPrefix) {
		return tofuPoisoned
	}

	if storedHash == "" {
		return tofuFirstUse
	}

	if storedHash == downloadedHash {
		return tofuMatch
	}

	return tofuMismatch
}

// cachedISOPoisoned returns true iff the stored hash indicates the cached
// ISO was tainted by a prior mismatch. Used on cache hits when no fresh
// download has happened — we only read, not compare.
func cachedISOPoisoned(storedHash string) bool {
	return strings.HasPrefix(storedHash, poisonedPrefix)
}

// poisonMarker formats a POISON-tagged stored value carrying the mismatched
// hash that triggered the poison. Exposed so tests can assert on format.
func poisonMarker(badHash string) string {
	return poisonedPrefix + badHash
}

// ZFS user-property keys for the TOFU baseline, parameterized per-imageID.
// Hash is the long-standing supply-chain check; size and mtime were added so
// a cache-hit can detect a post-baseline byte swap without paying the cost
// of a full re-hash (TrueNAS exposes no streaming-hash RPC, so a re-hash
// would mean pulling the bytes back off TrueNAS just to re-verify them).
//
// Stat-based detection catches:
//   - replication / snapshot restore that touches mtime
//   - in-place edits that change size
//   - a separate workload writing to the dataset
//   - casual swaps that don't bother to preserve metadata
//
// It does NOT catch an attacker with TrueNAS write access who can rewrite
// BOTH the bytes and the user properties. The bytes and the properties
// live behind the same trust boundary — pool.dataset.update accepts both —
// so byte tamper and property tamper are indistinguishable to such an
// attacker. They can substitute bytes and in the same RPC sequence rewrite
// hash + size + mtime to match the new bytes; the next provision then
// happily reuses them.
//
// The defense at this layer is therefore meaningful only against actors
// who can write the file but not the properties (a misconfigured share, a
// foreign workload), or against non-malicious metadata drift. For the
// "TrueNAS admin / API key compromise" threat model, the only true
// mitigation is to store the TOFU triple in a sink the provider's API key
// has no write access to — tracked as future work in docs/backlog.md.
// docs/hardening.md spells this trade-off out for operators who turn the
// flag on.
const (
	isoHashPropertyPrefix  = "org.omni:iso-sha256-"
	isoSizePropertyPrefix  = "org.omni:iso-size-"
	isoMtimePropertyPrefix = "org.omni:iso-mtime-"
)

func isoHashProperty(imageID string) string  { return isoHashPropertyPrefix + imageID }
func isoSizeProperty(imageID string) string  { return isoSizePropertyPrefix + imageID }
func isoMtimeProperty(imageID string) string { return isoMtimePropertyPrefix + imageID }

// formatISOMtime preserves TrueNAS's sub-second precision by serializing
// at -1 ('shortest unique') so a stored value round-trips bit-for-bit
// through ParseFloat. A bare %g or %f would round-trip with extra precision
// and miscompare against the original value. The size pair was previously
// wrapped here too, but a passthrough to strconv.FormatInt/ParseInt added
// no semantic content — those are inlined at call sites.
func formatISOMtime(mtime float64) string {
	return strconv.FormatFloat(mtime, 'f', -1, 64)
}

func parseISOMtime(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// verifyISOMetadata compares stored TOFU metadata against the freshly-stat'd
// on-disk values for a cached ISO. Returns:
//   - nil if metadata matches (caller proceeds with cache hit)
//   - a tofuResult of tofuFirstUse if no metadata was recorded yet (legacy
//     ISO from before stat-based detection landed; caller should record now)
//   - tofuMismatch if size or mtime drifted (caller poison-marks + fails)
//
// Intentionally pure: no RPC, no logger. Drives a unit-test decision table.
func verifyISOMetadata(storedSize, storedMtime string, actual *client.FileInfo) (tofuResult, error) {
	if storedSize == "" && storedMtime == "" {
		// Pre-existing ISO from a provider version that recorded only the
		// hash. Caller should treat as first-use for the metadata fields
		// and record fresh values now.
		return tofuFirstUse, nil
	}

	if actual == nil {
		// Stat returned no info but the file existed at the cache-hit check.
		// A race occurred (file deleted between FileExists and StatFile) —
		// treat as mismatch rather than silently accepting "unknown".
		return tofuMismatch, fmt.Errorf("on-disk ISO disappeared between cache-hit check and stat — treating as tampered")
	}

	wantSize, err := strconv.ParseInt(storedSize, 10, 64)
	if err != nil {
		return tofuMismatch, fmt.Errorf("recorded ISO size %q is not a valid int64: %w", storedSize, err)
	}

	if wantSize != actual.Size {
		return tofuMismatch, fmt.Errorf("ISO size drift: recorded %d bytes, on-disk %d bytes", wantSize, actual.Size)
	}

	wantMtime, err := parseISOMtime(storedMtime)
	if err != nil {
		return tofuMismatch, fmt.Errorf("recorded ISO mtime %q is not a valid float64: %w", storedMtime, err)
	}

	// Floating-point equality is correct here: both values flow through the
	// same FormatFloat -1 / ParseFloat round-trip, which is bit-stable.
	if wantMtime != actual.Mtime {
		return tofuMismatch, fmt.Errorf("ISO mtime drift: recorded %s, on-disk %s",
			formatISOMtime(wantMtime), formatISOMtime(actual.Mtime))
	}

	return tofuMatch, nil
}

// poisonSetter is the minimal slice of *client.Client that setIfPoisonable
// and recordTOFUProperty need — accepts an interface so a unit test can
// drop in a recording fake without standing up a JSON-RPC mock for one
// method.
type poisonSetter interface {
	SetDatasetUserProperty(ctx context.Context, path, key, value string) error
}

// verifyCachedISO is called on a cache hit (filesystem.stat returned a
// non-nil FileInfo). It reads the TOFU triple, classifies the on-disk
// bytes against the recorded baseline, and returns nil iff the cached
// file is safe to reuse. Side effects:
//
//   - Property-read RPC failures abort the provision (the captured-error
//     fix from the original SAST report — refusing to silently degrade
//     to "trust whatever is on disk").
//   - On a metadata-drift mismatch, best-effort POISON-marks the recorded
//     hash via setIfPoisonable so a future provision refuses to reuse the
//     bytes. The Bool the helper returns flows into the outer error so
//     operators see "POISON marker NOT persisted" if the marker write
//     also failed.
//   - On a tofuFirstUse classification (legacy ISO from a provider
//     version before stat-detection landed — hash recorded, size+mtime
//     missing) the helper records the live size/mtime now. This upgrades
//     the cache entry to the full TOFU triple without forcing a
//     re-download, so the next cache hit gets metadata-drift detection.
//
// Extracted from stepUploadISO so the cache-hit branch reads as a single
// call and the four-arm switch on tofuResult lives next to the helper
// that produces it. The previous inline form was 80 lines and 4 levels
// of nesting in the middle of stepUploadISO.
func (p *Provisioner) verifyCachedISO(
	ctx context.Context,
	logger *zap.Logger,
	isoDataset, isoPath, imageID string,
	hashProp, sizeProp, mtimeProp string,
	stat *client.FileInfo,
) error {
	props, err := p.client.GetDatasetUserProperties(ctx, isoDataset)
	if err != nil {
		return fmt.Errorf("failed to read TOFU baseline for cached ISO %s: %w — refusing to reuse cached bytes without verification", imageID, err)
	}

	storedHash := props[hashProp]
	if cachedISOPoisoned(storedHash) {
		return fmt.Errorf("ISO %s is marked POISONED from a prior factory-compromise detection — delete %s on TrueNAS and retry (see docs/hardening.md)", imageID, isoPath)
	}

	if storedHash == "" {
		// Pre-TOFU ISO from a provider version before the supply-chain
		// hash was recorded at all. Proceed (matches prior behaviour);
		// the next download for this imageID will establish the baseline.
		return nil
	}

	result, vErr := verifyISOMetadata(props[sizeProp], props[mtimeProp], stat)
	switch result {
	case tofuMatch:
		return nil

	case tofuFirstUse:
		// Legacy entry: hash present, size/mtime missing. Record them
		// now using the live stat so the next cache hit gets the full
		// triple. Best-effort: a write failure is logged + countered
		// inside recordTOFUProperty but does not block the provision.
		recordTOFUProperty(ctx, logger, p.client, isoDataset, sizeProp, strconv.FormatInt(stat.Size, 10), "size", imageID)
		recordTOFUProperty(ctx, logger, p.client, isoDataset, mtimeProp, formatISOMtime(stat.Mtime), "mtime", imageID)
		return nil

	case tofuMismatch:
		persisted := setIfPoisonable(ctx, logger, p.client, isoDataset, hashProp, poisonMarker(storedHash), imageID, isoPath)

		if telemetry.ISOHashMismatches != nil {
			telemetry.ISOHashMismatches.Add(ctx, 1, metric.WithAttributes(attribute.String("detection_path", "cache_hit_metadata")))
		}

		logger.Error("cached ISO metadata drift — possible at-rest tamper",
			zap.String("image_id", imageID),
			zap.String("iso_path", isoPath),
			zap.Bool("poison_marker_persisted", persisted),
			zap.Error(vErr),
		)

		msg := "cached ISO %s failed metadata re-verification: %w — delete %s on TrueNAS and retry"
		if !persisted {
			msg += " (POISON marker NOT persisted; on-disk bytes still trusted by next run)"
		}

		return fmt.Errorf(msg, imageID, vErr, isoPath)
	}

	// tofuPoisoned would only be reachable if classifyTOFU's poison
	// detection drifted from cachedISOPoisoned's; both share poisonedPrefix
	// today so this is dead code, included only to keep the switch
	// exhaustive against future tofuResult additions.
	return fmt.Errorf("verifyCachedISO: unexpected tofuResult %d for ISO %s", result, imageID)
}

// reverifyISOBeforeAttach is the TOCTOU defense between cache-hit
// verification and CDROM attach. The cache-hit check ran at upload-step
// time; bytes can change before the VM actually boots — replication,
// snapshot rollback, a foreign workload writing to the dataset, or an
// attacker racing the provisioner. Re-stat and re-compare here so a
// drift POISON-marks the hash and aborts the create rather than
// silently booting tampered bytes.
//
// If the file is missing or the baseline was never recorded (legacy
// ISO), this is a no-op — verifyCachedISO already classifies those as
// firstUse / not-found and the caller already trusted them. The defense
// is specifically against tamper between two trusted snapshots.
func (p *Provisioner) reverifyISOBeforeAttach(ctx context.Context, logger *zap.Logger, isoDataset, isoPath, imageID string) error {
	stat, err := p.client.StatFile(ctx, isoPath)
	if err != nil {
		return fmt.Errorf("failed to re-stat ISO %s before CDROM attach: %w", isoPath, err)
	}

	if stat == nil {
		// File disappeared between upload and attach. Refuse to attach
		// a CDROM to a path that no longer exists — boot would fail
		// downstream with a confusing error.
		return fmt.Errorf("ISO %s vanished between upload and CDROM attach — refusing to attach a missing path", isoPath)
	}

	return p.verifyCachedISO(ctx, logger, isoDataset, isoPath, imageID,
		isoHashProperty(imageID), isoSizeProperty(imageID), isoMtimeProperty(imageID), stat)
}

// recordTOFUProperty writes one of the three TOFU companion properties
// (hash / size / mtime) and surfaces a failure as a warn-level log + a
// labelled telemetry counter increment. Centralizing this turns three
// near-identical try-and-warn blocks at the upload site into a single
// call site each, and gives operators a metric (rather than a log
// scrape) to alert on a degrading property RPC.
func recordTOFUProperty(ctx context.Context, logger *zap.Logger, c poisonSetter, dataset, key, value, field, imageID string) {
	if err := c.SetDatasetUserProperty(ctx, dataset, key, value); err != nil {
		logger.Warn("failed to record ISO TOFU companion property — cache-hit re-verification will degrade for this field",
			zap.String("image_id", imageID),
			zap.String("field", field),
			zap.Error(err),
		)

		if telemetry.ISOTOFUMetadataWriteFailed != nil {
			telemetry.ISOTOFUMetadataWriteFailed.Add(ctx, 1, metric.WithAttributes(attribute.String("property", field)))
		}
	}
}

// poisonRetryAttempts is the number of times setIfPoisonable will retry the
// SetDatasetUserProperty RPC before giving up. The previous implementation
// fired-and-forgot a single write — a transient WebSocket reconnect at the
// wrong moment left the bad bytes on disk with the *trusted* baseline still
// in place, defeating the whole supply-chain check. Three attempts with
// short fixed backoff covers the typical 1–2s reconnect window without
// stalling the provisioner thread for long.
const poisonRetryAttempts = 3
const poisonRetryDelay = 500 * time.Millisecond

// setIfPoisonable persists the POISON marker for a TOFU mismatch with
// retries. On final failure it logs MANUAL CLEANUP REQUIRED at error level
// with the exact path so an operator alert can fire on the message and an
// on-call human knows what to delete by hand. Reports whether the marker
// was successfully persisted so the caller can mention an unflushed marker
// in its outer error wrap (otherwise the secondary failure mode is only
// visible via log scraping).
//
// Telemetry: increments ISOPoisonMarkerRetries on each failed attempt so
// operators can graph a degrading property RPC before the retries exhaust;
// increments ISOPoisonMarkerWriteFailed exactly once if every attempt
// fails, giving alerting a metric to fire on rather than a log-string match.
func setIfPoisonable(ctx context.Context, logger *zap.Logger, c poisonSetter, dataset, key, marker, imageID, isoPath string) (persisted bool) {
	var lastErr error

	// Hoist the timer outside the loop so a `time.After` per iteration does
	// not leak a *time.Timer the runtime cannot collect until it fires. Tiny
	// gain on this cold path, but the pattern is the right reflex.
	timer := time.NewTimer(poisonRetryDelay)
	defer timer.Stop()
	if !timer.Stop() {
		// Drain so the first Reset gets a fresh deadline.
		select {
		case <-timer.C:
		default:
		}
	}

retry:
	for attempt := 1; attempt <= poisonRetryAttempts; attempt++ {
		err := c.SetDatasetUserProperty(ctx, dataset, key, marker)
		if err == nil {
			return true
		}

		lastErr = err

		if telemetry.ISOPoisonMarkerRetries != nil {
			telemetry.ISOPoisonMarkerRetries.Add(ctx, 1)
		}

		// Bail out early if the caller's context died — no point hammering
		// a property write the request is already cancelling.
		if ctx.Err() != nil {
			lastErr = ctx.Err()
			break
		}

		if attempt < poisonRetryAttempts {
			timer.Reset(poisonRetryDelay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				lastErr = ctx.Err()
				if !timer.Stop() {
					<-timer.C
				}
				break retry
			}
		}
	}

	if telemetry.ISOPoisonMarkerWriteFailed != nil {
		telemetry.ISOPoisonMarkerWriteFailed.Add(ctx, 1)
	}

	logger.Error("MANUAL CLEANUP REQUIRED — failed to persist ISO POISON marker after retries; the on-disk bytes are confirmed bad and remain accessible. Delete the file from TrueNAS before re-provisioning.",
		zap.String("image_id", imageID),
		zap.String("iso_path", isoPath),
		zap.String("dataset", dataset),
		zap.String("property", key),
		zap.Error(lastErr),
	)

	return false
}
