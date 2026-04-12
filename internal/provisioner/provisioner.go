// Package provisioner implements the TrueNAS infra provider core.
package provisioner

import (
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
	MaxErrorRecoveries      int           // Max consecutive ERROR state recoveries before deprovisioning a VM (default: 5, negative=disable)
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

	// Circuit breaker: track consecutive ERROR recoveries per VM ID
	errorMu     sync.Mutex
	errorCounts map[int]int
}

// NewProvisioner creates a new TrueNAS provisioner.
func NewProvisioner(c *client.Client, cfg ProviderConfig) *Provisioner {
	if cfg.MaxErrorRecoveries == 0 {
		cfg.MaxErrorRecoveries = 5
	}

	return &Provisioner{
		client:         c,
		config:         cfg,
		activeImageIDs: make(map[string]bool),
		activeVMNames:  make(map[string]bool),
		errorCounts:    make(map[int]int),
	}
}

// recordVMError increments the consecutive error count for a VM.
// Returns the new count.
func (p *Provisioner) recordVMError(vmID int) int {
	p.errorMu.Lock()
	defer p.errorMu.Unlock()

	p.errorCounts[vmID]++

	return p.errorCounts[vmID]
}

// clearVMErrors resets the error count for a VM (called when VM reaches RUNNING).
func (p *Provisioner) clearVMErrors(vmID int) {
	p.errorMu.Lock()
	defer p.errorMu.Unlock()

	delete(p.errorCounts, vmID)
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
