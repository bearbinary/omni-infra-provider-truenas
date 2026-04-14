package provisioner

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// gibiByte = 1024^3 — exact size of a 1 GiB ZFS zvol in bytes.
const gibiByte = 1 << 30

// diskSizeTolerance is the match window around the target zvol size.
// ZFS rounds volsize to the volume block size, and Talos reports usable
// block-device size which may differ from volsize by a sector or two. A
// ±1 MiB window catches the real disk without overlapping neighbor sizes.
const diskSizeTolerance = 1 << 20 // 1 MiB

// buildUserVolumePatch generates a multi-doc Talos config patch that formats
// and mounts each additional disk as a Talos UserVolumeConfig. Without this,
// disks attached via TrueNAS appear as raw unformatted block devices inside
// the guest and are invisible to Kubernetes workloads.
//
// Each disk becomes its own UserVolumeConfig document. The disk selector uses
// exact zvol byte-size so each document claims a specific disk even when
// multiple disks of different sizes are attached. The !system_disk clause
// prevents the root zvol from ever matching.
//
// Output is multi-doc YAML — one document per disk, separated by "---". The
// provider SDK applies this as a single named config patch; Omni distributes
// the documents to machine config + standalone kinds (UserVolumeConfig is a
// standalone Talos v1alpha1 kind, not a merge patch into machine config).
//
// Example output for AdditionalDisks=[{Size: 150, Name: "longhorn"}]:
//
//	apiVersion: v1alpha1
//	kind: UserVolumeConfig
//	name: longhorn
//	provisioning:
//	    diskSelector:
//	        match: '!system_disk && disk.size >= 161060225024u && disk.size <= 161062322176u'
//	    minSize: 1GiB
//	    maxSize: 0
//	    grow: true
//	filesystem:
//	    type: xfs
//
// Returns (nil, nil) when disks is empty — caller should skip patch emission.
func buildUserVolumePatch(disks []AdditionalDisk) ([]byte, error) {
	if len(disks) == 0 {
		return nil, nil
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	for i, disk := range disks {
		if disk.Name == "" {
			return nil, fmt.Errorf("additional_disks[%d]: name is empty — ApplyDefaults must run before buildUserVolumePatch", i)
		}

		if disk.Size <= 0 {
			return nil, fmt.Errorf("additional_disks[%d] (%q): size must be > 0", i, disk.Name)
		}

		fs := disk.Filesystem
		if fs == "" {
			fs = "xfs"
		}

		sizeBytes := int64(disk.Size) * gibiByte
		low := sizeBytes - diskSizeTolerance
		high := sizeBytes + diskSizeTolerance

		// CEL selector: exclude system disk, match byte-range around zvol size.
		// Talos claims the first unclaimed match per UserVolumeConfig, so if
		// two disks share an exact size the earlier-indexed volume gets the
		// first discovered disk.
		selector := fmt.Sprintf("!system_disk && disk.size >= %du && disk.size <= %du", low, high)

		// Omit maxSize entirely when we want the volume to grow unbounded.
		// Talos validates minSize <= maxSize, so emitting `maxSize: 0` fails
		// with "min size is greater than max size" — observed on a real
		// cluster in v0.14.3–v0.14.5. Per Talos v1.12 UserVolumeConfig docs,
		// an unset maxSize with grow:true means "fill the matched disk".
		doc := map[string]any{
			"apiVersion": "v1alpha1",
			"kind":       "UserVolumeConfig",
			"name":       disk.Name,
			"provisioning": map[string]any{
				"diskSelector": map[string]any{
					"match": selector,
				},
				"minSize": "1GiB",
				"grow":    true,
			},
			"filesystem": map[string]any{
				"type": fs,
			},
		}

		if err := enc.Encode(doc); err != nil {
			return nil, fmt.Errorf("encode UserVolumeConfig for additional_disks[%d] (%q): %w", i, disk.Name, err)
		}
	}

	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close yaml encoder: %w", err)
	}

	return buf.Bytes(), nil
}
