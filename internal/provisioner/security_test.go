package provisioner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSafeName_ValidNames(t *testing.T) {
	validNames := []string{
		"default",
		"tank",
		"my-pool",
		"my_pool",
		"pool.name",
		"Tank123",
		"br100",
		"enp5s0",
		"vlan666",
		"a",
	}

	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, validateSafeName("test", name))
		})
	}
}

func TestValidateSafeName_Empty(t *testing.T) {
	// Empty is allowed — validation for required fields is done elsewhere
	assert.NoError(t, validateSafeName("test", ""))
}

func TestValidateSafeName_PathTraversal(t *testing.T) {
	malicious := []string{
		"../etc",
		"../../passwd",
		"pool/subdir",
		"tank/omni-vms",
		"/absolute/path",
	}

	for _, name := range malicious {
		t.Run(name, func(t *testing.T) {
			err := validateSafeName("pool", name)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unsafe characters")
		})
	}
}

func TestValidateSafeName_SpecialChars(t *testing.T) {
	malicious := []string{
		"pool name",       // spaces
		"pool;rm -rf /",   // command injection
		"pool$(whoami)",   // subshell
		"pool`id`",        // backtick
		"pool\nname",      // newline
		"pool\x00name",    // null byte
		"$HOME",           // env var expansion
		"pool&background", // ampersand
		"|pipe",           // pipe
	}

	for _, name := range malicious {
		t.Run(name, func(t *testing.T) {
			err := validateSafeName("pool", name)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unsafe characters")
		})
	}
}

func TestValidateSafeName_StartsWithSpecialChar(t *testing.T) {
	// Must start with alphanumeric
	invalid := []string{
		"-leading-hyphen",
		"_leading-underscore",
		".leading-dot",
	}

	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			err := validateSafeName("test", name)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unsafe characters")
		})
	}
}

func TestData_Validate_PoolInjection(t *testing.T) {
	d := &Data{
		Pool:      "../etc",
		NICAttach: "br100",
	}

	err := d.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsafe characters")
}

func TestData_Validate_NICInjection(t *testing.T) {
	d := &Data{
		Pool:      "tank",
		NICAttach: "br100; rm -rf /",
	}

	err := d.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsafe characters")
}

func TestData_Validate_AdditionalNICInjection(t *testing.T) {
	d := &Data{
		Pool:      "tank",
		NICAttach: "br100",
		AdditionalNICs: []AdditionalNIC{
			{NICAttach: "../../etc/shadow"},
		},
	}

	err := d.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsafe characters")
}

func TestHashRequestID_Deterministic(t *testing.T) {
	h1 := hashRequestID("test-request-123")
	h2 := hashRequestID("test-request-123")
	assert.Equal(t, h1, h2)
}

func TestHashRequestID_DifferentInputsDifferentOutput(t *testing.T) {
	h1 := hashRequestID("request-a")
	h2 := hashRequestID("request-b")
	assert.NotEqual(t, h1, h2)
}

func TestHashRequestID_DoesNotContainInput(t *testing.T) {
	input := "talos-test-workers-abc123"
	h := hashRequestID(input)
	assert.NotContains(t, h, input)
	assert.NotContains(t, h, "talos")
	assert.Len(t, h, 16) // 8 bytes = 16 hex chars
}
