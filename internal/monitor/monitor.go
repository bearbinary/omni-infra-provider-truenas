// Package monitor periodically collects TrueNAS host health metrics
// and publishes them as OTEL gauges.
package monitor

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/telemetry"
)

// Config holds monitor configuration.
type Config struct {
	Interval time.Duration // How often to collect metrics (default: 30s)
}

// Monitor periodically collects TrueNAS host health and publishes OTEL metrics.
type Monitor struct {
	client *client.Client
	config Config
	logger *zap.Logger
}

// New creates a new host health monitor.
func New(c *client.Client, cfg Config, logger *zap.Logger) *Monitor {
	if cfg.Interval == 0 {
		cfg.Interval = 30 * time.Second
	}

	return &Monitor{
		client: c,
		config: cfg,
		logger: logger.Named("monitor"),
	}
}

// Run starts the periodic metrics collection loop. Blocks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()

	// Collect once immediately
	m.collect(ctx)

	for {
		select {
		case <-ticker.C:
			m.collect(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (m *Monitor) collect(ctx context.Context) {
	m.collectHostInfo(ctx)
	m.collectPoolInfo(ctx)
	m.collectDiskInfo(ctx)
	m.collectVMInfo(ctx)
}

func (m *Monitor) collectHostInfo(ctx context.Context) {
	info, err := m.client.GetHostInfo(ctx)
	if err != nil {
		m.logger.Debug("failed to collect host info", zap.Error(err))

		return
	}

	if telemetry.HostCPUCores != nil {
		telemetry.HostCPUCores.Record(ctx, int64(info.Cores))
	}

	if telemetry.HostMemoryTotal != nil {
		telemetry.HostMemoryTotal.Record(ctx, info.Physmem)
	}
}

func (m *Monitor) collectPoolInfo(ctx context.Context) {
	pools, err := m.client.ListPools(ctx)
	if err != nil {
		m.logger.Debug("failed to collect pool info", zap.Error(err))

		return
	}

	for _, p := range pools {
		poolAttr := telemetry.WithPool(p.Name)

		if telemetry.HostPoolFreeBytes != nil {
			telemetry.HostPoolFreeBytes.Record(ctx, p.Free, poolAttr)
		}

		if telemetry.HostPoolUsedBytes != nil {
			telemetry.HostPoolUsedBytes.Record(ctx, p.Used, poolAttr)
		}

		if telemetry.HostPoolHealthy != nil {
			healthy := int64(0)
			if p.Healthy {
				healthy = 1
			}

			telemetry.HostPoolHealthy.Record(ctx, healthy, poolAttr)
		}
	}
}

func (m *Monitor) collectDiskInfo(ctx context.Context) {
	disks, err := m.client.ListDisks(ctx)
	if err != nil {
		m.logger.Debug("failed to collect disk info", zap.Error(err))

		return
	}

	if telemetry.HostDisksTotal != nil {
		telemetry.HostDisksTotal.Record(ctx, int64(len(disks)))
	}
}

func (m *Monitor) collectVMInfo(ctx context.Context) {
	vms, err := m.client.ListVMs(ctx)
	if err != nil {
		m.logger.Debug("failed to collect VM info", zap.Error(err))

		return
	}

	running := 0
	for _, vm := range vms {
		if vm.Status.State == "RUNNING" {
			running++
		}
	}

	if telemetry.HostVMsRunning != nil {
		telemetry.HostVMsRunning.Record(ctx, int64(running))
	}
}

// PoolSelector selects the best pool for a new VM based on available space.
type PoolSelector struct {
	client *client.Client
	logger *zap.Logger
}

// NewPoolSelector creates a pool selector.
func NewPoolSelector(c *client.Client, logger *zap.Logger) *PoolSelector {
	return &PoolSelector{client: c, logger: logger}
}

// SelectPool chooses the best pool for a new VM. If explicitPool is set, validates
// it exists and returns it. Otherwise, selects the pool with the most free space.
func (ps *PoolSelector) SelectPool(ctx context.Context, explicitPool string) (string, error) {
	if explicitPool != "" {
		return explicitPool, nil
	}

	pools, err := ps.client.ListPools(ctx)
	if err != nil {
		return "", err
	}

	var best *client.PoolInfo
	for i := range pools {
		p := &pools[i]
		if !p.Healthy {
			continue
		}

		if best == nil || p.Free > best.Free {
			best = p
		}
	}

	if best == nil {
		return "", fmt.Errorf("no healthy pools available")
	}

	ps.logger.Info("auto-selected pool",
		zap.String("pool", best.Name),
		zap.Int64("free_gib", best.Free/(1024*1024*1024)),
	)

	return best.Name, nil
}
