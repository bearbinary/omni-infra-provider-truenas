package truenasrpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateHost_RejectsSmugglingShapes(t *testing.T) {
	t.Parallel()

	bad := []string{
		"",                       // empty
		"good.tld@evil.tld",      // userinfo-as-host
		"evil.tld/x",             // embedded path
		"evil.tld?",              // query
		"evil.tld#",              // fragment
		"https://evil.tld",       // scheme prefix
		"good.tld good.tld",      // whitespace
		"good.tld\tx",            // tab
		"good.tld\rsneak",        // bare CR
		"good.tld\nLocation: /x", // CRLF injection
		"evil_host.tld",          // underscore in DNS label — strict allow-list
		"foo$bar.tld",            // arbitrary special char
	}

	for _, h := range bad {
		t.Run(h, func(t *testing.T) {
			t.Parallel()

			err := ValidateHost(h)
			require.Error(t, err, "host %q must be rejected", h)
		})
	}
}

func TestValidateHost_AcceptsBareHostAndHostPort(t *testing.T) {
	t.Parallel()

	ok := []string{
		"truenas.local",
		"truenas.local:8443",
		"192.168.1.10",
		"192.168.1.10:8443",
		"[::1]:443",
	}

	for _, h := range ok {
		t.Run(h, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, ValidateHost(h))
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"localhost:8080", true},
		{"127.0.0.1", true},
		{"127.0.0.1:443", true},
		{"::1", true},
		{"[::1]:443", true},
		{"example.com", false},
		{"10.0.0.5", false},
		{"localhost-hijacker.example", false}, // not literally "localhost"
	}

	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsLoopbackHost(tc.host))
		})
	}
}

func TestNormalizeParams(t *testing.T) {
	t.Parallel()

	// Nil → empty positional array.
	got := NormalizeParams(nil)
	arr, ok := got.([]any)
	require.True(t, ok)
	assert.Empty(t, arr)

	// Pre-shaped slice passes through.
	in := []any{"a", 1}
	assert.Equal(t, in, NormalizeParams(in))

	// Bare object gets wrapped.
	wrapped, ok := NormalizeParams(map[string]any{"k": "v"}).([]any)
	require.True(t, ok)
	require.Len(t, wrapped, 1)
}
