package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Cassette holds a sequence of recorded JSON-RPC interactions for replay in CI.
type Cassette struct {
	TrueNASVersion string        `json:"truenas_version"`
	RecordedAt     time.Time     `json:"recorded_at"`
	Interactions   []Interaction `json:"interactions"`
}

// Interaction is a single JSON-RPC request/response pair.
type Interaction struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RecordedError  `json:"error,omitempty"`
	Upload bool            `json:"upload,omitempty"`
}

// RecordedError captures a TrueNAS API error for replay.
type RecordedError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// LoadCassette reads a cassette file from disk.
func LoadCassette(path string) (*Cassette, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read cassette %s: %w", path, err)
	}

	var c Cassette
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to parse cassette %s: %w", path, err)
	}

	return &c, nil
}

// SaveCassette writes a cassette to disk, creating parent directories as needed.
func SaveCassette(path string, c *Cassette) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create cassette directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cassette: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write cassette %s: %w", path, err)
	}

	return nil
}
