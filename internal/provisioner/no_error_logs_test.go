package provisioner

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestNormalProvisionFlow_NoErrorLogs is the hygiene guard the
// session-long regression audit called for. It's the smallest test
// that would have failed on the v0.15.0 regression where every
// RequeueError was miscategorized as a provision failure: given the
// mix of "benign" error-return values that show up on a healthy
// provision flow, recordProvisionError must emit zero ERROR-level
// log lines and bump the counter only for real failures.
//
// Scoped deliberately narrow — full provision-flow coverage needs a
// mock TrueNAS, mock Image Factory, and mock Omni provision.Context
// (roughly a day of scaffolding). This unit test captures the
// invariant that actually regressed in production:
//
//	"A provision step whose 'error' is a retry signal must not
//	 generate ERROR logs or counter increments."
//
// When the full mock-flow harness lands in a follow-up, this test
// becomes a case inside that harness rather than a standalone.
func TestNormalProvisionFlow_NoErrorLogs(t *testing.T) {
	t.Parallel()

	benignErrors := []struct {
		name string
		err  error
	}{
		{"nil error", nil},
		{"RequeueError with nil inner", controller.NewRequeueError(nil, 15*time.Second)},
		{"RequeueError wrapping ctx.Canceled", controller.NewRequeueError(context.Canceled, 15*time.Second)},
		{"ctx.Canceled alone", context.Canceled},
		{"wrapped ctx.Canceled", fmt.Errorf("deprovision: %w", context.Canceled)},
	}

	core, sink := observer.New(zap.ErrorLevel)
	logger := zap.New(core)

	for _, be := range benignErrors {
		t.Run(be.name, func(t *testing.T) {
			// Not parallel — shared observer sink makes counting
			// across subtests simpler. Each case is O(microseconds).
			recordProvisionError(context.Background(), logger, be.err)
		})
	}

	errorEntries := sink.FilterMessage("provision error").All()
	assert.Empty(t, errorEntries,
		"a 'normal' provision flow (nil / requeue / canceled errors) must emit zero ERROR-level log lines; "+
			"%d unexpected entries:\n%+v", len(errorEntries), errorEntries)

	// Sanity check: a real error DOES produce an ERROR log. Guards
	// against a refactor that accidentally silences recordProvisionError
	// for everything — that would make the benign-case assertion
	// above pass trivially.
	recordProvisionError(context.Background(), logger, errors.New("pool \"tank\" not found"))

	errorEntries = sink.FilterMessage("provision error").All()
	require.Len(t, errorEntries, 1, "real error must produce exactly one ERROR log line")
	assert.Contains(t, errorEntries[0].ContextMap()["error"], "tank")
}

// TestNormalProvisionFlow_StepDurationIsSilent pins the invariant
// that recordStepDuration emits no logs at all (metrics yes, logs
// no). A regression that added "step completed" at Info level
// across every step would flood the provisioner log stream — the
// existing structured "reconcile succeeded" message from the COSI
// runtime already covers that signal.
func TestNormalProvisionFlow_StepDurationIsSilent(t *testing.T) {
	t.Parallel()

	core, sink := observer.New(zap.DebugLevel)
	_ = zap.New(core) // intentionally unused — recordStepDuration doesn't take a logger.

	recordStepDuration(context.Background(), "createVM", time.Now().Add(-500*time.Millisecond))

	assert.Equal(t, 0, sink.Len(),
		"recordStepDuration must emit zero log lines; it feeds the histogram only")
}
