package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTransport is a no-frills Transport implementation used to drive
// RecordingTransport + ReplayTransport tests without a real websocket.
type fakeTransport struct {
	calls    []fakeCall
	response any
	respErr  error
}

type fakeCall struct {
	Method string
	Params any
}

func (f *fakeTransport) Name() string { return "fake" }
func (f *fakeTransport) Close() error { return nil }

func (f *fakeTransport) Call(_ context.Context, method string, params, result any) error {
	f.calls = append(f.calls, fakeCall{Method: method, Params: params})

	if f.respErr != nil {
		return f.respErr
	}

	if result == nil || f.response == nil {
		return nil
	}

	data, err := json.Marshal(f.response)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, result)
}

func (f *fakeTransport) UploadFile(_ context.Context, _ string, _ io.Reader, _ int64) error {
	return nil
}

// TestRecordingTransport_E2E_RedactsPassphraseEndToEnd drives a full call
// through the recorder and then inspects the generated cassette bytes to
// prove the passphrase never made it to disk.
func TestRecordingTransport_E2E_RedactsPassphraseEndToEnd(t *testing.T) {
	t.Parallel()

	inner := &fakeTransport{response: map[string]any{"ok": true}}
	rec := NewRecordingTransport(inner)

	params := []any{
		map[string]any{
			"name": "tank/vm-root",
			"type": "VOLUME",
			"encryption_options": map[string]any{
				"algorithm":  "AES-256-GCM",
				"passphrase": "super-secret-passphrase-value",
			},
		},
	}

	err := rec.Call(context.Background(), "pool.dataset.create", params, &map[string]any{})
	require.NoError(t, err)

	// Marshal the full cassette; that's what would land on disk.
	require.Len(t, rec.cassette.Interactions, 1)
	out, err := json.Marshal(rec.cassette)
	require.NoError(t, err)

	assert.NotContains(t, string(out), "super-secret-passphrase-value",
		"recorder must redact passphrase from cassette")
	assert.Contains(t, string(out), "[REDACTED]")
	assert.Contains(t, string(out), "AES-256-GCM",
		"non-sensitive sibling fields must survive")
}

// TestRecordingTransport_E2E_RedactsResultField proves the recorder also
// scrubs passphrases that appear in response bodies (not just request
// params), which can happen when TrueNAS echoes props back on create.
func TestRecordingTransport_E2E_RedactsResultField(t *testing.T) {
	t.Parallel()

	inner := &fakeTransport{response: map[string]any{
		"id": "tank/vm-root",
		"user_properties": map[string]any{
			"org.omni:passphrase": map[string]any{"value": "leaked-into-response"},
		},
	}}
	rec := NewRecordingTransport(inner)

	err := rec.Call(context.Background(), "pool.dataset.query", []any{"tank/vm-root"}, &map[string]any{})
	require.NoError(t, err)

	require.Len(t, rec.cassette.Interactions, 1)
	out, err := json.Marshal(rec.cassette.Interactions[0])
	require.NoError(t, err)

	assert.NotContains(t, string(out), "leaked-into-response",
		"passphrase value appearing in response must be redacted before cassette write")
}

// TestRecordingTransport_E2E_SensitiveMethodWholePayloadBlanked verifies
// that the entire positional-params array is replaced for methods whose
// first argument IS the secret (not a nested field).
func TestRecordingTransport_E2E_SensitiveMethodWholePayloadBlanked(t *testing.T) {
	t.Parallel()

	inner := &fakeTransport{response: true}
	rec := NewRecordingTransport(inner)

	err := rec.Call(context.Background(), "auth.login_with_api_key",
		[]any{"1-ThisIsATotallyRealApiKey1234567890"}, nil)
	require.NoError(t, err)

	require.Len(t, rec.cassette.Interactions, 1)
	out, err := json.Marshal(rec.cassette.Interactions[0])
	require.NoError(t, err)

	assert.NotContains(t, string(out), "ThisIsATotallyReal",
		"api key substring must not survive the recorder")
	assert.Contains(t, string(out), "[REDACTED]")
}

// --- ReplayTransport strict params ---

// TestReplayTransport_StrictParams_Off_IgnoresMismatch pins the default
// behavior: strict params is opt-in, so cassettes with randomized VM names
// keep replaying for backward compatibility.
func TestReplayTransport_StrictParams_Off_IgnoresMismatch(t *testing.T) {
	t.Parallel()

	cassette := &Cassette{
		Interactions: []Interaction{
			{
				Method: "system.info",
				Params: json.RawMessage(`[{"want":"recorded-value"}]`),
				Result: json.RawMessage(`{"ok":true}`),
			},
		},
	}

	r := &ReplayTransport{cassette: cassette, t: t}

	var got map[string]any
	require.NoError(t, r.Call(context.Background(), "system.info", []any{map[string]any{"want": "different-value"}}, &got))
}

// TestReplayTransport_StrictParams_On_CatchesMismatch proves the knob has
// teeth when enabled: mismatched params fail the test loudly.
func TestReplayTransport_StrictParams_On_CatchesMismatch(t *testing.T) {
	t.Parallel()

	cassette := &Cassette{
		Interactions: []Interaction{
			{
				Method: "system.info",
				Params: json.RawMessage(`[{"want":"recorded-value"}]`),
				Result: json.RawMessage(`{"ok":true}`),
			},
		},
	}

	// Use a sub-test with a fatalRecorder so the replay Fatalf doesn't kill
	// our enclosing test.
	ft := &fatalRecorder{}
	r := &ReplayTransport{cassette: cassette, t: ft}
	r.SetStrictParams(true)

	var got map[string]any
	_ = r.Call(context.Background(), "system.info",
		[]any{map[string]any{"want": "different-value"}}, &got)

	require.NotEmpty(t, ft.messages, "strict mode must fail the test on param mismatch")
	assert.Contains(t, ft.messages[0], "params mismatch")
}

// TestReplayTransport_StrictParams_On_OrderInsensitiveMatch verifies the
// JSON comparison is structural: swapping field order in a request still
// matches the recorded shape.
func TestReplayTransport_StrictParams_On_OrderInsensitiveMatch(t *testing.T) {
	t.Parallel()

	cassette := &Cassette{
		Interactions: []Interaction{
			{
				Method: "system.info",
				Params: json.RawMessage(`[{"a":1,"b":2}]`),
				Result: json.RawMessage(`{"ok":true}`),
			},
		},
	}

	r := &ReplayTransport{cassette: cassette, t: t}
	r.SetStrictParams(true)

	var got map[string]any

	// Same values, different insertion order.
	require.NoError(t, r.Call(context.Background(), "system.info",
		[]any{map[string]any{"b": 2, "a": 1}}, &got))
}

// fatalRecorder captures Fatalf / Errorf messages without aborting the
// calling test. Satisfies the testReporter interface.
type fatalRecorder struct {
	messages []string
}

func (f *fatalRecorder) Helper() {}

func (f *fatalRecorder) Fatalf(format string, args ...any) {
	f.messages = append(f.messages, fmtSprintf(format, args...))
}

func (f *fatalRecorder) Errorf(format string, args ...any) {
	f.messages = append(f.messages, fmtSprintf(format, args...))
}

func (f *fatalRecorder) Logf(_ string, _ ...any) {}

// fmtSprintf is a tiny wrapper so the test file doesn't need its own fmt
// import sprinkled through assertions.
func fmtSprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// --- isLoopbackHost ---

func TestIsLoopbackHost_Table(t *testing.T) {
	t.Parallel()

	cases := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"localhost:8080", true},
		{"127.0.0.1", true},
		{"127.0.0.1:8443", true},
		{"::1", true}, // bare IPv6 loopback
		{"[::1]:8080", true},
		{"truenas.local", false},
		{"192.168.1.100", false},
		{"192.168.1.100:8080", false},
		{"10.0.0.1", false},
	}

	for _, tc := range cases {
		got := isLoopbackHost(tc.host)
		if got != tc.want {
			t.Errorf("isLoopbackHost(%q) = %t, want %t", tc.host, got, tc.want)
		}
	}
}

// TestIsLoopbackHost_ReservedHostnames asserts that non-loopback names that
// happen to embed "localhost" as a substring do NOT slip past the check.
func TestIsLoopbackHost_ReservedHostnamesNotSubstringMatched(t *testing.T) {
	t.Parallel()

	assert.False(t, isLoopbackHost("localhost-evil.example"),
		"substring 'localhost' must not match a FQDN that contains it")
	assert.False(t, isLoopbackHost("evil.localhost.example"),
		"a subdomain containing 'localhost' must not match")
	// Guard against strings.Contains-style regressions.
	assert.False(t, isLoopbackHost("not.localhost"),
		"a different host with localhost as a label must not match")
}

// Ensure the helpers don't go unused by lint if tests are pruned.
var _ = strings.Contains
