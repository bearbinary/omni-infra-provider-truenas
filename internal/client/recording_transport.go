package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

// sensitiveFieldNames is the set of JSON field-name SUBSTRINGS the recorder
// will redact wherever they appear. Substring matching is deliberate: real
// payloads have credentials under namespaced keys like
// `org.omni:passphrase` or `api_key_header` where exact-match wouldn't
// catch them. False positives (e.g. a harmless `passphrase_policy` field)
// are an acceptable trade; cassettes are for replay, not introspection.
//
// Case-sensitive on purpose — JSON keys on the TrueNAS wire are
// consistently lowercase, and making this case-insensitive would redact
// TLS-related keys like "Transport" that happen to contain "port".
var sensitiveFieldNames = []string{
	"passphrase",
	"password",
	"api_key",
	"apikey",
	"token",
	"secret",
}

// isSensitiveFieldName returns true if name contains any sensitive substring.
func isSensitiveFieldName(name string) bool {
	for _, needle := range sensitiveFieldNames {
		if strings.Contains(name, needle) {
			return true
		}
	}

	return false
}

// sensitiveMethods names JSON-RPC methods whose first positional parameter is
// itself a secret (no nested field to target). All positional params become
// `[REDACTED]` strings in the recorded cassette for these methods.
var sensitiveMethods = map[string]bool{
	"auth.login_with_api_key":   true,
	"auth.login":                true,
	"auth.login_with_token":     true,
	"auth.generate_token":       true,
	"auth.twofactor.set_secret": true,
}

// redactSensitive walks a JSON-encoded value and replaces any sensitive field
// values with the string "[REDACTED]". Returns a NEW byte slice; the input is
// unchanged. Used by the recorder so secrets never land on disk. Never call
// this on bytes that will be sent to TrueNAS — the redaction is lossy.
func redactSensitive(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}

	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		// Not valid JSON — pass through unchanged so the recorder at least
		// captures the malformed envelope.
		return raw
	}

	redacted := walkRedact(v)

	out, err := json.Marshal(redacted)
	if err != nil {
		return raw
	}

	return out
}

// walkRedact recurses into a decoded JSON value, replacing sensitive field
// values with "[REDACTED]". Operates on the output of encoding/json which
// produces map[string]any for objects and []any for arrays.
func walkRedact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if isSensitiveFieldName(k) {
				x[k] = "[REDACTED]"

				continue
			}

			x[k] = walkRedact(val)
		}

		return x

	case []any:
		for i, val := range x {
			x[i] = walkRedact(val)
		}

		return x
	}

	return v
}

// redactMethodParams scrubs the params of a recorded call. For methods whose
// first positional param IS the secret, every positional param is blanked;
// otherwise the sensitive-field walker runs.
func redactMethodParams(method string, params json.RawMessage) json.RawMessage {
	if sensitiveMethods[method] {
		// Shape on the wire is [secret] or [secret, opts...]. Replace every
		// element with a placeholder so param count stays honest for replay
		// shape-checks but no credential bytes remain.
		var arr []any
		if err := json.Unmarshal(params, &arr); err == nil {
			for i := range arr {
				arr[i] = "[REDACTED]"
			}

			if out, mErr := json.Marshal(arr); mErr == nil {
				return out
			}
		}

		return json.RawMessage(`["[REDACTED]"]`)
	}

	return redactSensitive(params)
}

// RecordingTransport wraps a real Transport, passes all calls through, and
// records the JSON-RPC interactions for later replay in CI.
type RecordingTransport struct {
	inner    Transport
	mu       sync.Mutex
	cassette *Cassette
}

// NewRecordingTransport wraps a real transport with recording.
func NewRecordingTransport(inner Transport) *RecordingTransport {
	return &RecordingTransport{
		inner: inner,
		cassette: &Cassette{
			RecordedAt: time.Now().UTC(),
		},
	}
}

func (t *RecordingTransport) Name() string { return "recording(" + t.inner.Name() + ")" }
func (t *RecordingTransport) Close() error { return t.inner.Close() }

// Call delegates to the real transport and records the interaction.
// Request/response payloads pass through redactMethodParams / redactSensitive
// before being saved so secrets never reach disk.
func (t *RecordingTransport) Call(ctx context.Context, method string, params any, result any) error {
	rawParams, _ := json.Marshal(normalizeParams(params))

	err := t.inner.Call(ctx, method, params, result)

	interaction := Interaction{
		Method: method,
		Params: redactMethodParams(method, rawParams),
	}

	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			interaction.Error = &RecordedError{Code: apiErr.Code, Message: apiErr.Message}
		} else {
			interaction.Error = &RecordedError{Code: -1, Message: err.Error()}
		}
	} else if result != nil {
		if resultBytes, mErr := json.Marshal(result); mErr == nil {
			interaction.Result = redactSensitive(resultBytes)
		}
	}

	t.mu.Lock()
	t.cassette.Interactions = append(t.cassette.Interactions, interaction)

	// Capture TrueNAS version from the first system.version call
	if method == "system.version" && err == nil && t.cassette.TrueNASVersion == "" {
		var version string
		if result != nil {
			if data, marshalErr := json.Marshal(result); marshalErr == nil {
				_ = json.Unmarshal(data, &version)
			}
		}

		t.cassette.TrueNASVersion = version
	}

	t.mu.Unlock()

	return err
}

// UploadFile delegates to the real transport and records the upload.
func (t *RecordingTransport) UploadFile(ctx context.Context, destPath string, data io.Reader, size int64) error {
	err := t.inner.UploadFile(ctx, destPath, data, size)

	interaction := Interaction{
		Method: "filesystem.put",
		Upload: true,
	}

	if err != nil {
		interaction.Error = &RecordedError{Code: -1, Message: err.Error()}
	}

	t.mu.Lock()
	t.cassette.Interactions = append(t.cassette.Interactions, interaction)
	t.mu.Unlock()

	return err
}

// Save writes the recorded cassette to disk.
func (t *RecordingTransport) Save(path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	return SaveCassette(path, t.cassette)
}
