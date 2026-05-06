package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateProbeHost_RejectsSmugglingShapes covers the bearer-exfil
// vectors documented in the SAST finding. Anything that would let
// fmt.Sprintf("https://%s/_upload/", host) target a different host than
// the operator typed must be rejected before the request goes out.
func TestValidateProbeHost_RejectsSmugglingShapes(t *testing.T) {
	t.Parallel()

	bad := []string{
		"",                       // empty
		"good.tld@evil.tld",      // userinfo-as-host
		"evil.tld/x",             // embedded path
		"evil.tld?x",             // query
		"evil.tld#x",             // fragment
		"https://evil.tld",       // scheme prefix
		"good.tld good.tld",      // whitespace
		"good.tld\nLocation: /x", // CRLF injection
	}

	for _, h := range bad {
		t.Run(h, func(t *testing.T) {
			err := validateProbeHost(h)
			require.Error(t, err, "host %q must be rejected", h)
		})
	}
}

func TestValidateProbeHost_AcceptsBareHostAndHostPort(t *testing.T) {
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
			require.NoError(t, validateProbeHost(h))
		})
	}
}

// TestUploadClient_RefusesRedirect proves that the production-path
// Bearer token does not get re-sent to a redirect target across every
// 3xx class. The original SAST report cited 302 as the exemplar; 307
// and 308 are the dangerous classes — they preserve method AND body, so
// a compromised TrueNAS returning a 307 not only re-emits the
// Authorization header but re-POSTs the multipart payload to the
// attacker's host. CheckRedirect returns ErrUseLastResponse
// unconditionally so all of these short-circuit before Go's default
// re-emit, but pinning each class keeps the regression guard honest.
func TestUploadClient_RefusesRedirect(t *testing.T) {
	t.Parallel()

	for _, code := range []int{
		http.StatusMovedPermanently,  // 301
		http.StatusFound,             // 302
		http.StatusSeeOther,          // 303
		http.StatusTemporaryRedirect, // 307 — preserves method + body
		http.StatusPermanentRedirect, // 308 — preserves method + body
	} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()

			var sawAuthOnRedirectTarget bool
			var sawBodyOnRedirectTarget bool

			redirectTarget := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
					sawAuthOnRedirectTarget = true
				}
				if r.ContentLength > 0 {
					sawBodyOnRedirectTarget = true
				}
			}))
			defer redirectTarget.Close()

			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, redirectTarget.URL+"/captured", code)
			}))
			defer origin.Close()

			req, err := http.NewRequest(http.MethodPost, origin.URL+"/_upload/", strings.NewReader("payload"))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer test-secret-key")

			resp, err := newProbeUploadClient().Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, code, resp.StatusCode,
				"client must surface the %d itself, not auto-follow", code)
			assert.False(t, sawAuthOnRedirectTarget,
				"CheckRedirect failed: Bearer token leaked to redirect target on %d", code)
			assert.False(t, sawBodyOnRedirectTarget,
				"CheckRedirect failed: request body re-emitted to redirect target on %d", code)
		})
	}
}
