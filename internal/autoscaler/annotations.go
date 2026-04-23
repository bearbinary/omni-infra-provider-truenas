// Package autoscaler implements the experimental cluster-autoscaler external
// gRPC cloud-provider interface for Omni. Ships as a subcommand of
// omni-infra-provider-truenas so operators can deploy it alongside an
// existing provider without running a second binary.
//
// Status: EXPERIMENTAL. The annotation schema in this package and the gRPC
// surface are subject to breaking changes until the feature is promoted
// to stable. See docs/autoscaler.md for current scope and known gaps.
//
// Design notes:
//   - Opt-in per MachineClass via `bearbinary.com/autoscale-*` annotations.
//     A MachineClass without both the min and max annotations is simply
//     not discovered as a node group — zero blast radius for clusters that
//     don't opt in.
//   - Package intentionally depends on nothing inside internal/provisioner
//     or internal/client except through narrow interfaces. A future
//     `git subtree split --prefix=internal/autoscaler` should yield a
//     standalone repo if the feature gains traction outside TrueNAS or
//     Sidero never ships a first-party autoscaler.
package autoscaler

import (
	"fmt"
	"strconv"
	"strings"
)

// Annotation keys applied to Omni MachineClass resources to declare
// autoscaling bounds and gating policy. All keys live under the
// bearbinary.com/ namespace, matching our existing convention
// (`bearbinary.com/singleton-*`). Exported so the reverse-lookup logic in
// discovery.go and any operator-facing docgen can share one source of
// truth.
const (
	// AnnotationAutoscaleMin is the minimum size of the node group
	// (MachineSet.MachineAllocation.MachineCount floor). Required. Must
	// parse as an integer ≥ 0.
	AnnotationAutoscaleMin = "bearbinary.com/autoscale-min"

	// AnnotationAutoscaleMax is the maximum size of the node group.
	// Required. Must parse as an integer ≥ the min value.
	AnnotationAutoscaleMax = "bearbinary.com/autoscale-max"

	// AnnotationAutoscaleCapacityGate selects the TrueNAS capacity check
	// policy. Optional. Values: CapacityGateHard (default) or
	// CapacityGateSoft. Unknown values fail the parse and skip the node
	// group — safer than silently defaulting when the operator's intent
	// is unclear.
	AnnotationAutoscaleCapacityGate = "bearbinary.com/autoscale-capacity-gate"

	// AnnotationAutoscaleMinPoolFreeGiB is the threshold (in GiB) below
	// which the hard capacity gate refuses scale-up on this MachineSet's
	// backing pool. Optional. 0 disables the pool check.
	AnnotationAutoscaleMinPoolFreeGiB = "bearbinary.com/autoscale-min-pool-free-gib"

	// AnnotationAutoscaleMinHostMemGiB is the threshold (in GiB) below
	// which the hard capacity gate refuses scale-up on this MachineSet's
	// host free memory. Optional. 0 disables the memory check.
	AnnotationAutoscaleMinHostMemGiB = "bearbinary.com/autoscale-min-host-mem-gib"
)

// CapacityGate selects how strict the TrueNAS capacity check is on
// scale-up. See AnnotationAutoscaleCapacityGate.
type CapacityGate string

const (
	// CapacityGateHard blocks scale-up when TrueNAS pool free bytes or
	// host free memory drops below the configured thresholds. The gRPC
	// server returns an error response so the cluster-autoscaler sidecar
	// can mark the node group as capacity-exceeded and stop retrying.
	CapacityGateHard CapacityGate = "hard"

	// CapacityGateSoft emits a warn log + a
	// truenas_autoscaler_capacity_warnings_total increment but still
	// attempts the scale-up. The provisioner then fails naturally if
	// TrueNAS can't fulfill the VM create.
	CapacityGateSoft CapacityGate = "soft"
)

// Defaults applied when the operator omits an optional annotation. Tuned
// conservatively for single-host TrueNAS deployments; operators with
// larger pools should raise these via annotation rather than relying on
// the defaults.
const (
	DefaultMinPoolFreeGiB = 50
	DefaultMinHostMemGiB  = 8
)

// Config is the parsed autoscaling configuration for one MachineClass.
// All fields are validated by ParseMachineClassAutoscaleConfig before the
// struct is returned — callers can trust the struct values without
// re-validating.
type Config struct {
	// Min is the node-group floor (required).
	Min int

	// Max is the node-group ceiling (required, ≥ Min).
	Max int

	// CapacityGate is the enforcement policy for the TrueNAS capacity
	// check. Never empty — ParseMachineClassAutoscaleConfig substitutes
	// CapacityGateHard when the annotation is absent.
	CapacityGate CapacityGate

	// MinPoolFreeGiB is the hard-gate pool-free-bytes threshold in GiB.
	// 0 disables the pool check. Substituted to DefaultMinPoolFreeGiB
	// when the annotation is absent.
	MinPoolFreeGiB int

	// MinHostMemGiB is the hard-gate host-free-memory threshold in GiB.
	// 0 disables the memory check. Substituted to DefaultMinHostMemGiB
	// when the annotation is absent.
	MinHostMemGiB int
}

// IsAutoscaleOptIn reports whether a MachineClass carries any
// autoscaler-related annotation — cheap pre-filter for the discovery
// scan so the full parse only runs on opted-in classes.
func IsAutoscaleOptIn(annotations map[string]string) bool {
	_, hasMin := annotations[AnnotationAutoscaleMin]
	_, hasMax := annotations[AnnotationAutoscaleMax]

	return hasMin || hasMax
}

// ParseMachineClassAutoscaleConfig reads the bearbinary.com/autoscale-*
// annotations off a MachineClass annotations map and returns the
// validated Config. Returns (nil, nil) when the class is not opted in
// (absent min AND max annotations) so the caller can cheaply skip it.
//
// Missing or malformed annotations on an opted-in class are fatal to the
// parse — we return an error rather than partial defaults because
// silently scaling to "some number the operator didn't ask for" is
// worse than logging and skipping the node group.
func ParseMachineClassAutoscaleConfig(annotations map[string]string) (*Config, error) {
	if !IsAutoscaleOptIn(annotations) {
		return nil, nil
	}

	minStr, okMin := annotations[AnnotationAutoscaleMin]
	maxStr, okMax := annotations[AnnotationAutoscaleMax]

	if !okMin {
		return nil, fmt.Errorf("%s is required when %s is set", AnnotationAutoscaleMin, AnnotationAutoscaleMax)
	}

	if !okMax {
		return nil, fmt.Errorf("%s is required when %s is set", AnnotationAutoscaleMax, AnnotationAutoscaleMin)
	}

	minVal, err := parseNonNegativeInt(AnnotationAutoscaleMin, minStr)
	if err != nil {
		return nil, err
	}

	maxVal, err := parseNonNegativeInt(AnnotationAutoscaleMax, maxStr)
	if err != nil {
		return nil, err
	}

	if maxVal < minVal {
		return nil, fmt.Errorf("%s (%d) must be ≥ %s (%d)", AnnotationAutoscaleMax, maxVal, AnnotationAutoscaleMin, minVal)
	}

	gate, err := parseCapacityGate(annotations[AnnotationAutoscaleCapacityGate])
	if err != nil {
		return nil, err
	}

	minPoolFreeGiB, err := parseOptionalGiBThreshold(AnnotationAutoscaleMinPoolFreeGiB, annotations, DefaultMinPoolFreeGiB)
	if err != nil {
		return nil, err
	}

	minHostMemGiB, err := parseOptionalGiBThreshold(AnnotationAutoscaleMinHostMemGiB, annotations, DefaultMinHostMemGiB)
	if err != nil {
		return nil, err
	}

	return &Config{
		Min:            minVal,
		Max:            maxVal,
		CapacityGate:   gate,
		MinPoolFreeGiB: minPoolFreeGiB,
		MinHostMemGiB:  minHostMemGiB,
	}, nil
}

func parseNonNegativeInt(annotation, raw string) (int, error) {
	raw = strings.TrimSpace(raw)

	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", annotation, raw, err)
	}

	if n < 0 {
		return 0, fmt.Errorf("%s %q: must be ≥ 0", annotation, raw)
	}

	return n, nil
}

func parseCapacityGate(raw string) (CapacityGate, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))

	switch raw {
	case "":
		return CapacityGateHard, nil
	case string(CapacityGateHard):
		return CapacityGateHard, nil
	case string(CapacityGateSoft):
		return CapacityGateSoft, nil
	default:
		return "", fmt.Errorf("%s %q: want %q or %q", AnnotationAutoscaleCapacityGate, raw, CapacityGateHard, CapacityGateSoft)
	}
}

func parseOptionalGiBThreshold(annotation string, annotations map[string]string, fallback int) (int, error) {
	raw, ok := annotations[annotation]
	if !ok {
		return fallback, nil
	}

	return parseNonNegativeInt(annotation, raw)
}
