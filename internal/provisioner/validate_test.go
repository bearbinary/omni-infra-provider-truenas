package provisioner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

func TestValidatePool_Exists(t *testing.T) {
	t.Parallel()
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.query" {
			return []map[string]any{{"name": "tank"}}, nil
		}

		return nil, nil
	})

	err := p.validatePool(context.Background(), "tank")
	require.NoError(t, err)
}

func TestValidatePool_NotFound(t *testing.T) {
	t.Parallel()
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.query" {
			return []map[string]any{}, nil
		}

		return nil, nil
	})

	err := p.validatePool(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found on TrueNAS")
	assert.Contains(t, err.Error(), "not a dataset")
}

func TestValidatePool_DatasetPath(t *testing.T) {
	t.Parallel()
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.query" {
			return []map[string]any{}, nil
		}

		return nil, nil
	})

	err := p.validatePool(context.Background(), "tank/my-dataset")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "looks like a dataset path")
	assert.Contains(t, err.Error(), "not a pool name")
}

func TestValidatePool_APIError(t *testing.T) {
	t.Parallel()
	p := testProvisioner(func(method string, _ json.RawMessage) (any, error) {
		if method == "pool.query" {
			return nil, &client.APIError{Code: 13, Message: "permission denied"}
		}

		return nil, nil
	})

	err := p.validatePool(context.Background(), "tank")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to verify pool")
}
