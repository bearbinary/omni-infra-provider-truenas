package client

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretString_Reveal(t *testing.T) {
	t.Parallel()
	s := NewSecretString("my-api-key-12345")
	assert.Equal(t, "my-api-key-12345", s.Reveal())
}

func TestSecretString_String_Redacted(t *testing.T) {
	t.Parallel()
	s := NewSecretString("super-secret-value")
	assert.Equal(t, "[REDACTED]", s.String())
	// Verify fmt.Sprint uses String()
	assert.Equal(t, "[REDACTED]", fmt.Sprint(s))
	assert.Equal(t, "[REDACTED]", s.String())
	assert.Equal(t, "[REDACTED]", fmt.Sprintf("%v", s))
}

func TestSecretString_GoString_Redacted(t *testing.T) {
	t.Parallel()
	s := NewSecretString("super-secret-value")
	assert.Equal(t, "SecretString{[REDACTED]}", s.GoString())
	assert.Equal(t, "SecretString{[REDACTED]}", fmt.Sprintf("%#v", s))
}

func TestSecretString_MarshalJSON_Redacted(t *testing.T) {
	t.Parallel()
	s := NewSecretString("super-secret-value")
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Equal(t, `"[REDACTED]"`, string(data))
}

func TestSecretString_MarshalJSON_InStruct(t *testing.T) {
	t.Parallel()
	type Config struct {
		APIKey SecretString `json:"api_key"`
		Host   string       `json:"host"`
	}

	cfg := Config{
		APIKey: NewSecretString("1-WIku99SLhxc2q9c8nZuE"),
		Host:   "truenas.local",
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"[REDACTED]"`)
	assert.NotContains(t, string(data), "WIku99")
}

func TestSecretString_IsEmpty(t *testing.T) {
	t.Parallel()
	assert.True(t, NewSecretString("").IsEmpty())
	assert.False(t, NewSecretString("something").IsEmpty())
}

func TestSecretString_NotInErrorMessage(t *testing.T) {
	t.Parallel()
	s := NewSecretString("super-secret-value")
	err := fmt.Errorf("connection failed with key %s", s)
	assert.Contains(t, err.Error(), "[REDACTED]")
	assert.NotContains(t, err.Error(), "super-secret-value")
}
