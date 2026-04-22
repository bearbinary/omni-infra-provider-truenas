package provisioner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateExtensions_AllowsEmpty(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateExtensions(nil))
	require.NoError(t, validateExtensions([]string{}))
}

func TestValidateExtensions_AllowsVetted(t *testing.T) {
	t.Parallel()

	vetted := []string{
		"siderolabs/qemu-guest-agent",
		"siderolabs/iscsi-tools",
		"siderolabs/util-linux-tools",
		"siderolabs/nfs-utils",
		"siderolabs/zfs",
	}

	for _, ext := range vetted {
		require.NoError(t, validateExtensions([]string{ext}),
			"vetted extension %q should be allowed by default", ext)
	}
}

func TestValidateExtensions_RejectsUnknown(t *testing.T) {
	// Cannot t.Parallel — t.Setenv mutates process env.
	t.Setenv("ALLOW_UNSIGNED_EXTENSIONS", "")

	err := validateExtensions([]string{"attacker/sneaky-extension"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not on the provider's allowlist")
}

func TestValidateExtensions_BypassFlagAllowsUnknown(t *testing.T) {
	// Cannot parallel — t.Setenv
	t.Setenv("ALLOW_UNSIGNED_EXTENSIONS", "true")

	err := validateExtensions([]string{"community/experimental-driver"})
	require.NoError(t, err, "bypass flag should permit unreviewed extensions")
}

func TestValidateExtensions_RejectsStructuralIssues(t *testing.T) {
	// Even under bypass, structurally-bad names must fail — prevents
	// shell/path injection if the string ever reaches a CLI.
	t.Setenv("ALLOW_UNSIGNED_EXTENSIONS", "true")

	bad := []string{
		"",
		"siderolabs/../etc/shadow",
		"siderolabs/qemu-guest-agent with space",
		"/etc/shadow",
		"siderolabs//double-slash",
	}

	for _, ext := range bad {
		err := validateExtensions([]string{ext})
		assert.Error(t, err, "structurally bad extension %q must be rejected", ext)
	}
}
