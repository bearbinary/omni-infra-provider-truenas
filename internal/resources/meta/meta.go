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
