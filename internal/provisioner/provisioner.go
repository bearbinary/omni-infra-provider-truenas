// Package provisioner implements the TrueNAS infra provider core.
package provisioner

import (
	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources"
	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"golang.org/x/sync/singleflight"
)

// ProviderConfig holds the provider-level configuration that applies to all VMs.
type ProviderConfig struct {
	DefaultPool       string
	DefaultNICAttach  string // Bridge, VLAN, or physical interface for VM NICs
	DefaultBootMethod string
}

// Provisioner implements the Omni provision.Provisioner interface for TrueNAS.
type Provisioner struct {
	client *client.Client
	config ProviderConfig
	isoGroup singleflight.Group
}

// NewProvisioner creates a new TrueNAS provisioner.
func NewProvisioner(c *client.Client, cfg ProviderConfig) *Provisioner {
	return &Provisioner{
		client: c,
		config: cfg,
	}
}

// ProvisionSteps returns the ordered provision steps.
func (p *Provisioner) ProvisionSteps() []provision.Step[*resources.Machine] {
	return []provision.Step[*resources.Machine]{
		provision.NewStep("createSchematic", p.stepCreateSchematic),
		provision.NewStep("uploadISO", p.stepUploadISO),
		provision.NewStep("createVM", p.stepCreateVM),
	}
}
