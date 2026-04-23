package provisioner

import (
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strings"
)

// Data is the provider custom machine config from the MachineClass.
// Fields map to the schema.json that is reported to Omni.

// AdditionalNIC describes an extra NIC to attach to the VM beyond the primary.
// All NICs (primary and additional) receive a deterministic MAC derived from
// the machine request ID so DHCP reservations survive reprovisioning.
type AdditionalNIC struct {
	NetworkInterface string `yaml:"network_interface"` // Required: bridge, VLAN, or physical interface
	Type             string `yaml:"type,omitempty"`    // VIRTIO (default) or E1000
	MTU              int    `yaml:"mtu,omitempty"`     // MTU size (default: 0 = use host default, typically 1500). Set to 9000 for jumbo frames.

	// DHCP controls whether Talos runs a DHCPv4 client on this NIC.
	//   - nil / unset → default is true when Addresses is empty, false when Addresses is set
	//   - explicit true → DHCP always runs (can coexist with static Addresses)
	//   - explicit false → no DHCP, link stays configured only by Addresses (if any)
	// Without this patch Talos only DHCPs the primary link by default, so
	// additional NICs would come up with link-local IPv6 only.
	DHCP *bool `yaml:"dhcp,omitempty"`

	// Addresses are static IPv4/IPv6 addresses in CIDR form assigned to
	// this NIC (e.g., "10.20.0.5/24"). Optional — leave empty for
	// DHCP-only. Setting Addresses without an explicit DHCP value turns
	// DHCP off (static-only); set DHCP: true explicitly to run both.
	Addresses []string `yaml:"addresses,omitempty"`

	// Gateway is an optional default route advertised via this NIC (IP,
	// not CIDR). Only meaningful with Addresses — a DHCP-only NIC gets
	// routes from the DHCP server. Set when the secondary segment needs
	// a different default route than the primary NIC.
	Gateway string `yaml:"gateway,omitempty"`
}

// AdditionalDisk describes an extra disk to attach to the VM beyond the root disk.
type AdditionalDisk struct {
	Size          int    `yaml:"size"`                     // Size in GiB (required)
	Pool          string `yaml:"pool,omitempty"`           // Pool override (defaults to primary pool)
	DatasetPrefix string `yaml:"dataset_prefix,omitempty"` // Dataset prefix override (defaults to MachineClass dataset_prefix)
	Encrypted     bool   `yaml:"encrypted,omitempty"`      // Per-disk encryption toggle

	// Name sets the Talos UserVolumeConfig name emitted for this disk. The
	// volume is mounted at /var/mnt/<name> inside the guest. Default: data-N
	// (1-indexed). Setting this to "longhorn" matches the default Longhorn
	// defaultDataPath = /var/mnt/longhorn.
	Name string `yaml:"name,omitempty"`

	// Filesystem for the emitted UserVolumeConfig. "xfs" (default) or "ext4".
	// Longhorn recommends xfs for best performance on modern kernels.
	Filesystem string `yaml:"filesystem,omitempty"`
}

type Data struct {
	Pool             string   `yaml:"pool,omitempty"`
	NetworkInterface string   `yaml:"network_interface,omitempty"` // Primary NIC: bridge, VLAN, or physical interface
	BootMethod       string   `yaml:"boot_method,omitempty"`
	Architecture     string   `yaml:"architecture,omitempty"`
	Extensions       []string `yaml:"extensions,omitempty"` // Additional Talos system extensions beyond the defaults
	Encrypted        bool     `yaml:"encrypted,omitempty"`  // Enable ZFS native encryption on the VM zvol
	CPUs             int      `yaml:"cpus,omitempty"`
	Memory           int      `yaml:"memory,omitempty"`
	DiskSize         int      `yaml:"disk_size,omitempty"`

	// DatasetPrefix is an optional ZFS dataset path under the pool.
	// When set, zvols are created at <pool>/<dataset_prefix>/omni-vms/<request-id>
	// and ISOs are cached at <pool>/<dataset_prefix>/talos-iso/.
	// Each segment must be a valid ZFS name (no slashes in individual segments).
	// Example: "previewk8/k8" places zvols at default/previewk8/k8/omni-vms/...
	DatasetPrefix string `yaml:"dataset_prefix,omitempty"`

	// Multi-disk: attach additional data disks beyond the root disk
	AdditionalDisks []AdditionalDisk `yaml:"additional_disks,omitempty"`

	// Multi-NIC: attach additional NICs for network segmentation
	AdditionalNICs []AdditionalNIC `yaml:"additional_nics,omitempty"`

	// Multihoming: pin etcd/kubelet to specific subnets when multiple NICs are present
	// Comma-separated CIDRs, e.g., "192.168.100.0/24" or "192.168.100.0/24,fd00::/64"
	AdvertisedSubnets string `yaml:"advertised_subnets,omitempty"`

	// StorageDiskSize adds a dedicated data disk (in GiB) for persistent storage (Longhorn).
	// Equivalent to additional_disks: [{size: N}].
	StorageDiskSize int `yaml:"storage_disk_size,omitempty"`
}

// ApplyDefaults fills in zero values from the provider config.
func (d *Data) ApplyDefaults(cfg ProviderConfig) {
	if d.CPUs == 0 {
		d.CPUs = 2
	}

	if d.Memory == 0 {
		d.Memory = 4096
	}

	if d.DiskSize == 0 {
		d.DiskSize = 40
	}

	if d.Pool == "" {
		d.Pool = cfg.DefaultPool
	}

	if d.NetworkInterface == "" {
		d.NetworkInterface = cfg.DefaultNetworkInterface
	}

	if d.BootMethod == "" {
		d.BootMethod = cfg.DefaultBootMethod
	}

	if d.BootMethod == "" {
		d.BootMethod = "UEFI"
	}

	if d.Architecture == "" {
		d.Architecture = "amd64"
	}

	// Expand storage_disk_size into additional_disks[0].
	// This is a convenience shorthand for adding a dedicated data disk for Longhorn:
	// the emitted UserVolumeConfig is named "longhorn" so the volume mounts at
	// /var/mnt/longhorn, which matches Longhorn's defaultDataPath. The
	// provisioner also emits a Longhorn operational patch (iscsi_tcp kernel
	// module + /var/lib/longhorn bind mount + vm.overcommit_memory sysctl)
	// when it sees a disk named "longhorn" — see buildLonghornOperationalPatch.
	if d.StorageDiskSize > 0 {
		storageDisk := AdditionalDisk{Size: d.StorageDiskSize, Name: LonghornVolumeName}
		d.AdditionalDisks = append([]AdditionalDisk{storageDisk}, d.AdditionalDisks...)
		d.StorageDiskSize = 0 // consumed — prevent double-expansion
	}

	// Fill defaults for each additional disk. Index assignment happens after
	// any prepended storage_disk_size expansion so names stay 1-based and stable.
	for i := range d.AdditionalDisks {
		if d.AdditionalDisks[i].Name == "" {
			d.AdditionalDisks[i].Name = fmt.Sprintf("data-%d", i+1)
		}

		if d.AdditionalDisks[i].Filesystem == "" {
			d.AdditionalDisks[i].Filesystem = "xfs"
		}
	}
}

// BasePath returns the ZFS dataset root for this machine config.
// If DatasetPrefix is set, returns "<pool>/<prefix>", otherwise just "<pool>".
func (d *Data) BasePath() string {
	if d.DatasetPrefix != "" {
		return d.Pool + "/" + d.DatasetPrefix
	}

	return d.Pool
}

// cachedKnownFields is computed once at init time since Data struct tags never change.
var cachedKnownFields = func() map[string]bool {
	fields := make(map[string]bool)
	t := reflect.TypeOf(Data{})

	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}

		name := strings.Split(tag, ",")[0]
		fields[name] = true
	}

	return fields
}()

// knownFields returns the set of known YAML field names from the Data struct tags.
func knownFields() map[string]bool {
	return cachedKnownFields
}

// UnknownFields returns field names present in rawData that are not recognized by the Data struct.
func UnknownFields(rawData map[string]any) []string {
	known := knownFields()
	var unknown []string

	for key := range rawData {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}

	return unknown
}

// safeNameRe matches ZFS-safe identifiers: alphanumeric, hyphens, underscores, dots.
// No slashes, spaces, or special characters that could enable path traversal.
var safeNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// validateSafeName checks that a user-provided name is safe for use in filesystem paths and API calls.
func validateSafeName(field, value string) error {
	if value == "" {
		return nil
	}

	if !safeNameRe.MatchString(value) {
		return fmt.Errorf("%s contains unsafe characters: %q — only alphanumeric, hyphens, underscores, and dots are allowed", field, value)
	}

	return nil
}

// MinDiskSizeGiB is the minimum allowed size for any additional data disk
// (a disk attached alongside the Talos system disk). 5 GiB is enough for
// small data volumes, log sidecars, and test fixtures; workloads that
// need more tune per-MachineClass via `additional_disks`.
const MinDiskSizeGiB = 5

// MinRootDiskSizeGiB is the floor for the VM's primary (OS / system)
// disk. Must be large enough to hold the Talos image plus every image
// a Kubernetes control-plane node pulls during cluster bootstrap:
// kube-apiserver, kube-controller-manager, kube-scheduler, etcd, the
// kubelet sidecars, the CNI image, CoreDNS, and an overhead margin
// for update layering. Empirically 20 GiB is the smallest number
// where a CP node survives a full 1.30+ bootstrap without the kubelet
// hitting DiskPressure-triggered image garbage collection mid-install.
//
// Observed failure mode when this was set to 5 GiB (the additional-disk
// floor applied to the root): control-plane nodes entered a loop of
// "failed to pull image: no space left on device" → GC → re-pull, and
// etcd never came up because its image was evicted mid-write.
//
// Workers could technically get away with less, but the simpler policy
// is one root-disk minimum for every role — a 20 GiB zvol is negligible
// overhead on any TrueNAS pool we ship against.
const MinRootDiskSizeGiB = 20

// MaxDiskSizeGiB mirrors the JSON schema ceiling (1 PiB). Schema is the UX-level
// gate; this Go-side check is defense-in-depth for callers that bypass schema
// validation (direct COSI edits, legacy MachineClass payloads).
const MaxDiskSizeGiB = 1048576

// MaxCPUs and MaxMemoryMiB mirror the schema ceilings and protect int arithmetic
// downstream (byte conversion, pool capacity planning) from silent overflow.
const (
	MaxCPUs      = 512
	MaxMemoryMiB = 16777216
	MinMemoryMiB = 1024
)

// MaxAdditionalNICs and MaxAddressesPerNIC cap per-MachineClass input size.
// Without them a MachineClass with 10k entries serializes to a multi-MB
// ConfigPatchRequest resource that Omni stores and every reconcile re-fetches
// — trivial operator-input DoS vector. 16 is the TrueNAS practical ceiling
// for NICs on a single VM and generously covers dual-stack + a few VIPs
// worth of static addresses on one link.
const (
	MaxAdditionalNICs  = 16
	MaxAddressesPerNIC = 16
)

// Validate checks the Data config for logical errors.
func (d *Data) Validate() error {
	if d.CPUs < 0 {
		return fmt.Errorf("cpus must be >= 0, got %d", d.CPUs)
	}

	if d.CPUs > MaxCPUs {
		return fmt.Errorf("cpus must be <= %d, got %d", MaxCPUs, d.CPUs)
	}

	if d.Memory < 0 {
		return fmt.Errorf("memory must be >= 0, got %d", d.Memory)
	}

	if d.Memory != 0 && d.Memory < MinMemoryMiB {
		return fmt.Errorf("memory must be >= %d MiB when set, got %d", MinMemoryMiB, d.Memory)
	}

	if d.Memory > MaxMemoryMiB {
		return fmt.Errorf("memory must be <= %d MiB, got %d", MaxMemoryMiB, d.Memory)
	}

	if d.DiskSize < 0 {
		return fmt.Errorf("disk_size must be >= 0, got %d", d.DiskSize)
	}

	// The OS / system disk floor is deliberately higher than the
	// additional-disk floor: a Talos control-plane node pulls
	// kube-apiserver, etcd, scheduler, controller-manager, CNI, and
	// CoreDNS during bootstrap. Undersized root disks trigger the
	// kubelet's image GC loop mid-install and etcd never comes up.
	// See MinRootDiskSizeGiB's docstring for the incident history.
	if d.DiskSize != 0 && d.DiskSize < MinRootDiskSizeGiB {
		return fmt.Errorf("disk_size must be >= %d GiB — control-plane nodes need room for the Talos image plus kube-* and etcd image pulls during bootstrap (got %d)", MinRootDiskSizeGiB, d.DiskSize)
	}

	if d.DiskSize > MaxDiskSizeGiB {
		return fmt.Errorf("disk_size must be <= %d GiB, got %d", MaxDiskSizeGiB, d.DiskSize)
	}

	if d.StorageDiskSize < 0 {
		return fmt.Errorf("storage_disk_size must be >= 0, got %d", d.StorageDiskSize)
	}

	if d.StorageDiskSize > 0 && d.StorageDiskSize < MinDiskSizeGiB {
		return fmt.Errorf("storage_disk_size must be >= %d GiB when set, got %d", MinDiskSizeGiB, d.StorageDiskSize)
	}

	if d.StorageDiskSize > MaxDiskSizeGiB {
		return fmt.Errorf("storage_disk_size must be <= %d GiB, got %d", MaxDiskSizeGiB, d.StorageDiskSize)
	}

	if err := validateExtensions(d.Extensions); err != nil {
		return err
	}

	// Validate names used in filesystem paths to prevent path traversal
	if err := validateSafeName("pool", d.Pool); err != nil {
		return err
	}

	if err := validateSafeName("network_interface", d.NetworkInterface); err != nil {
		return err
	}

	// Validate each segment of dataset_prefix individually (slashes are path separators, not part of names)
	if d.DatasetPrefix != "" {
		segments := strings.Split(d.DatasetPrefix, "/")
		for i, seg := range segments {
			if seg == "" {
				return fmt.Errorf("dataset_prefix has empty segment at position %d — use 'a/b' not 'a//b' or '/a/b'", i)
			}

			if err := validateSafeName(fmt.Sprintf("dataset_prefix segment %d (%q)", i, seg), seg); err != nil {
				return err
			}
		}
	}

	if len(d.AdditionalDisks) > 16 {
		return fmt.Errorf("additional_disks: maximum 16 additional disks allowed, got %d", len(d.AdditionalDisks))
	}

	seenVolumeNames := make(map[string]int)

	for i, disk := range d.AdditionalDisks {
		if disk.Size < MinDiskSizeGiB {
			return fmt.Errorf("additional_disks[%d]: size must be >= %d GiB, got %d", i, MinDiskSizeGiB, disk.Size)
		}

		if disk.Size > MaxDiskSizeGiB {
			return fmt.Errorf("additional_disks[%d]: size must be <= %d GiB, got %d", i, MaxDiskSizeGiB, disk.Size)
		}

		if disk.Pool != "" {
			if err := validateSafeName(fmt.Sprintf("additional_disks[%d].pool", i), disk.Pool); err != nil {
				return err
			}
		}

		if disk.DatasetPrefix != "" {
			for j, seg := range strings.Split(disk.DatasetPrefix, "/") {
				if seg == "" {
					continue
				}

				if err := validateSafeName(fmt.Sprintf("additional_disks[%d].dataset_prefix segment %d (%q)", i, j, seg), seg); err != nil {
					return err
				}
			}
		}

		if disk.Name != "" {
			if err := validateSafeName(fmt.Sprintf("additional_disks[%d].name", i), disk.Name); err != nil {
				return err
			}

			if prev, dup := seenVolumeNames[disk.Name]; dup {
				return fmt.Errorf("additional_disks[%d].name %q collides with additional_disks[%d].name — each volume name must be unique because it becomes the mount path at /var/mnt/<name>", i, disk.Name, prev)
			}

			seenVolumeNames[disk.Name] = i
		}

		if disk.Filesystem != "" && disk.Filesystem != "xfs" && disk.Filesystem != "ext4" {
			return fmt.Errorf("additional_disks[%d].filesystem must be \"xfs\" or \"ext4\", got %q", i, disk.Filesystem)
		}
	}

	if len(d.AdditionalNICs) > MaxAdditionalNICs {
		return fmt.Errorf("additional_nics: at most %d NICs supported (got %d) — caps prevent operator-input DoS on the ConfigPatchRequest resource size", MaxAdditionalNICs, len(d.AdditionalNICs))
	}

	seen := make(map[string]bool)

	// Primary NIC is always in the "seen" set
	if d.NetworkInterface != "" {
		seen[d.NetworkInterface] = true
	}

	for i, nic := range d.AdditionalNICs {
		if nic.NetworkInterface == "" {
			return fmt.Errorf("additional_nics[%d]: network_interface is required", i)
		}

		if len(nic.Addresses) > MaxAddressesPerNIC {
			return fmt.Errorf("additional_nics[%d].addresses: at most %d addresses per NIC (got %d)", i, MaxAddressesPerNIC, len(nic.Addresses))
		}

		if err := validateSafeName(fmt.Sprintf("additional_nics[%d].network_interface", i), nic.NetworkInterface); err != nil {
			return err
		}

		if seen[nic.NetworkInterface] {
			return fmt.Errorf("additional_nics[%d]: duplicate network_interface %q — each NIC must use a different interface", i, nic.NetworkInterface)
		}

		seen[nic.NetworkInterface] = true

		if nic.Type != "" && nic.Type != "VIRTIO" && nic.Type != "E1000" {
			return fmt.Errorf("additional_nics[%d]: type must be VIRTIO or E1000, got %q", i, nic.Type)
		}

		if nic.MTU != 0 && (nic.MTU < 576 || nic.MTU > 9216) {
			return fmt.Errorf("additional_nics[%d]: mtu must be between 576 and 9216, got %d", i, nic.MTU)
		}

		parsedAddrs := make([]*net.IPNet, 0, len(nic.Addresses))

		for j, addr := range nic.Addresses {
			ip, ipnet, cidrErr := net.ParseCIDR(addr)
			if cidrErr != nil {
				return fmt.Errorf("additional_nics[%d].addresses[%d]: %q is not a valid CIDR (e.g. \"10.0.0.5/24\") — %w", i, j, addr, cidrErr)
			}

			// Reject addresses that are not valid interface addresses. Any of
			// these would either hijack routing (0.0.0.0/0), fail Linux's
			// address-assign (multicast/loopback/broadcast), or act as a
			// silent "attach but do nothing" (unspecified).
			if ip.IsUnspecified() {
				return fmt.Errorf("additional_nics[%d].addresses[%d]: %q is the unspecified address; not valid as an interface address", i, j, addr)
			}

			if ip.IsMulticast() {
				return fmt.Errorf("additional_nics[%d].addresses[%d]: %q is a multicast address; not valid as an interface address", i, j, addr)
			}

			if ip.IsLoopback() {
				return fmt.Errorf("additional_nics[%d].addresses[%d]: %q is a loopback address; not valid as an interface address", i, j, addr)
			}

			if ones, _ := ipnet.Mask.Size(); ones == 0 {
				return fmt.Errorf("additional_nics[%d].addresses[%d]: %q uses a zero-length mask (default route); assigning /0 to an interface hijacks all routing", i, j, addr)
			}

			// Track the network (with IP embedded for reachability checks
			// against the gateway below).
			parsedAddrs = append(parsedAddrs, &net.IPNet{IP: ip, Mask: ipnet.Mask})
		}

		if nic.Gateway != "" {
			ip := net.ParseIP(nic.Gateway)
			if ip == nil {
				return fmt.Errorf("additional_nics[%d].gateway: %q is not a valid IP address", i, nic.Gateway)
			}

			// Unicast check — reject obviously broken gateway IPs that would
			// produce nonsense default routes (kernel rejects at apply) or
			// silent black-holes (if the broken address happens to be
			// reachable on the segment).
			if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLoopback() || ip.Equal(net.IPv4bcast) {
				return fmt.Errorf("additional_nics[%d].gateway: %q is not a valid unicast IP (unspecified, multicast, loopback, and broadcast addresses are rejected)", i, nic.Gateway)
			}

			if len(nic.Addresses) == 0 {
				return fmt.Errorf("additional_nics[%d]: gateway %q set without addresses — a gateway only applies to a statically-configured NIC", i, nic.Gateway)
			}

			// Family match: an IPv6 gateway needs at least one IPv6 address
			// on the link (and vice-versa). Without this, the builder would
			// emit an IPv4 default route (network: 0.0.0.0/0) pointing at an
			// IPv6 gateway — Talos or Linux rejects at apply, or the node
			// silently has no default route.
			gwIsV4 := ip.To4() != nil
			hasMatchingFamily := false
			onLink := false

			for _, cidr := range parsedAddrs {
				addrIsV4 := cidr.IP.To4() != nil
				if addrIsV4 == gwIsV4 {
					hasMatchingFamily = true
				}

				if cidr.Contains(ip) {
					onLink = true
				}
			}

			if !hasMatchingFamily {
				fam := "IPv6"
				if gwIsV4 {
					fam = "IPv4"
				}

				return fmt.Errorf("additional_nics[%d]: gateway %q is %s but none of the configured addresses are in that family — add an %s address or change the gateway", i, nic.Gateway, fam, fam)
			}

			if !onLink {
				return fmt.Errorf("additional_nics[%d]: gateway %q is not on-link with any of the configured addresses %v — Talos/Linux will refuse to install the route", i, nic.Gateway, nic.Addresses)
			}
		}
	}

	// Only one additional NIC may declare a gateway. Multiple NICs each
	// emitting a 0.0.0.0/0 default route with no distinguishing metric leads
	// to kernel-defined non-deterministic routing — exactly the "traffic
	// sometimes goes the wrong way" multi-homed failure mode, and
	// exploitable by a malicious operator to flap the node between two
	// upstream paths.
	gatewayCount := 0
	for _, nic := range d.AdditionalNICs {
		if nic.Gateway != "" {
			gatewayCount++
		}
	}

	if gatewayCount > 1 {
		return fmt.Errorf("additional_nics: at most one NIC may declare a gateway (got %d) — multiple default routes without distinct metrics cause non-deterministic routing", gatewayCount)
	}

	return nil
}
