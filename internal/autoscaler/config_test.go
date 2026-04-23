package autoscaler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadSubcommandConfig pins the env-var contract the Helm chart in
// `deploy/autoscaler/` relies on. Each test uses t.Setenv so there's no
// parallelism concern and no env-var bleed between runs. Matches the
// approach taken by the provisioner's secret_env_test.go.
func TestLoadSubcommandConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    *SubcommandConfig
		wantErr string
	}{
		{
			name: "minimum valid config — cluster name only",
			env: map[string]string{
				EnvClusterName: "talos-home",
			},
			want: &SubcommandConfig{
				ClusterName:     "talos-home",
				ListenAddress:   DefaultListenAddress,
				RefreshInterval: DefaultRefreshInterval,
			},
		},
		{
			name: "all overrides respected",
			env: map[string]string{
				EnvClusterName:     "talos-preview",
				EnvListenAddress:   ":9090",
				EnvRefreshInterval: "30s",
			},
			want: &SubcommandConfig{
				ClusterName:     "talos-preview",
				ListenAddress:   ":9090",
				RefreshInterval: 30 * time.Second,
			},
		},
		{
			name: "whitespace-trimmed cluster name",
			env: map[string]string{
				EnvClusterName: "  talos-home  ",
			},
			want: &SubcommandConfig{
				ClusterName:     "talos-home",
				ListenAddress:   DefaultListenAddress,
				RefreshInterval: DefaultRefreshInterval,
			},
		},
		{
			name:    "missing cluster name is fatal",
			env:     map[string]string{},
			wantErr: EnvClusterName,
		},
		{
			name: "empty-string cluster name is fatal",
			env: map[string]string{
				EnvClusterName: "   ",
			},
			wantErr: EnvClusterName,
		},
		{
			name: "malformed refresh interval",
			env: map[string]string{
				EnvClusterName:     "talos-home",
				EnvRefreshInterval: "not-a-duration",
			},
			wantErr: EnvRefreshInterval,
		},
		{
			name: "zero refresh interval is fatal — would cause a hot loop",
			env: map[string]string{
				EnvClusterName:     "talos-home",
				EnvRefreshInterval: "0s",
			},
			wantErr: "must be a positive duration",
		},
		{
			name: "negative refresh interval is fatal",
			env: map[string]string{
				EnvClusterName:     "talos-home",
				EnvRefreshInterval: "-5s",
			},
			wantErr: "must be a positive duration",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// NOT t.Parallel() — t.Setenv forbids it.
			for _, k := range []string{EnvClusterName, EnvListenAddress, EnvRefreshInterval} {
				t.Setenv(k, "")
			}

			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			got, err := LoadSubcommandConfig()

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
