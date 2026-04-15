package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEnvDefaults_SafetyCriticalSettings pins the default value of every
// environment variable whose default has a known runtime safety impact. If
// a future config refactor flips a default, this test fails immediately
// with a pointer to the historical reason.
//
// Singleton enforcement (PROVIDER_SINGLETON_ENABLED=true) is the highest-
// stakes default — if it flips to false, two providers can race on every
// MachineRequest and produce duplicate VMs / failed zvol creates / stranded
// state. Per CHANGELOG v0.13.0 this is intentionally a hard requirement.
//
// NOTE: this test cannot use t.Parallel at either level — it calls t.Setenv
// to scrub the developer's shell env, which Go forbids in parallel tests.
func TestEnvDefaults_SafetyCriticalSettings(t *testing.T) {
	cases := []struct {
		envVar     string
		isBool     bool
		boolWant   bool
		isInt      bool
		intWant    int
		isString   bool
		stringWant string // sentinel passed as the default when probing envString
		stringHas  string // substring the returned default must contain
		why        string
	}{
		{
			envVar:   "PROVIDER_SINGLETON_ENABLED",
			isBool:   true,
			boolWant: true,
			why:      "two provider instances racing on the same MachineRequests cause duplicate VMs and failed zvol creates — must default ON. Per v0.13.0 CHANGELOG, this is a hard safety requirement.",
		},
		{
			envVar:   "TRUENAS_INSECURE_SKIP_VERIFY",
			isBool:   true,
			boolWant: false,
			why:      "TLS verification must default ON — production deployments connect to remote TrueNAS over public networks; turning verification off silently exposes API key in flight to MITM. Local-on-TrueNAS deployments override to true explicitly.",
		},
		{
			envVar:   "OMNI_INSECURE_SKIP_VERIFY",
			isBool:   true,
			boolWant: false,
			why:      "TLS to Omni must default ON — same MITM rationale as TrueNAS. Should never silently disable.",
		},
		{
			envVar:     "OTEL_EXPORTER_OTLP_PROTOCOL",
			isString:   true,
			stringWant: defaultOTELProtocol,
			stringHas:  "grpc",
			why:        "default protocol must remain documented — gRPC is the historical default; users opt into http/protobuf for Grafana Cloud.",
		},
		{
			envVar:  "GRACEFUL_SHUTDOWN_TIMEOUT",
			isInt:   true,
			intWant: 30,
			why:     "30s ACPI graceful shutdown is enough for Talos to flush etcd and shut down kubelet without data loss; lower defaults risk corruption on Deprovision.",
		},
		{
			envVar:  "MAX_ERROR_RECOVERIES",
			isInt:   true,
			intWant: 5,
			why:     "5 consecutive failed recoveries before auto-replace circuit breaker fires — too low (1) and transient errors trigger churn; too high (none) and stuck VMs accumulate.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.envVar, func(t *testing.T) {
			// Scrub the developer's shell env so the default-value path is
			// exercised. t.Setenv with "" isn't equivalent to unset, but
			// envBool/envInt/envString treat empty the same as missing, so
			// that's fine here. t.Setenv restores the prior value at cleanup
			// and forbids t.Parallel — so the outer test can't be parallel
			// either.
			t.Setenv(tc.envVar, "")

			switch {
			case tc.isBool:
				got := envBool(tc.envVar, tc.boolWant)
				assert.Equal(t, tc.boolWant, got, "default for %s changed.\nWhy this matters: %s", tc.envVar, tc.why)
			case tc.isInt:
				got := envInt(tc.envVar, tc.intWant)
				assert.Equal(t, tc.intWant, got, "default for %s changed.\nWhy this matters: %s", tc.envVar, tc.why)
			case tc.isString:
				got := envString(tc.envVar, tc.stringWant)
				assert.Equal(t, tc.stringWant, got, "default for %s changed.\nWhy this matters: %s", tc.envVar, tc.why)
				assert.True(t, strings.Contains(got, tc.stringHas), "default for %s missing %q.\nWhy this matters: %s", tc.envVar, tc.stringHas, tc.why)
			}
		})
	}
}
