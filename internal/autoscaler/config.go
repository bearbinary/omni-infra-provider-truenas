package autoscaler

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// SubcommandConfig is the process-level configuration for the autoscaler
// subcommand. Values are sourced from environment variables so the
// deploy manifest (Helm chart in `deploy/autoscaler/`) has a single
// config surface to set.
//
// Kept separate from Config (the per-MachineClass annotation-sourced
// struct) because these are deploy-time decisions the operator makes
// once per autoscaler Deployment, while annotations are per-MachineSet
// policy that can change at runtime without a restart.
type SubcommandConfig struct {
	// ClusterName is the Omni cluster whose MachineSets this autoscaler
	// manages. Required. Matches rothgar's OMNI_CLUSTER_NAME env var so
	// operators migrating between the standalone PoC and this vendored
	// build don't need to re-learn config keys. Each autoscaler
	// Deployment targets one cluster — matches Cluster Autoscaler's
	// assumption and the external-gRPC interface.
	ClusterName string

	// ListenAddress is the gRPC bind address the cluster-autoscaler
	// sidecar dials. Default ":8086" matches rothgar's upstream default
	// — a deliberate compatibility choice so the sidecar's
	// --cloud-provider-flag can stay identical between PoC and
	// vendored builds.
	ListenAddress string

	// RefreshInterval is how often the autoscaler re-scans Omni for
	// MachineClass annotation changes. Low enough that operator edits
	// take effect without a restart; high enough to not hammer the
	// Omni API. Default 60s.
	RefreshInterval time.Duration
}

// Environment variable names consumed by LoadSubcommandConfig. Exported
// so docs/autoscaler.md can reference them without string duplication.
const (
	EnvClusterName     = "OMNI_CLUSTER_NAME"
	EnvListenAddress   = "AUTOSCALER_LISTEN_ADDRESS"
	EnvRefreshInterval = "AUTOSCALER_REFRESH_INTERVAL"
)

// Defaults applied when the corresponding env var is unset.
const (
	DefaultListenAddress   = ":8086"
	DefaultRefreshInterval = 60 * time.Second
)

// LoadSubcommandConfig parses the autoscaler subcommand's environment
// configuration. Returns a fully-populated SubcommandConfig or an error
// describing exactly which env var was missing/malformed.
//
// Reads but does NOT unset the env vars — unlike the provisioner's
// secret env handling, nothing here is sensitive (cluster name,
// listen address, refresh interval are all safe to leave in /proc
// environ).
func LoadSubcommandConfig() (*SubcommandConfig, error) {
	cluster := strings.TrimSpace(os.Getenv(EnvClusterName))
	if cluster == "" {
		return nil, fmt.Errorf("%s is required — set it to the Omni cluster this autoscaler should manage", EnvClusterName)
	}

	listen := os.Getenv(EnvListenAddress)
	if listen == "" {
		listen = DefaultListenAddress
	}

	refresh, err := parseOptionalDuration(EnvRefreshInterval, DefaultRefreshInterval)
	if err != nil {
		return nil, err
	}

	return &SubcommandConfig{
		ClusterName:     cluster,
		ListenAddress:   listen,
		RefreshInterval: refresh,
	}, nil
}

func parseOptionalDuration(envVar string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(envVar))
	if raw == "" {
		return fallback, nil
	}

	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", envVar, raw, err)
	}

	if d <= 0 {
		return 0, fmt.Errorf("%s %q: must be a positive duration", envVar, raw)
	}

	return d, nil
}
