package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsumeSecretEnv_UnsetsAfterRead pins the defense-in-depth invariant
// that secret env vars leave no trace in os.Environ() after capture. Code
// reading from /proc/<pid>/environ, core dumps, or child-process inheritance
// cannot recover a value that's been consumed.
func TestConsumeSecretEnv_UnsetsAfterRead(t *testing.T) {
	// Cannot t.Parallel — t.Setenv mutates process env.
	t.Setenv("TEST_SECRET_VAR", "rot13-is-not-encryption")

	got := consumeSecretEnv("TEST_SECRET_VAR")
	assert.Equal(t, "rot13-is-not-encryption", got)

	// Critical assertion: env var is gone.
	_, stillSet := os.LookupEnv("TEST_SECRET_VAR")
	assert.False(t, stillSet, "env var must be unset after consumeSecretEnv")

	// Second call returns empty and is a no-op.
	again := consumeSecretEnv("TEST_SECRET_VAR")
	assert.Equal(t, "", again)
}

// TestConsumeSecretEnv_MissingReturnsEmpty ensures calling the helper on a
// never-set var doesn't panic and returns an empty string so callers can
// use the result in SecretString wrappers without a nil-check.
func TestConsumeSecretEnv_MissingReturnsEmpty(t *testing.T) {
	// Guarantee the test var is unset before we start.
	_ = os.Unsetenv("A_VAR_THAT_IS_DEFINITELY_NOT_SET_12345")

	got := consumeSecretEnv("A_VAR_THAT_IS_DEFINITELY_NOT_SET_12345")
	assert.Equal(t, "", got)
}

// TestIsLocalOmniEndpoint_TableDriven pins the exhaustive match rules. The
// function is load-bearing: a false positive would let a multi-tenant SaaS
// Omni deployment run without an explicit PROVIDER_ID, causing lease
// collision. A false negative would block localhost dev loops.
func TestIsLocalOmniEndpoint_TableDriven(t *testing.T) {
	t.Parallel()

	cases := []struct {
		endpoint string
		want     bool
	}{
		// Local — various schemes and port forms.
		{"http://localhost:8080", true},
		{"https://localhost", true},
		{"https://localhost:8443", true},
		{"grpc://localhost:50051", true},
		{"http://127.0.0.1:8080", true},
		{"https://127.0.0.2:443", true}, // entire 127.0.0.0/8 is loopback in spirit
		{"grpc://127.0.0.1", true},
		{"http://[::1]:8080", true},
		{"https://[::1]", true},
		{"grpc://[::1]", true},

		// Case-insensitive.
		{"HTTPS://LOCALHOST:9000", true},
		{"Http://127.0.0.1", true},

		// Remote.
		{"https://omni.example.com", false},
		{"https://omni.sidero.dev", false},
		{"http://10.0.0.5:8080", false},
		{"https://192.168.1.100:443", false},
		{"grpc://omni.internal:50051", false},

		// Empty.
		{"", false},

		// Edge: hostname that CONTAINS localhost must not match.
		{"https://localhost-hijacker.example", false},
	}

	for _, tc := range cases {
		got := isLocalOmniEndpoint(tc.endpoint)
		if got != tc.want {
			t.Errorf("isLocalOmniEndpoint(%q) = %t, want %t", tc.endpoint, got, tc.want)
		}
	}
}

// TestIsLocalOmniEndpoint_CorrectlyRejectsSubdomain is a dedicated test for
// the tricky case where the endpoint looks local by prefix but isn't.
// Without proper parsing, a naive HasPrefix check might match
// "https://localhost.evil.example" → true.
func TestIsLocalOmniEndpoint_CorrectlyRejectsDeceptiveSubdomain(t *testing.T) {
	t.Parallel()

	// If this ever returns true, the guard is vulnerable to a deceptive
	// endpoint name tricking the provider into dropping the PROVIDER_ID
	// requirement.
	require.False(t, isLocalOmniEndpoint("https://localhost.attacker.example"),
		"deceptive subdomain must not match as local")
}
