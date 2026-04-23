package autoscaler

import (
	"context"
	"errors"
	"fmt"

	truenasclient "github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

// TrueNASCapacityAdapter wraps an *internal/client.Client so it satisfies
// CapacityQuery. Lives in this package (not internal/client) because the
// interface is an autoscaler-specific concern — the provisioner path
// calls ListPools / GetHostInfo directly and doesn't need this shape.
//
// Keep the adapter thin: any logic (thresholds, rate-limiting, caching)
// lives elsewhere. This type exists solely to translate between
// internal/client's return shapes and the CapacityQuery interface, so a
// future extraction of internal/autoscaler into its own repo can swap
// in a different infra source by implementing CapacityQuery without
// touching the gate code.
type TrueNASCapacityAdapter struct {
	client *truenasclient.Client
}

// NewTrueNASCapacityAdapter constructs a CapacityQuery backed by an
// existing TrueNAS client. The client must be authenticated — the
// adapter does not handle login.
func NewTrueNASCapacityAdapter(c *truenasclient.Client) *TrueNASCapacityAdapter {
	return &TrueNASCapacityAdapter{client: c}
}

// PoolFreeBytes returns the root-dataset "available" bytes on the named
// pool — matches what TrueNAS displays in its UI and accounts for ZFS
// parity, metadata, and reservation overhead. A bare `pool.query.size`
// would over-report by the ZFS overhead amount and let the gate allow
// scale-ups that actually can't fit.
//
// Returns an ErrPoolNotFound-class error when the pool doesn't exist on
// the TrueNAS host — propagated to the caller as OutcomeErrored so
// operators see a clear misconfiguration message rather than a silent
// always-deny.
func (a *TrueNASCapacityAdapter) PoolFreeBytes(ctx context.Context, pool string) (int64, error) {
	pools, err := a.client.ListPools(ctx)
	if err != nil {
		return 0, fmt.Errorf("list pools: %w", err)
	}

	for _, p := range pools {
		if p.Name == pool {
			return p.Free, nil
		}
	}

	return 0, fmt.Errorf("pool %q: not found on TrueNAS host", pool)
}

// HostFreeMemoryBytes is not yet wired against TrueNAS 25.10 — no
// `system.mem_info` wrapper exists in internal/client, and deriving
// free memory from `system.info.physmem` minus per-VM reservations
// double-counts balloon memory and ZFS ARC. Returns
// ErrHostMemNotImplemented so the gate treats the check as errored
// (fail-closed) rather than silently skipping it.
//
// Operators who want to deploy the autoscaler before the memory
// wrapper lands can disable the host-mem check by setting
// `bearbinary.com/autoscale-min-host-mem-gib: "0"` on the MachineClass.
// The pool-free check is unaffected.
//
// Tracked as the next internal/client addition; a real implementation
// will land together with a contract cassette and a method-allowlist
// entry for the new RPC.
func (a *TrueNASCapacityAdapter) HostFreeMemoryBytes(_ context.Context) (int64, error) {
	return 0, ErrHostMemNotImplemented
}

// ErrHostMemNotImplemented signals the memory gate's production
// dependency is still pending. Exported so callers (and tests) can
// errors.Is against it and provide a specific operator-facing message
// ("set autoscale-min-host-mem-gib to 0 to disable until the memory
// wrapper lands") rather than parsing the generic "capacity query
// failed" string.
var ErrHostMemNotImplemented = errors.New("host free memory query not implemented yet — set bearbinary.com/autoscale-min-host-mem-gib=0 to disable this check until the TrueNAS system.mem_info wrapper lands")
