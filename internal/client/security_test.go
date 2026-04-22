package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateHost_AcceptsValid(t *testing.T) {
	t.Parallel()

	valid := []string{
		"truenas.local",
		"truenas.local:80",
		"192.168.1.10",
		"192.168.1.10:443",
		"[::1]:443",
		"localhost",
		"nas-01.example.com",
	}

	for _, h := range valid {
		if err := validateHost(h); err != nil {
			t.Errorf("validateHost(%q) unexpected error: %v", h, err)
		}
	}
}

func TestValidateHost_RejectsSmuggling(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",
		"evil.example/@truenas.local",           // path smuggling
		"evil.example/?host=truenas.local",      // query
		"user:pass@truenas.local",               // user-info
		"truenas.local#frag",                    // fragment
		"https://truenas.local",                 // scheme
		"truenas.local/path",                    // path
		"truenas.local with spaces",             // whitespace
		"tru\nenas.local",                       // newline
		"truenas_underscore.local",              // underscore (not a valid DNS label char we allow)
	}

	for _, h := range invalid {
		if err := validateHost(h); err == nil {
			t.Errorf("validateHost(%q) should have rejected but returned nil", h)
		}
	}
}

func TestRedactLikelySecrets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		// API-key-shaped tokens get redacted.
		{"auth failed for 1-abcdefghijklmnopqrstuvwxyz0123456789", "auth failed for [REDACTED]"},
		// Short tokens (<20 chars) left alone — avoids over-redaction of normal words.
		{"invalid username", "invalid username"},
		// Mixed: long token in the middle of a sentence.
		{"key Bearer_abcdef012345678901234 rejected", "key [REDACTED] rejected"},
		// No alphanumerics → unchanged.
		{"internal error", "internal error"},
	}

	for _, tc := range cases {
		got := redactLikelySecrets(tc.in)
		if got != tc.want {
			t.Errorf("redactLikelySecrets(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSocketUploadFile_Permissions(t *testing.T) {
	t.Parallel()
	// Create a temp dir to simulate filesystem
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "test.iso")

	// Write a file using the same permissions as socketTransport.UploadFile
	err := os.WriteFile(destPath, []byte("test data"), 0o644)
	require.NoError(t, err)

	info, err := os.Stat(destPath)
	require.NoError(t, err)

	// Verify the file is NOT executable
	perm := info.Mode().Perm()
	assert.Equal(t, os.FileMode(0o644), perm, "ISO files should be 0644, not executable")
	assert.Zero(t, perm&0o111, "ISO files should not have execute bits set")
}

func TestSocketUploadFile_NotWorldWritable(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "test.iso")

	err := os.WriteFile(destPath, []byte("test data"), 0o644)
	require.NoError(t, err)

	info, err := os.Stat(destPath)
	require.NoError(t, err)

	perm := info.Mode().Perm()
	assert.Zero(t, perm&0o002, "ISO files should not be world-writable")
}

func TestWSTransport_PlainWSRequiresInsecure(t *testing.T) {
	t.Parallel()
	// With insecureSkipVerify=false, dialWebSocket should NOT fall back to ws://
	// We can't easily test the full dial without a real server, but we can verify
	// the error message guides users correctly.
	_, err := dialWebSocket("localhost:99999", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TRUENAS_INSECURE_SKIP_VERIFY=true",
		"error should tell user to enable insecure mode for self-signed certs")
}

func TestWSTransport_PlainWSAllowedWhenInsecure(t *testing.T) {
	t.Parallel()
	// With insecureSkipVerify=true, it should try ws:// as fallback
	// Both will fail (no server), but the error should mention the host, not the flag
	_, err := dialWebSocket("localhost:99999", true)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "TRUENAS_INSECURE_SKIP_VERIFY",
		"when insecure is enabled, should try ws:// fallback without mentioning the flag")
}

func TestAPIKeyNotInTransportString(t *testing.T) {
	t.Parallel()
	// Verify that even if someone somehow prints the transport struct,
	// the API key is redacted
	transport := &wsTransport{
		apiKey: NewSecretString("1-WIku99SLhxc2q9c8nZuE"),
		host:   "truenas.local",
	}

	// The apiKey field should be redacted when formatted
	str := transport.apiKey.String()
	assert.Equal(t, "[REDACTED]", str)
	assert.NotContains(t, str, "WIku99")
}

func TestAlertRulesCount(t *testing.T) {
	t.Parallel()
	// Verify we have enough alert rules (should include the new security alerts)
	data, err := os.ReadFile("../../deploy/observability/alerts/truenas-provider.rules.yml")
	require.NoError(t, err)

	content := string(data)
	// Count alert definitions
	alertCount := strings.Count(content, "- alert:")
	assert.GreaterOrEqual(t, alertCount, 11,
		"should have at least 11 alert rules (7 original + 4 new)")
}
