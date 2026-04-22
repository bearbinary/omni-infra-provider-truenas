package provisioner

import (
	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

// managedVM returns a client.VM that carries the Omni ownership marker so
// deprovision / adoption tests can exercise the happy path without repeating
// the description string at every call site. Use this helper in place of
// hand-rolling `client.VM{Description: omniVMDescriptionPrefix + " (test)", ...}`.
func managedVM(id int, state string) client.VM {
	return client.VM{
		ID:          id,
		Description: omniVMDescriptionPrefix + " (test)",
		Status:      client.VMStatus{State: state},
	}
}

// managedVMWithName is like managedVM but also sets Name — convenient for
// tests that exercise the FindVMByName path (cassette-replay mocks match on
// name filters).
func managedVMWithName(id int, name, state string) client.VM {
	vm := managedVM(id, state)
	vm.Name = name

	return vm
}

// managedVMPtr returns a pointer to a managedVM. Many call sites want the
// pointer form; avoid the extra &managedVM(...) indirection with this.
func managedVMPtr(id int, state string) *client.VM {
	vm := managedVM(id, state)

	return &vm
}

// managedZvolQueryResult returns the shape of a pool.dataset.query reply
// for an Omni-managed zvol: carries both the managed=true tag and the
// request-id tag matching the argument. Useful whenever a mock handler
// needs to answer the deprovision ownership check.
func managedZvolQueryResult(requestID string) map[string]any {
	return map[string]any{
		"user_properties": map[string]any{
			"org.omni:managed":    map[string]any{"value": "true"},
			"org.omni:request-id": map[string]any{"value": requestID},
		},
	}
}
