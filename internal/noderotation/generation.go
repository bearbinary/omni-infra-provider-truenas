package noderotation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// generationHashBytes is the number of raw SHA-256 bytes carried in
// the persisted generation hash. 8 bytes encode to 16 hex characters —
// short enough to embed in a COSI annotation value (combined with a
// timestamp the lock annotation stays well under the 256-byte cap) and
// wide enough that operator-time collisions are astronomically
// unlikely (64-bit birthday bound). Named so a future change is
// deliberate, not a magic number tweak.
const generationHashBytes = 8

// generationInputs is the canonical, ordered set of fields hashed to
// produce a MachineClass generation. Marshaled as JSON via the standard
// library's deterministic map-key ordering so two MachineClasses with
// the same effective spec produce the same hash regardless of
// annotation churn or field-ordering in protobuf encoders.
//
// Deliberately narrow: only fields that originate from MachineClass.
// AutoProvision. ProviderData is the primary signal (CPU / memory /
// disk size live in there). KernelArgs / MetaValues / GrpcTunnel also
// flow from the class to the request.
//
// NOT hashed (and why):
//   - TalosVersion / Extensions / Overlay: these live on the Cluster
//     spec, not the MachineClass. A cluster-level Talos bump rotates
//     via Omni's existing upgrade flow, not this controller.
//   - AutoProvision.ProviderID: changing it means migrating the class
//     to a different provider — a manual surgery outside rotation
//     scope.
type generationInputs struct {
	ProviderData string   `json:"provider_data"`
	KernelArgs   []string `json:"kernel_args,omitempty"`
	MetaValues   []string `json:"meta_values,omitempty"`
	GrpcTunnel   string   `json:"grpc_tunnel,omitempty"`
}

// generationBufferPool keeps a single *bytes.Buffer alive across
// ComputeGeneration calls. The reconciler runs per-tick and hashes
// every opted-in MachineClass + every MachineRequest under it; without
// pooling each call allocates a fresh buffer that's immediately
// garbage. Pool resets the buffer length to zero on Put so memory is
// reused without state bleed.
var generationBufferPool = sync.Pool{
	New: func() any { return &bytes.Buffer{} },
}

// ComputeGeneration returns the canonical hex hash of the inputs that
// determine whether a Machine needs rotating. The hash is stable across
// process restarts and across the two parties that compute it
// (reconciler hashing the MachineClass.AutoProvision values, and the
// per-Machine comparison hashing the matching MachineRequest values).
//
// Returns the first 16 hex characters of SHA-256 — see the
// generationHashBytes constant for the rationale.
func ComputeGeneration(inputs generationInputs) (string, error) {
	buf, _ := generationBufferPool.Get().(*bytes.Buffer)

	buf.Reset()

	defer generationBufferPool.Put(buf)

	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(inputs); err != nil {
		return "", fmt.Errorf("encode generation inputs: %w", err)
	}

	sum := sha256.Sum256(buf.Bytes())

	return hex.EncodeToString(sum[:generationHashBytes]), nil
}

// MachineClassGeneration extracts the rotation-relevant fields off a
// MachineClass.AutoProvision spec and returns their canonical hash. The
// caller passes the raw getters rather than the protobuf struct so
// tests can hash synthetic inputs without standing up a full COSI
// state.
//
// Returns ("", nil) when autoProvisionPresent is false — a MachineClass
// that doesn't auto-provision can't be rotated by this controller
// (rotation requires Omni to be the one issuing MachineRequests).
func MachineClassGeneration(autoProvisionPresent bool,
	providerData, grpcTunnel string,
	kernelArgs, metaValues []string,
) (string, error) {
	if !autoProvisionPresent {
		return "", nil
	}

	return ComputeGeneration(generationInputs{
		ProviderData: providerData,
		KernelArgs:   kernelArgs,
		MetaValues:   metaValues,
		GrpcTunnel:   grpcTunnel,
	})
}

// MachineRequestGeneration computes the same hash from the fields baked
// into a MachineRequest at the time Omni issued it. A MachineRequest
// whose hash matches the current MachineClass generation is "fresh"; a
// mismatch is "stale" and a rotation candidate.
func MachineRequestGeneration(
	providerData, grpcTunnel string,
	kernelArgs, metaValues []string,
) (string, error) {
	return ComputeGeneration(generationInputs{
		ProviderData: providerData,
		KernelArgs:   kernelArgs,
		MetaValues:   metaValues,
		GrpcTunnel:   grpcTunnel,
	})
}
