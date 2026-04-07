package provisioner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestData_EncryptedPreserved(t *testing.T) {
	cfg := ProviderConfig{DefaultPool: "tank"}

	d := Data{Encrypted: true}
	d.ApplyDefaults(cfg)

	assert.True(t, d.Encrypted, "encrypted flag should be preserved through defaults")
}

func TestData_EncryptedDefaultFalse(t *testing.T) {
	cfg := ProviderConfig{DefaultPool: "tank"}

	d := Data{}
	d.ApplyDefaults(cfg)

	assert.False(t, d.Encrypted, "encrypted should default to false")
}

func TestProviderConfig_EncryptionPassphrase(t *testing.T) {
	p := NewProvisioner(nil, ProviderConfig{
		DefaultPool:          "tank",
		EncryptionPassphrase: "test-secret",
	})

	assert.Equal(t, "test-secret", p.config.EncryptionPassphrase)
}

func TestProviderConfig_NoPassphrase(t *testing.T) {
	p := NewProvisioner(nil, ProviderConfig{
		DefaultPool: "tank",
	})

	assert.Empty(t, p.config.EncryptionPassphrase)
}
