// Package provisioner implements the TrueNAS infra provider core.
package provisioner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/resources"
	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"golang.org/x/sync/singleflight"
)

// ProviderConfig holds the provider-level configuration that applies to all VMs.
type ProviderConfig struct {
	DefaultPool             string
	DefaultNetworkInterface string // Bridge, VLAN, or physical interface for VM NICs
	DefaultBootMethod       string
	GracefulShutdownTimeout time.Duration // How long to wait for ACPI shutdown before force (default: 30s, 0=force immediately)
	PollInterval            time.Duration // How often to poll VM state during graceful shutdown (default: 2s)
}

// Provisioner implements the Omni provision.Provisioner interface for TrueNAS.
type Provisioner struct {
	client   *client.Client
	config   ProviderConfig
	isoGroup singleflight.Group

	// Track active resources for cleanup
	mu             sync.RWMutex
	activeImageIDs map[string]bool
	activeVMNames  map[string]bool
}

// NewProvisioner creates a new TrueNAS provisioner.
func NewProvisioner(c *client.Client, cfg ProviderConfig) *Provisioner {
	return &Provisioner{
		client:         c,
		config:         cfg,
		activeImageIDs: make(map[string]bool),
		activeVMNames:  make(map[string]bool),
	}
}

// SeedActiveVMs queries TrueNAS for all omni_ prefixed VMs and pre-populates
// the active VM tracking map. This must be called before the cleanup loop starts
// to prevent the cleanup from treating all VMs as orphans after a provider restart.
// Without this, PROVISIONED machines (which don't re-run provision steps) would
// never be tracked and would be deleted on the first cleanup cycle.
func (p *Provisioner) SeedActiveVMs(ctx context.Context) error {
	vms, err := p.client.ListVMs(ctx)
	if err != nil {
		return fmt.Errorf("failed to list VMs for seeding: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, vm := range vms {
		if len(vm.Name) > 5 && vm.Name[:5] == "omni_" {
			p.activeVMNames[vm.Name] = true
		}
	}

	return nil
}

// TrackImageID records an image ID as actively in use.
func (p *Provisioner) TrackImageID(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.activeImageIDs[id] = true
}

// TrackVMName records a VM name as actively tracked by Omni.
func (p *Provisioner) TrackVMName(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.activeVMNames[name] = true
}

// UntrackVMName removes a VM name from tracking (called on deprovision).
func (p *Provisioner) UntrackVMName(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.activeVMNames, name)
}

// ActiveImageIDs returns a snapshot of currently active image IDs.
func (p *Provisioner) ActiveImageIDs() map[string]bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]bool, len(p.activeImageIDs))
	for k, v := range p.activeImageIDs {
		result[k] = v
	}

	return result
}

// ActiveVMNames returns a snapshot of currently active VM names.
func (p *Provisioner) ActiveVMNames() map[string]bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]bool, len(p.activeVMNames))
	for k, v := range p.activeVMNames {
		result[k] = v
	}

	return result
}

// ProvisionSteps returns the ordered provision steps.
func (p *Provisioner) ProvisionSteps() []provision.Step[*resources.Machine] {
	return []provision.Step[*resources.Machine]{
		provision.NewStep("createSchematic", p.stepCreateSchematic),
		provision.NewStep("uploadISO", p.stepUploadISO),
		provision.NewStep("createVM", p.stepCreateVM),
		provision.NewStep("healthCheck", p.stepHealthCheck),
	}
}
