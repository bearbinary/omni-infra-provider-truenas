package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

func TestData_EncryptedPreserved(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{DefaultPool: "tank"}
	d := Data{Encrypted: true}
	d.ApplyDefaults(cfg)

	assert.True(t, d.Encrypted, "encrypted flag should be preserved through defaults")
}

func TestData_EncryptedDefaultFalse(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{DefaultPool: "tank"}
	d := Data{}
	d.ApplyDefaults(cfg)

	assert.False(t, d.Encrypted, "encrypted should default to false")
}

func TestGeneratePassphrase_Length(t *testing.T) {
	t.Parallel()

	pass, err := generatePassphrase()
	require.NoError(t, err)
	assert.Len(t, pass, 64, "should be 32 bytes = 64 hex chars")
}

func TestGeneratePassphrase_Unique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for range 100 {
		pass, err := generatePassphrase()
		require.NoError(t, err)
		assert.False(t, seen[pass], "passphrase collision detected")
		seen[pass] = true
	}
}

func TestGeneratePassphrase_HexEncoded(t *testing.T) {
	t.Parallel()

	pass, err := generatePassphrase()
	require.NoError(t, err)

	for _, c := range pass {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"passphrase should be hex-encoded, got char %q", string(c))
	}
}

func TestGeneratePassphrase_NotAllZeros(t *testing.T) {
	t.Parallel()

	pass, err := generatePassphrase()
	require.NoError(t, err)

	allZeros := true
	for _, c := range pass {
		if c != '0' {
			allZeros = false
			break
		}
	}

	assert.False(t, allZeros, "passphrase should not be all zeros")
}

func TestPassphraseProperty_Constant(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "org.omni:passphrase", passphraseProperty)
}

// --- Encryption orchestration tests ---

func TestEncryption_NewZvol_PassphraseStoredInProperties(t *testing.T) {
	t.Parallel()

	var createdProps json.RawMessage
	p := testProvisioner(func(method string, params json.RawMessage) (any, error) {
		if method == "pool.dataset.create" {
			createdProps = params
			return &client.Dataset{ID: "tank/omni-vms/test"}, nil
		}
		return nil, nil
	})

	passphrase, err := generatePassphrase()
	require.NoError(t, err)

	omniProps := client.OmniManagedProperties("test-req")
	omniProps = append(omniProps, client.UserProperty{Key: passphraseProperty, Value: passphrase})

	_, err = p.client.CreateEncryptedZvol(context.Background(), "tank/omni-vms/test", 40, passphrase, omniProps)
	require.NoError(t, err)

	// Verify passphrase property was included in the create request
	assert.Contains(t, string(createdProps), `"key":"org.omni:passphrase"`)
	assert.Contains(t, string(createdProps), fmt.Sprintf(`"value":"%s"`, passphrase))
}

func TestEncryption_ExistingZvol_RetrievesPassphraseAndUnlocks(t *testing.T) {
	t.Parallel()

	var unlockPassphrase string
	p := NewProvisioner(client.NewMockClient(func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "pool.dataset.create":
			return nil, &client.APIError{Code: client.ErrCodeExists, Message: "already exists"}
		case "pool.dataset.query":
			return map[string]any{
				"locked": true,
				"user_properties": map[string]any{
					"org.omni:passphrase": map[string]any{"value": "stored-secret-from-zfs"},
					"org.omni:managed":    map[string]any{"value": "true"},
				},
			}, nil
		case "pool.dataset.unlock":
			unlockPassphrase = string(params)
			return nil, nil
		}
		return nil, nil
	}), ProviderConfig{DefaultPool: "tank"})

	// Step 1: Create fails (already exists)
	passphrase, _ := generatePassphrase()
	_, err := p.client.CreateEncryptedZvol(context.Background(), "tank/test", 40, passphrase)
	require.Error(t, err)

	// Step 2: Retrieve stored passphrase
	stored, err := p.client.GetDatasetUserProperty(context.Background(), "tank/test", passphraseProperty)
	require.NoError(t, err)
	assert.Equal(t, "stored-secret-from-zfs", stored)

	// Step 3: Check locked
	locked, err := p.client.IsDatasetLocked(context.Background(), "tank/test")
	require.NoError(t, err)
	assert.True(t, locked)

	// Step 4: Unlock with STORED passphrase (not the generated one)
	err = p.client.UnlockDataset(context.Background(), "tank/test", stored)
	require.NoError(t, err)
	assert.Contains(t, unlockPassphrase, "stored-secret-from-zfs",
		"should unlock with the stored passphrase, not the freshly generated one")
}

func TestEncryption_ExistingZvol_NoStoredPassphrase_Errors(t *testing.T) {
	t.Parallel()

	p := NewProvisioner(client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "pool.dataset.create":
			return nil, &client.APIError{Code: client.ErrCodeExists, Message: "already exists"}
		case "pool.dataset.query":
			return map[string]any{
				"user_properties": map[string]any{
					"org.omni:managed": map[string]any{"value": "true"},
				},
			}, nil
		}
		return nil, nil
	}), ProviderConfig{DefaultPool: "tank"})

	// Create fails (exists)
	passphrase, _ := generatePassphrase()
	_, err := p.client.CreateEncryptedZvol(context.Background(), "tank/test", 10, passphrase)
	require.Error(t, err)

	// Retrieve passphrase — empty (not stored)
	stored, err := p.client.GetDatasetUserProperty(context.Background(), "tank/test", passphraseProperty)
	require.NoError(t, err)
	assert.Empty(t, stored, "should have no stored passphrase")
}

func TestEncryption_ExistingZvol_AlreadyUnlocked_SkipsUnlock(t *testing.T) {
	t.Parallel()

	unlockCalled := false
	p := NewProvisioner(client.NewMockClient(func(method string, _ json.RawMessage) (any, error) {
		switch method {
		case "pool.dataset.create":
			return nil, &client.APIError{Code: client.ErrCodeExists, Message: "already exists"}
		case "pool.dataset.query":
			return map[string]any{
				"locked": false,
				"user_properties": map[string]any{
					"org.omni:passphrase": map[string]any{"value": "stored-secret"},
				},
			}, nil
		case "pool.dataset.unlock":
			unlockCalled = true
			return nil, nil
		}
		return nil, nil
	}), ProviderConfig{DefaultPool: "tank"})

	passphrase, _ := generatePassphrase()
	_, _ = p.client.CreateEncryptedZvol(context.Background(), "tank/test", 10, passphrase)

	locked, _ := p.client.IsDatasetLocked(context.Background(), "tank/test")
	assert.False(t, locked, "zvol should be unlocked")
	assert.False(t, unlockCalled, "should not call unlock when already unlocked")
}
