package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSmoke_BinaryBuilds verifies the provider binary compiles successfully.
// Catches import cycles, missing embed files, and build-time errors.
func TestSmoke_BinaryBuilds(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "binary should compile: %s", string(out))
}

// TestSmoke_SchemaEmbedIsValidJSON verifies the embedded schema.json parses correctly.
func TestSmoke_SchemaEmbedIsValidJSON(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, schema, "embedded schema should not be empty")

	var parsed map[string]any
	err := json.Unmarshal([]byte(schema), &parsed)
	require.NoError(t, err, "embedded schema should be valid JSON")

	assert.Contains(t, parsed, "type", "schema should have a 'type' field")
	assert.Contains(t, parsed, "properties", "schema should have 'properties'")
	assert.Contains(t, parsed, "required", "schema should have 'required'")
}

// TestSmoke_IconEmbedNotEmpty verifies the embedded icon.svg is present.
func TestSmoke_IconEmbedNotEmpty(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, icon, "embedded icon should not be empty")
	assert.Contains(t, string(icon), "<svg", "icon should be SVG")
}

// TestSmoke_MissingOmniEndpoint verifies the binary fails cleanly when
// OMNI_ENDPOINT is not set, rather than panicking or hanging.
func TestSmoke_MissingOmniEndpoint(t *testing.T) {
	// Cannot use t.Parallel — t.Setenv mutates process env
	t.Setenv("OMNI_ENDPOINT", "")
	t.Setenv("TRUENAS_HOST", "")

	err := run()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "OMNI_ENDPOINT", "should fail with clear error about missing OMNI_ENDPOINT")
}

// TestSmoke_MissingTrueNASConnection verifies the binary fails cleanly when
// TRUENAS_HOST is not configured.
func TestSmoke_MissingTrueNASConnection(t *testing.T) {
	// Cannot use t.Parallel — t.Setenv mutates process env
	t.Setenv("OMNI_ENDPOINT", "https://fake.example.com")
	t.Setenv("TRUENAS_HOST", "")

	err := run()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "TRUENAS_HOST is required", "should fail with clear config error")
}

// TestSmoke_VersionParser verifies the TrueNAS version parser handles edge cases.
func TestSmoke_VersionParser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version   string
		supported bool
	}{
		{"TrueNAS-SCALE-25.04.0", true},
		{"TrueNAS-SCALE-25.10.1", true},
		{"TrueNAS-SCALE-26.04.0", true},
		{"TrueNAS-SCALE-24.10.0", false},
		{"TrueNAS-SCALE-22.12.0", false},
		{"unknown-format", true}, // Don't block on unparseable
		{"", true},               // Don't block on empty
	}

	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.supported, isSupportedTrueNASVersion(tc.version))
		})
	}
}
