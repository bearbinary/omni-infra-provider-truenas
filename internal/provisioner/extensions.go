package provisioner

import (
	"fmt"
	"os"
	"strings"
)

// allowedExtensions is the vetted set of Talos Image Factory extensions that
// the provider will bake into a schematic without an operator override. The
// list is intentionally conservative: extensions that ship kernel modules,
// setuid binaries, or host-reaching side-effects must be added here before
// operators can use them.
//
// Not allowlisting an extension is not a statement that it is malicious — it
// just means the provider maintainers have not reviewed the supply chain for
// that extension. Operators who know what they're doing can set
// ALLOW_UNSIGNED_EXTENSIONS=true to bypass this gate.
//
// Source: https://github.com/siderolabs/extensions
//
// Keep this list sorted alphabetically for easy auditing.
var allowedExtensions = map[string]bool{
	// Container runtime adjuncts
	"siderolabs/gvisor":          true,
	"siderolabs/kata-containers": true,
	"siderolabs/spin":            true,
	"siderolabs/wasmedge":        true,

	// Firmware blobs (board-specific)
	"siderolabs/amd-ucode":         true,
	"siderolabs/amdgpu-firmware":   true,
	"siderolabs/bnx2-bnx2x":        true,
	"siderolabs/chelsio-firmware":  true,
	"siderolabs/dell-firmware":     true,
	"siderolabs/i915-ucode":        true,
	"siderolabs/intel-ucode":       true,
	"siderolabs/mei-firmware":      true,
	"siderolabs/mellanox-firmware": true,
	"siderolabs/rtl8169-firmware":  true,
	"siderolabs/xe-firmware":       true,

	// GPU / compute
	"siderolabs/gasket-driver":                  true,
	"siderolabs/nonfree-kmod-nvidia":            true,
	"siderolabs/nvidia-container-toolkit":       true,
	"siderolabs/nvidia-fabric-manager":          true,
	"siderolabs/nvidia-open-gpu-kernel-modules": true,

	// Filesystems / storage drivers
	"siderolabs/binfmt-misc": true,
	"siderolabs/drbd":        true,
	"siderolabs/iscsi-tools": true,
	"siderolabs/mdadm":       true,
	"siderolabs/nfs-utils":   true,
	"siderolabs/nut-client":  true,
	"siderolabs/zfs":         true,

	// Networking / management
	"siderolabs/bind-tools":              true,
	"siderolabs/btrfs-progs":             true,
	"siderolabs/cifs-utils":              true,
	"siderolabs/glibc":                   true,
	"siderolabs/kernel-samepage-merging": true,
	"siderolabs/lldpd":                   true,
	"siderolabs/qemu-guest-agent":        true,
	"siderolabs/tailscale":               true,
	"siderolabs/thunderbolt":             true,
	"siderolabs/util-linux-tools":        true,
	"siderolabs/v4l-uvc-drivers":         true,
	"siderolabs/vmtoolsd-guest-agent":    true,
	"siderolabs/xen-guest-agent":         true,
	"siderolabs/zerotier":                true,
}

// allowUnsignedExtensions returns true when the operator has opted out of the
// built-in extension allowlist. Intended for operators who publish custom
// schematic IDs or who want to pull in an extension the provider maintainers
// haven't catalogued yet.
func allowUnsignedExtensions() bool {
	v := strings.ToLower(os.Getenv("ALLOW_UNSIGNED_EXTENSIONS"))
	return v == "true" || v == "1" || v == "yes"
}

// validateExtensions returns an error if any entry in extensions is not on
// the built-in allowlist and the operator has not set ALLOW_UNSIGNED_EXTENSIONS.
// Also rejects entries with a clearly malformed name (path traversal, whitespace).
func validateExtensions(extensions []string) error {
	bypass := allowUnsignedExtensions()

	for i, ext := range extensions {
		// Structural sanity: no empty strings, no path-traversal tricks,
		// no whitespace. These must always fail even under the bypass flag.
		if ext == "" {
			return fmt.Errorf("extensions[%d] is empty", i)
		}

		if strings.ContainsAny(ext, " \t\r\n") {
			return fmt.Errorf("extensions[%d] %q contains whitespace", i, ext)
		}

		if strings.Contains(ext, "..") || strings.HasPrefix(ext, "/") || strings.Contains(ext, "//") {
			return fmt.Errorf("extensions[%d] %q contains invalid path characters", i, ext)
		}

		if bypass {
			continue
		}

		if !allowedExtensions[ext] {
			return fmt.Errorf("extensions[%d] %q is not on the provider's allowlist — either choose a vetted extension (see docs/hardening.md) or set ALLOW_UNSIGNED_EXTENSIONS=true to opt into running unreviewed extensions", i, ext)
		}
	}

	return nil
}
