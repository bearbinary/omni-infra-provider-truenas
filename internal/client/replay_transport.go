package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
)

// ReplayTransport plays back recorded JSON-RPC interactions from a cassette.
// It matches sequentially by method name — each Call advances a cursor.
// Optionally asserts that params match the recording (see SetStrictParams).
type ReplayTransport struct {
	cassette     *Cassette
	cursor       int
	mu           sync.Mutex
	t            *testing.T
	strictParams bool
}

// SetStrictParams enables params-equality checking against the cassette.
// Off by default because many tests randomize VM names / request IDs and
// cassettes encode those random values at record time. Turn on for tests
// that feed deterministic inputs — it catches regressions where a caller
// silently changes the filter/body sent to TrueNAS.
func (t *ReplayTransport) SetStrictParams(strict bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.strictParams = strict
}

// NewReplayTransport loads a cassette and creates a replay transport.
func NewReplayTransport(t *testing.T, cassettePath string) *ReplayTransport {
	t.Helper()

	cassette, err := LoadCassette(cassettePath)
	if err != nil {
		t.Fatalf("failed to load cassette: %v", err)
	}

	t.Logf("Replaying cassette: %s (TrueNAS %s, recorded %s, %d interactions)",
		cassettePath, cassette.TrueNASVersion, cassette.RecordedAt.Format("2006-01-02"), len(cassette.Interactions))

	return &ReplayTransport{
		cassette: cassette,
		t:        t,
	}
}

func (t *ReplayTransport) Name() string { return "replay" }
func (t *ReplayTransport) Close() error { return nil }

// Call replays the next recorded interaction.
func (t *ReplayTransport) Call(_ context.Context, method string, params any, result any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cursor >= len(t.cassette.Interactions) {
		t.t.Fatalf("replay: exhausted all %d interactions, but got call to %q",
			len(t.cassette.Interactions), method)
	}

	interaction := t.cassette.Interactions[t.cursor]
	t.cursor++

	if interaction.Method != method {
		t.t.Fatalf("replay: interaction %d expected method %q, got %q",
			t.cursor-1, interaction.Method, method)
	}

	// Assert params when the recording captured them and strict mode is on.
	// Matches the *shape* of the JSON payload (field order doesn't matter) so
	// a caller that reorders struct fields or renames Go types still replays
	// if the wire form is the same. Catches regressions where a caller
	// silently changes the filter passed to TrueNAS (e.g., vm.query narrowing)
	// and the test still passes.
	if t.strictParams && len(interaction.Params) > 0 {
		gotJSON, err := json.Marshal(normalizeParams(params))
		if err != nil {
			t.t.Fatalf("replay: interaction %d (%s) failed to marshal params for comparison: %v",
				t.cursor-1, method, err)
		}

		if !jsonEqual(gotJSON, interaction.Params) {
			t.t.Fatalf("replay: interaction %d (%s) params mismatch\n  want: %s\n   got: %s",
				t.cursor-1, method, string(interaction.Params), string(gotJSON))
		}
	}

	if interaction.Error != nil {
		return &APIError{Code: interaction.Error.Code, Message: interaction.Error.Message}
	}

	if result != nil && interaction.Result != nil {
		if err := json.Unmarshal(interaction.Result, result); err != nil {
			t.t.Fatalf("replay: interaction %d (%s) failed to unmarshal result: %v",
				t.cursor-1, method, err)
		}
	}

	return nil
}

// jsonEqual compares two JSON byte slices by structural value (order-insensitive for objects).
func jsonEqual(a, b []byte) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}

	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}

	ac, err := json.Marshal(av)
	if err != nil {
		return false
	}

	bc, err := json.Marshal(bv)
	if err != nil {
		return false
	}

	return string(ac) == string(bc)
}

// UploadFile replays a recorded upload (always succeeds unless the recording had an error).
func (t *ReplayTransport) UploadFile(_ context.Context, _ string, _ io.Reader, _ int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cursor >= len(t.cassette.Interactions) {
		t.t.Fatalf("replay: exhausted all %d interactions during UploadFile", len(t.cassette.Interactions))
	}

	interaction := t.cassette.Interactions[t.cursor]
	t.cursor++

	if !interaction.Upload {
		t.t.Fatalf("replay: interaction %d expected upload, got method %q", t.cursor-1, interaction.Method)
	}

	if interaction.Error != nil {
		return fmt.Errorf("%s", interaction.Error.Message)
	}

	return nil
}

// AssertAllConsumed fails the test if not all interactions were replayed.
func (t *ReplayTransport) AssertAllConsumed(tb *testing.T) {
	tb.Helper()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cursor < len(t.cassette.Interactions) {
		tb.Errorf("replay: only consumed %d of %d interactions — test made fewer API calls than recorded",
			t.cursor, len(t.cassette.Interactions))
	}
}
