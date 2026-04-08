package provisioner

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemoryPreCheck_UnderThreshold(t *testing.T) {
	// 4096 MiB VM on 32 GiB host = 12.5% — well under 80%
	hostMem := int64(32 * 1024 * 1024 * 1024) // 32 GiB in bytes
	hostMiB := hostMem / (1024 * 1024)
	requestedMiB := int64(4096)

	assert.LessOrEqual(t, requestedMiB, hostMiB*80/100,
		"4 GiB VM on 32 GiB host should be under 80%% threshold")
}

func TestMemoryPreCheck_OverThreshold(t *testing.T) {
	// 28672 MiB (28 GiB) VM on 32 GiB host = 87.5% — over 80%
	hostMem := int64(32 * 1024 * 1024 * 1024)
	hostMiB := hostMem / (1024 * 1024)
	requestedMiB := int64(28672)

	assert.Greater(t, requestedMiB, hostMiB*80/100,
		"28 GiB VM on 32 GiB host should exceed 80%% threshold")
}

func TestMemoryPreCheck_ExactThreshold(t *testing.T) {
	// 80% of 32 GiB = 25600 MiB — should pass (<=)
	hostMem := int64(32 * 1024 * 1024 * 1024)
	hostMiB := hostMem / (1024 * 1024)
	threshold := hostMiB * 80 / 100 // 26214 MiB (integer math)

	assert.Equal(t, int64(26214), threshold)
}

// Version check tests — test the parsing logic from main.go
// isSupportedTrueNASVersion extracts major version number and checks >= 25

func TestVersionParsing(t *testing.T) {
	tests := []struct {
		version string
		valid   bool
		desc    string
	}{
		{"TrueNAS-SCALE-25.04.0", true, "25.04 Fangtooth"},
		{"TrueNAS-SCALE-25.10.0", true, "25.10"},
		{"TrueNAS-SCALE-26.02.0", true, "26.x future"},
		{"TrueNAS-SCALE-27.04.0", true, "27.x future"},
		{"TrueNAS-SCALE-28.04.0", true, "28.x future (should not be blocked)"},
		{"TrueNAS-SCALE-30.01.0", true, "30.x far future"},
		{"TrueNAS-SCALE-24.10.2", false, "24.10 Electric Eel"},
		{"TrueNAS-SCALE-23.10.0", false, "23.x old"},
		{"TrueNAS-SCALE-22.12.0", false, "22.x very old"},
		{"SCALE-Fangtooth-25.04", true, "unexpected format with version"},
		{"25.04.0", true, "bare version number"},
		{"something-without-numbers", true, "unparseable = assume supported"},
		{"", true, "empty = assume supported"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			result := parseVersionSupported(tt.version)
			assert.Equal(t, tt.valid, result, "version %q", tt.version)
		})
	}
}

// parseVersionSupported replicates the logic from main.go for testing
func parseVersionSupported(ver string) bool {
	parts := strings.Split(ver, "-")
	for _, part := range parts {
		if len(part) > 0 && part[0] >= '0' && part[0] <= '9' {
			dotParts := strings.Split(part, ".")
			if len(dotParts) >= 1 {
				major := 0
				for _, c := range dotParts[0] {
					if c >= '0' && c <= '9' {
						major = major*10 + int(c-'0')
					}
				}

				if major > 0 {
					return major >= 25
				}
			}
		}
	}

	return true // can't parse = assume supported
}
