package provisioner

import (
	"context"
	"fmt"
	"strings"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

// omniVMDescriptionPrefix is the prefix applied to every Omni-managed VM's
// `Description` field. Used to distinguish provider-created VMs from look-alikes
// that happen to share a name (manually created, second provider instance,
// stale state). Historical VMs created before request-id was embedded still
// pass the prefix check because the prefix itself is unchanged.
const omniVMDescriptionPrefix = "Managed by Omni infra provider"

// omniVMDescription returns the description string to set on a newly created
// VM. Embeds the request ID so ownership can be traced end-to-end (VM → zvol
// user property → Omni MachineRequest).
func omniVMDescription(requestID string) string {
	return fmt.Sprintf("%s (request-id: %s)", omniVMDescriptionPrefix, requestID)
}

// isOmniManagedVM returns true if the VM carries the provider's management
// marker in its description. VMs without the marker must not be deleted or
// adopted — they belong to someone else.
func isOmniManagedVM(vm *client.VM) bool {
	if vm == nil {
		return false
	}

	return strings.HasPrefix(vm.Description, omniVMDescriptionPrefix)
}


// verifyZvolOwnership reads the ownership ZFS user properties from a zvol and
// returns nil iff the zvol is tagged as Omni-managed and (when requestID is
// non-empty) its recorded request-id matches. Used by the deprovision path to
// refuse deletion of look-alike datasets.
//
// Missing properties (empty string) are treated as "not managed by us" and
// cause the check to fail. Zvols created before the tagging feature (pre
// v0.10.0) will fail this check — operators upgrading from those versions
// must set `org.omni:managed=true` manually or delete the zvols from TrueNAS.
//
// Issues a single pool.dataset.query to fetch both ownership properties
// at once. An earlier implementation did two separate GetDatasetUserProperty
// calls — unnecessary RPC amplification in a deprovision loop that can run
// across dozens of VMs.
func verifyZvolOwnership(ctx context.Context, c *client.Client, zvolPath, requestID string) error {
	if zvolPath == "" {
		return fmt.Errorf("zvol path is empty")
	}

	props, err := c.GetDatasetUserProperties(ctx, zvolPath)
	if err != nil {
		return fmt.Errorf("failed to read ownership properties on %q: %w", zvolPath, err)
	}

	if managed := props["org.omni:managed"]; managed != "true" {
		return fmt.Errorf("zvol %q is not tagged org.omni:managed=true (got %q) — refusing to delete", zvolPath, managed)
	}

	if requestID == "" {
		return nil
	}

	if storedID := props["org.omni:request-id"]; storedID != "" && storedID != requestID {
		return fmt.Errorf("zvol %q has request-id %q, expected %q — refusing to delete", zvolPath, storedID, requestID)
	}

	return nil
}
