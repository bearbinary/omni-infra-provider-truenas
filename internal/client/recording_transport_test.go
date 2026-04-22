package client

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactSensitive_ObjectPassphrase(t *testing.T) {
	t.Parallel()

	in := json.RawMessage(`[{"name":"tank/x","encryption_options":{"algorithm":"AES-256-GCM","passphrase":"s3cret"}}]`)
	out := redactSensitive(in)

	assert.NotContains(t, string(out), "s3cret")
	assert.Contains(t, string(out), `"passphrase":"[REDACTED]"`)
	// Non-sensitive siblings preserved.
	assert.Contains(t, string(out), `"algorithm":"AES-256-GCM"`)
}

func TestRedactSensitive_NestedArrays(t *testing.T) {
	t.Parallel()

	in := json.RawMessage(`{"datasets":[{"name":"a","passphrase":"one"},{"name":"b","passphrase":"two"}]}`)
	out := redactSensitive(in)

	assert.NotContains(t, string(out), "one")
	assert.NotContains(t, string(out), "two")
	assert.Equal(t, strings.Count(string(out), "[REDACTED]"), 2)
}

func TestRedactSensitive_PassthroughOnInvalidJSON(t *testing.T) {
	t.Parallel()

	in := json.RawMessage(`{not valid`)
	out := redactSensitive(in)

	assert.Equal(t, string(in), string(out),
		"invalid JSON passes through unchanged so the cassette at least captures the malformed envelope")
}

func TestRedactSensitive_SensitiveKeyVariants(t *testing.T) {
	t.Parallel()

	in := json.RawMessage(`{"api_key":"k","apikey":"k2","token":"t","password":"p","secret":"s","unrelated":"keep"}`)
	out := redactSensitive(in)

	var m map[string]any
	require.NoError(t, json.Unmarshal(out, &m))

	assert.Equal(t, "[REDACTED]", m["api_key"])
	assert.Equal(t, "[REDACTED]", m["apikey"])
	assert.Equal(t, "[REDACTED]", m["token"])
	assert.Equal(t, "[REDACTED]", m["password"])
	assert.Equal(t, "[REDACTED]", m["secret"])
	assert.Equal(t, "keep", m["unrelated"])
}

func TestRedactMethodParams_SensitiveMethod(t *testing.T) {
	t.Parallel()

	// auth.login_with_api_key: first positional param is the api key.
	in := json.RawMessage(`["1-ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"]`)
	out := redactMethodParams("auth.login_with_api_key", in)

	assert.NotContains(t, string(out), "ABCDEFGHIJ")
	assert.Contains(t, string(out), "[REDACTED]")
}

func TestRedactMethodParams_NonSensitiveMethod(t *testing.T) {
	t.Parallel()

	// Non-sensitive method with a nested passphrase field gets field-level redaction.
	in := json.RawMessage(`[{"name":"tank/x","passphrase":"hidden"}]`)
	out := redactMethodParams("pool.dataset.create", in)

	assert.NotContains(t, string(out), "hidden")
	assert.Contains(t, string(out), `"passphrase":"[REDACTED]"`)
	assert.Contains(t, string(out), `"name":"tank/x"`)
}
