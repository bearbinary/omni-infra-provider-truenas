package client

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeParams_Nil(t *testing.T) {
	t.Parallel()
	result := normalizeParams(nil)
	arr, ok := result.([]any)
	require.True(t, ok, "nil should become empty array")
	assert.Empty(t, arr)
}

func TestNormalizeParams_SliceAny(t *testing.T) {
	t.Parallel()
	input := []any{"hello", 42}
	result := normalizeParams(input)
	assert.Equal(t, input, result, "[]any should pass through unchanged")
}

func TestNormalizeParams_SliceMap(t *testing.T) {
	t.Parallel()
	input := []map[string]any{{"key": "val"}}
	result := normalizeParams(input)
	assert.Equal(t, input, result, "[]map should pass through unchanged")
}

func TestNormalizeParams_SliceString(t *testing.T) {
	t.Parallel()
	input := []string{"a", "b"}
	result := normalizeParams(input)
	assert.Equal(t, input, result, "[]string should pass through unchanged")
}

func TestNormalizeParams_SliceInt(t *testing.T) {
	t.Parallel()
	input := []int{1, 2, 3}
	result := normalizeParams(input)
	assert.Equal(t, input, result, "[]int should pass through unchanged")
}

func TestNormalizeParams_SingleStruct(t *testing.T) {
	t.Parallel()
	input := struct {
		Name string `json:"name"`
	}{Name: "test"}

	result := normalizeParams(input)
	arr, ok := result.([]any)
	require.True(t, ok, "single struct should be wrapped in array")
	require.Len(t, arr, 1)

	// Verify JSON serialization works
	data, err := json.Marshal(arr)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"name":"test"`)
}

func TestNormalizeParams_SingleMap(t *testing.T) {
	t.Parallel()
	input := map[string]any{"key": "val"}
	result := normalizeParams(input)
	arr, ok := result.([]any)
	require.True(t, ok, "single map should be wrapped in array")
	require.Len(t, arr, 1)
}

func TestNormalizeParams_SingleString(t *testing.T) {
	t.Parallel()
	result := normalizeParams("hello")
	arr, ok := result.([]any)
	require.True(t, ok, "single string should be wrapped in array")
	assert.Equal(t, []any{"hello"}, arr)
}

func TestNormalizeParams_SingleInt(t *testing.T) {
	t.Parallel()
	result := normalizeParams(42)
	arr, ok := result.([]any)
	require.True(t, ok, "single int should be wrapped in array")
	assert.Equal(t, []any{42}, arr)
}
