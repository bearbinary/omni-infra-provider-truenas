// Package meta contains meta information about the provider.
package meta

import (
	"regexp"
	"strings"
)

// ProviderID is the ID of the provider.
var ProviderID = "truenas"

// vmNameSafeRe masks any character that is not safe in a TrueNAS VM name.
// VM names in bhyve are restricted to alphanumerics, hyphens, and underscores —
// request IDs may contain hyphens (converted to underscores), provider IDs
// may contain dots or slashes (both mapped to underscore for safety).
var vmNameSafeRe = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// BuildVMName returns the deterministic TrueNAS VM name for a given provider
// instance and MachineRequest ID. Prefix the name with the provider ID so
// two providers sharing a TrueNAS host don't collide on VM names (and,
// downstream, don't race on deprovision).
//
// The format changed in v0.15.0 from `omni_<requestID>` to
// `omni_<providerID>_<requestID>` — operators upgrading from v0.14 need to
// accept that existing VMs will not be adopted or cleaned up automatically.
// See docs/upgrading.md for the recommended upgrade path.
func BuildVMName(providerID, requestID string) string {
	p := vmNameSafeRe.ReplaceAllString(providerID, "_")
	r := strings.ReplaceAll(requestID, "-", "_")
	r = vmNameSafeRe.ReplaceAllString(r, "_")

	name := "omni_" + p + "_" + r

	// Collapse runs of underscores post-concatenation so adjacent sanitized
	// runs across the provider/request-id boundary also collapse (e.g.,
	// empty providerID or one that sanitizes to trailing underscores).
	// Trim leading/trailing underscores too — a future exact-match check
	// shouldn't diverge based on whether punctuation was at the edges.
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}

	return strings.TrimSuffix(name, "_")
}

// IsOmniVMName returns true when name has the provider-managed prefix. Used
// by the cleanup scanner to recognize both the v0.14 legacy shape
// (`omni_<reqID>`) and the v0.15+ namespaced shape (`omni_<providerID>_<reqID>`).
// The prefix check is loose by design: cleanup reconciles against zvol
// ownership tags for the definitive is-it-ours answer.
func IsOmniVMName(name string) bool {
	return strings.HasPrefix(name, "omni_")
}

// ParseRequestIDFromDescription extracts the request-id suffix embedded in
// an Omni-managed VM description (the provider writes "Managed by Omni infra
// provider (request-id: <id>)"). Returns "" when the suffix is absent — e.g.,
// for legacy v0.14 VMs whose description was the bare prefix with no
// request-id suffix. Callers MUST treat an empty return as "unknown," not
// "no request id," and either skip the VM or fall back to another identifier.
//
// This is the authoritative path for reading the request-id back off a
// running VM. Deriving the request-id from the VM name
// (`strings.ReplaceAll(strings.TrimPrefix(name, "omni_"), "_", "-")`) silently
// miscomputes it on v0.15+ VMs because it leaves the `<providerID>_` segment
// in place — that was the root cause of cleanupOrphanVMs deleting
// freshly-provisioned VMs after the v0.15.0 VM-name namespacing change,
// fixed in v0.15.3.
func ParseRequestIDFromDescription(description string) string {
	const marker = "(request-id: "

	i := strings.Index(description, marker)
	if i < 0 {
		return ""
	}

	rest := description[i+len(marker):]

	j := strings.IndexByte(rest, ')')
	if j < 0 {
		return ""
	}

	return rest[:j]
}
