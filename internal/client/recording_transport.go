package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

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
func (t *RecordingTransport) Call(ctx context.Context, method string, params any, result any) error {
	rawParams, _ := json.Marshal(normalizeParams(params))

	err := t.inner.Call(ctx, method, params, result)

	interaction := Interaction{
		Method: method,
		Params: rawParams,
	}

	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			interaction.Error = &RecordedError{Code: apiErr.Code, Message: apiErr.Message}
		} else {
			interaction.Error = &RecordedError{Code: -1, Message: err.Error()}
		}
	} else if result != nil {
		interaction.Result, _ = json.Marshal(result)
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
