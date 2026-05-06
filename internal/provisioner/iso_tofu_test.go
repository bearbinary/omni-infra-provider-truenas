package provisioner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/client"
)

// --- classifyTOFU decision table ---

func TestClassifyTOFU_EmptyStoredIsFirstUse(t *testing.T) {
	t.Parallel()

	assert.Equal(t, tofuFirstUse, classifyTOFU("", "abc"))
}

func TestClassifyTOFU_ExactMatch(t *testing.T) {
	t.Parallel()

	assert.Equal(t, tofuMatch, classifyTOFU("abc", "abc"))
}

func TestClassifyTOFU_Mismatch(t *testing.T) {
	t.Parallel()

	assert.Equal(t, tofuMismatch, classifyTOFU("abc", "def"))
}

func TestClassifyTOFU_PoisonedPrefixShortCircuits(t *testing.T) {
	t.Parallel()

	// Poisoned outcome fires whether the downloaded hash matches the poison
	// marker's tail or not — poison is permanent until operator cleanup.
	cases := []struct {
		stored, downloaded string
	}{
		{poisonMarker("badhash"), "badhash"},
		{poisonMarker("badhash"), "anything"},
		{poisonMarker(""), "anything"},
	}

	for _, tc := range cases {
		assert.Equal(t, tofuPoisoned, classifyTOFU(tc.stored, tc.downloaded),
			"stored=%q downloaded=%q should classify as poisoned", tc.stored, tc.downloaded)
	}
}

func TestCachedISOPoisoned(t *testing.T) {
	t.Parallel()

	assert.True(t, cachedISOPoisoned("POISONED-abc"))
	assert.True(t, cachedISOPoisoned("POISONED-"))
	assert.False(t, cachedISOPoisoned(""))
	assert.False(t, cachedISOPoisoned("abc"))
	assert.False(t, cachedISOPoisoned("poisoned-abc"), "prefix is case-sensitive on purpose")
}

func TestPoisonMarker_Format(t *testing.T) {
	t.Parallel()

	m := poisonMarker("abc123")
	assert.True(t, strings.HasPrefix(m, poisonedPrefix))
	assert.True(t, strings.HasSuffix(m, "abc123"))
	// Round-trip: a constructed poison marker must classify as poisoned.
	assert.Equal(t, tofuPoisoned, classifyTOFU(m, "abc123"))
}

// --- Integration via MockClient: observable side effects ---

// tofuMockClient tracks which dataset-user-property calls were made. Mocks
// pool.dataset.query (read) and pool.dataset.update (write) for the ISO hash
// property, plus the minimal calls the step emits around them.
type tofuMockClient struct {
	mu        sync.Mutex
	props     map[string]string // key: property name, value: stored string
	setCalls  []struct{ Key, Value string }
	updateErr error
}

func (m *tofuMockClient) handler(method string, params json.RawMessage) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch method {
	case "pool.dataset.query":
		// Return all tracked properties; GetDatasetUserProperty picks the one it wants.
		userProps := map[string]any{}
		for k, v := range m.props {
			userProps[k] = map[string]any{"value": v}
		}

		return map[string]any{"user_properties": userProps}, nil

	case "pool.dataset.update":
		if m.updateErr != nil {
			return nil, m.updateErr
		}

		// Extract the user_properties_update entry.
		var args []any
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, err
		}

		if len(args) < 2 {
			return nil, nil
		}

		upd, _ := args[1].(map[string]any)
		ups, _ := upd["user_properties_update"].([]any)

		for _, e := range ups {
			entry, _ := e.(map[string]any)
			k, _ := entry["key"].(string)
			v, _ := entry["value"].(string)
			m.props[k] = v
			m.setCalls = append(m.setCalls, struct{ Key, Value string }{k, v})
		}

		return nil, nil
	}

	return nil, nil
}

// newTOFUClient wires a MockClient into a real *client.Client so our helper
// code under test hits the mock's handler through the normal RPC path.
func newTOFUClient(t *testing.T) (*client.Client, *tofuMockClient) {
	t.Helper()

	m := &tofuMockClient{props: map[string]string{}}
	return client.NewMockClient(m.handler), m
}

func TestSetDatasetUserProperty_WritesValue(t *testing.T) {
	t.Parallel()

	c, m := newTOFUClient(t)
	ctx := context.Background()

	require.NoError(t, c.SetDatasetUserProperty(ctx, "tank/talos-iso", "org.omni:iso-sha256-xyz", "deadbeef"))

	m.mu.Lock()
	defer m.mu.Unlock()
	require.Len(t, m.setCalls, 1)
	assert.Equal(t, "org.omni:iso-sha256-xyz", m.setCalls[0].Key)
	assert.Equal(t, "deadbeef", m.setCalls[0].Value)
}

func TestGetDatasetUserProperty_ReturnsStoredValue(t *testing.T) {
	t.Parallel()

	c, m := newTOFUClient(t)
	ctx := context.Background()

	// Seed the mock.
	m.mu.Lock()
	m.props["org.omni:iso-sha256-xyz"] = "deadbeef"
	m.mu.Unlock()

	got, err := c.GetDatasetUserProperty(ctx, "tank/talos-iso", "org.omni:iso-sha256-xyz")
	require.NoError(t, err)
	assert.Equal(t, "deadbeef", got)
}

func TestGetDatasetUserProperty_MissingReturnsEmpty(t *testing.T) {
	t.Parallel()

	c, _ := newTOFUClient(t)
	ctx := context.Background()

	got, err := c.GetDatasetUserProperty(ctx, "tank/talos-iso", "org.omni:iso-sha256-never-set")
	require.NoError(t, err)
	assert.Equal(t, "", got, "missing property returns empty string so callers can distinguish first-use")
}

// --- verifyISOMetadata decision table ---

func TestVerifyISOMetadata_LegacyEntryClassifiesAsFirstUse(t *testing.T) {
	t.Parallel()

	// Provider versions before stat-detection only recorded the hash. The
	// cache-hit path must still accept those entries instead of treating
	// the missing fields as a tamper signal.
	result, err := verifyISOMetadata("", "", &client.FileInfo{Size: 1024, Mtime: 1.0})
	require.NoError(t, err)
	assert.Equal(t, tofuFirstUse, result)
}

func TestVerifyISOMetadata_NilStatIsMismatch(t *testing.T) {
	t.Parallel()

	// FileExists said "yes" but StatFile got nothing — race on a deleted
	// file. The safe answer is mismatch, not silent acceptance.
	result, err := verifyISOMetadata("100", "1.0", nil)
	assert.Equal(t, tofuMismatch, result)
	assert.ErrorContains(t, err, "disappeared")
}

func TestVerifyISOMetadata_SizeMatchMtimeMatchIsMatch(t *testing.T) {
	t.Parallel()

	result, err := verifyISOMetadata("125829120", "1766353846.6033258",
		&client.FileInfo{Size: 125_829_120, Mtime: 1766353846.6033258})
	require.NoError(t, err)
	assert.Equal(t, tofuMatch, result)
}

func TestVerifyISOMetadata_SizeDriftIsMismatch(t *testing.T) {
	t.Parallel()

	result, err := verifyISOMetadata("125829120", "1.0",
		&client.FileInfo{Size: 999, Mtime: 1.0})
	assert.Equal(t, tofuMismatch, result)
	assert.ErrorContains(t, err, "size drift")
}

func TestVerifyISOMetadata_MtimeDriftIsMismatch(t *testing.T) {
	t.Parallel()

	// Drift large enough to be representable in float64 at this magnitude
	// (float64 carries ~15–17 significant decimal digits, so a sub-ns delta
	// against a 10-digit unix epoch can quantize to zero — a realistic
	// touch-mediated tamper would shift mtime by seconds, not ns).
	result, err := verifyISOMetadata("100", "1766353846.6033258",
		&client.FileInfo{Size: 100, Mtime: 1766353900.0}) // ~54s drift
	assert.Equal(t, tofuMismatch, result)
	assert.ErrorContains(t, err, "mtime drift")
}

func TestVerifyISOMetadata_GarbageStoredSizeIsMismatch(t *testing.T) {
	t.Parallel()

	result, err := verifyISOMetadata("not-a-number", "1.0",
		&client.FileInfo{Size: 100, Mtime: 1.0})
	assert.Equal(t, tofuMismatch, result)
	assert.ErrorContains(t, err, "valid int64")
}

func TestVerifyISOMetadata_AsymmetricStoredFieldsAreMismatch(t *testing.T) {
	t.Parallel()

	// Asymmetric (size recorded, mtime missing — or vice versa) is
	// "partial baseline = suspicious" rather than "treat as legacy". A
	// future refactor that decides "any empty field → tofuFirstUse"
	// would silently weaken the guarantee. Pinning the choice here so
	// the change is loud.
	cases := []struct {
		name        string
		storedSize  string
		storedMtime string
	}{
		{"size-only", "100", ""},
		{"mtime-only", "", "1.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := verifyISOMetadata(tc.storedSize, tc.storedMtime,
				&client.FileInfo{Size: 100, Mtime: 1.0})
			assert.Equal(t, tofuMismatch, result, "partial recorded baseline must classify as mismatch, not first-use")
			require.Error(t, err)
		})
	}
}

func TestFormatParseRoundTrip_Mtime(t *testing.T) {
	t.Parallel()

	cases := []float64{
		0,
		1,
		1766353846.6033258, // TrueNAS sub-second epoch
		9999999999.9999999,
	}

	for _, in := range cases {
		s := formatISOMtime(in)
		got, err := parseISOMtime(s)
		require.NoError(t, err, "round-trip parse must succeed for %v", in)
		assert.Equal(t, in, got, "format/parse must be bit-stable for %v", in)
	}
}

// --- setIfPoisonable retry behavior ---

type recordingPoisonSetter struct {
	calls   atomic.Int32
	failFor int           // first N calls fail
	failErr error         // sentinel error returned during failures
	delay   time.Duration // optional sleep on each call to test ctx-cancel path
}

func (r *recordingPoisonSetter) SetDatasetUserProperty(ctx context.Context, _, _, _ string) error {
	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	n := r.calls.Add(1)
	if int(n) <= r.failFor {
		return r.failErr
	}

	return nil
}

func TestSetIfPoisonable_FirstAttemptSuccessNoRetry(t *testing.T) {
	t.Parallel()

	r := &recordingPoisonSetter{}
	logger := zaptest.NewLogger(t)

	persisted := setIfPoisonable(context.Background(), logger, r, "tank/iso", "key", "POISONED-x", "img-1", "/mnt/tank/iso/x.iso")

	assert.True(t, persisted, "first-attempt success must report persisted=true")
	assert.Equal(t, int32(1), r.calls.Load(), "no retry on first-attempt success")
}

func TestSetIfPoisonable_RetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	// Boundary table: failFor 0..poisonRetryAttempts-1 must all eventually
	// persist. Pinning each row separately so an off-by-one in the loop
	// counter is loud whichever direction it drifts.
	cases := []int{0, 1, 2}

	for _, failFor := range cases {
		t.Run(fmt.Sprintf("failFor=%d", failFor), func(t *testing.T) {
			t.Parallel()

			r := &recordingPoisonSetter{failFor: failFor, failErr: errors.New("transient ws hangup")}
			logger := zaptest.NewLogger(t)

			persisted := setIfPoisonable(context.Background(), logger, r, "tank/iso", "key", "POISONED-x", "img-1", "/mnt/tank/iso/x.iso")

			assert.True(t, persisted, "should ultimately persist on failFor=%d", failFor)
			assert.Equal(t, int32(failFor+1), r.calls.Load(), "should call exactly failFor+1 times")
		})
	}
}

func TestSetIfPoisonable_AllAttemptsFailLogsManualCleanup(t *testing.T) {
	t.Parallel()

	r := &recordingPoisonSetter{failFor: poisonRetryAttempts, failErr: errors.New("permanent")}

	core, observed := observer.New(zap.ErrorLevel)
	logger := zap.New(core)

	persisted := setIfPoisonable(context.Background(), logger, r, "tank/iso", "key", "POISONED-x", "img-1", "/mnt/tank/iso/x.iso")

	assert.False(t, persisted, "exhausted retries must report persisted=false so caller can mention it in the outer error")
	assert.Equal(t, int32(poisonRetryAttempts), r.calls.Load(), "exhausts retries")

	entries := observed.FilterMessageSnippet("MANUAL CLEANUP REQUIRED").All()
	require.Len(t, entries, 1, "must emit exactly one MANUAL CLEANUP error so alerting can fire on it")
	assert.Equal(t, zap.ErrorLevel, entries[0].Level)
	assert.Contains(t, entries[0].ContextMap()["iso_path"], "/mnt/tank/iso/x.iso",
		"the cleanup log must carry the path so the operator can find the file")
}

func TestSetIfPoisonable_ContextCancellationStopsRetries(t *testing.T) {
	t.Parallel()

	r := &recordingPoisonSetter{
		failFor: poisonRetryAttempts,
		failErr: errors.New("transient"),
		delay:   50 * time.Millisecond,
	}
	logger := zaptest.NewLogger(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	persisted := setIfPoisonable(ctx, logger, r, "tank/iso", "key", "POISONED-x", "img-1", "/mnt/tank/iso/x.iso")

	// First attempt blocks on the delay-vs-ctx select and bails immediately
	// with ctx.Err. No further retries should fire.
	assert.False(t, persisted)
	assert.LessOrEqual(t, r.calls.Load(), int32(1),
		"a cancelled context should not allow further retries")
}

// TestSetIfPoisonable_ContextCanceledMidRetry exercises mid-flight
// cancellation (vs the pre-cancelled case above): the inter-attempt sleep
// is the place a `time.Sleep` regression would silently re-introduce. A
// future refactor that replaces the `select { <-timer.C: <-ctx.Done() }`
// with a bare sleep would block past cancel and fire all retries — this
// test caps total elapsed under poisonRetryDelay so that regression is
// loud.
func TestSetIfPoisonable_ContextCanceledMidRetry(t *testing.T) {
	t.Parallel()

	r := &recordingPoisonSetter{
		failFor: poisonRetryAttempts,
		failErr: errors.New("transient"),
	}
	logger := zaptest.NewLogger(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	persisted := setIfPoisonable(ctx, logger, r, "tank/iso", "key", "POISONED-x", "img-1", "/mnt/tank/iso/x.iso")
	elapsed := time.Since(start)

	assert.False(t, persisted)
	assert.Less(t, r.calls.Load(), int32(poisonRetryAttempts),
		"mid-flight cancel must short-circuit before all attempts fire")
	assert.Less(t, elapsed, poisonRetryDelay*time.Duration(poisonRetryAttempts-1),
		"mid-flight cancel must not wait the full inter-retry budget")
}

// --- verifyCachedISO integration through a tofuMockClient ---
//
// The cache-hit branch in stepUploadISO is the entire reason the SAST fix
// exists; without these tests, a regression that re-discards the property
// read error or short-circuits the metadata check ships green. Each row
// of the table exercises one production code path the original SAST
// findings called out: legacy first-use re-record, RPC failure on the
// property read, RPC failure on the stat, drift detection, and poison
// classification.

// tofuStatMockClient extends tofuMockClient with a filesystem.stat
// handler so verifyCachedISO can drive both halves of its contract from
// the same test fixture.
type tofuStatMockClient struct {
	tofuMockClient

	// statResult, statErr drive the response of filesystem.stat. nil
	// statResult + nil statErr → "not found" via APIError, matching the
	// shape Client.StatFile returns (StatFile maps NotFound to nil info).
	statResult *client.FileInfo
	statErr    error

	// queryErr lets a test simulate a pool.dataset.query (property read)
	// RPC failure, the captured-error case the original SAST report
	// blamed for silently rotating the TOFU baseline.
	queryErr error
}

func (m *tofuStatMockClient) handler(method string, params json.RawMessage) (any, error) {
	switch method {
	case "filesystem.stat":
		if m.statErr != nil {
			return nil, m.statErr
		}
		if m.statResult == nil {
			return nil, &client.APIError{Code: client.ErrCodeNotFound, Message: "not found"}
		}
		return map[string]any{
			"size":  m.statResult.Size,
			"mtime": m.statResult.Mtime,
			"type":  "FILE",
		}, nil
	case "pool.dataset.query":
		if m.queryErr != nil {
			return nil, m.queryErr
		}
	}

	return m.tofuMockClient.handler(method, params)
}

func newProvisionerWithTOFUMock(t *testing.T, m *tofuStatMockClient) *Provisioner {
	t.Helper()

	if m.props == nil {
		m.props = map[string]string{}
	}

	c := client.NewMockClient(m.handler)
	return NewProvisioner(c, ProviderConfig{DefaultPool: "default"})
}

func TestVerifyCachedISO_Match(t *testing.T) {
	t.Parallel()

	m := &tofuStatMockClient{
		tofuMockClient: tofuMockClient{props: map[string]string{
			"org.omni:iso-sha256-img1": "expected-hash",
			"org.omni:iso-size-img1":   "100",
			"org.omni:iso-mtime-img1":  "1.0",
		}},
	}
	p := newProvisionerWithTOFUMock(t, m)

	stat := &client.FileInfo{Size: 100, Mtime: 1.0}
	err := p.verifyCachedISO(context.Background(), zaptest.NewLogger(t),
		"tank/talos-iso", "/mnt/tank/talos-iso/x.iso", "img1",
		"org.omni:iso-sha256-img1", "org.omni:iso-size-img1", "org.omni:iso-mtime-img1",
		stat)

	require.NoError(t, err, "match must succeed without writing anything")

	m.mu.Lock()
	defer m.mu.Unlock()
	assert.Empty(t, m.setCalls, "match path must not write any properties")
}

func TestVerifyCachedISO_LegacyEntryRerecordsMetadata(t *testing.T) {
	t.Parallel()

	// Hash recorded but no size/mtime → tofuFirstUse → re-record now so
	// the next cache hit gets the full triple. This is the regression
	// guard for the "legacy entry never upgrades" finding from QA F12.
	m := &tofuStatMockClient{
		tofuMockClient: tofuMockClient{props: map[string]string{
			"org.omni:iso-sha256-img1": "expected-hash",
		}},
	}
	p := newProvisionerWithTOFUMock(t, m)

	stat := &client.FileInfo{Size: 12345, Mtime: 1766353846.6033258}
	err := p.verifyCachedISO(context.Background(), zaptest.NewLogger(t),
		"tank/talos-iso", "/mnt/tank/talos-iso/x.iso", "img1",
		"org.omni:iso-sha256-img1", "org.omni:iso-size-img1", "org.omni:iso-mtime-img1",
		stat)

	require.NoError(t, err)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Two writes: size + mtime. Hash is already recorded.
	wroteSize := false
	wroteMtime := false
	for _, c := range m.setCalls {
		switch c.Key {
		case "org.omni:iso-size-img1":
			wroteSize = true
			assert.Equal(t, "12345", c.Value)
		case "org.omni:iso-mtime-img1":
			wroteMtime = true
			assert.Equal(t, "1766353846.6033258", c.Value)
		}
	}
	assert.True(t, wroteSize, "legacy first-use must re-record size on observation")
	assert.True(t, wroteMtime, "legacy first-use must re-record mtime on observation")
}

func TestVerifyCachedISO_PropertyReadFailureRefuses(t *testing.T) {
	t.Parallel()

	// The captured-error fix from the original SAST report. A
	// pool.dataset.query that fails RPC must NOT silently degrade to
	// "no recorded hash" → "first use" → "trust whatever is on disk".
	// If this regresses, an attacker who can induce one failed RPC at
	// the right moment rotates the TOFU baseline to bytes of their
	// choosing.
	m := &tofuStatMockClient{
		queryErr: errors.New("ws hangup"),
	}
	p := newProvisionerWithTOFUMock(t, m)

	stat := &client.FileInfo{Size: 100, Mtime: 1.0}
	err := p.verifyCachedISO(context.Background(), zaptest.NewLogger(t),
		"tank/talos-iso", "/mnt/tank/talos-iso/x.iso", "img1",
		"org.omni:iso-sha256-img1", "org.omni:iso-size-img1", "org.omni:iso-mtime-img1",
		stat)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to reuse cached bytes")
	assert.ErrorContains(t, err, "ws hangup", "underlying RPC error must wrap into the outer error so on-call can correlate")
}

func TestVerifyCachedISO_MetadataDriftPoisons(t *testing.T) {
	t.Parallel()

	m := &tofuStatMockClient{
		tofuMockClient: tofuMockClient{props: map[string]string{
			"org.omni:iso-sha256-img1": "expected-hash",
			"org.omni:iso-size-img1":   "100",
			"org.omni:iso-mtime-img1":  "1.0",
		}},
	}
	p := newProvisionerWithTOFUMock(t, m)

	stat := &client.FileInfo{Size: 999, Mtime: 1.0} // size drift
	err := p.verifyCachedISO(context.Background(), zaptest.NewLogger(t),
		"tank/talos-iso", "/mnt/tank/talos-iso/x.iso", "img1",
		"org.omni:iso-sha256-img1", "org.omni:iso-size-img1", "org.omni:iso-mtime-img1",
		stat)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "metadata re-verification")

	m.mu.Lock()
	defer m.mu.Unlock()

	// Hash property must now be POISON-marked.
	hashWrite := false
	for _, c := range m.setCalls {
		if c.Key == "org.omni:iso-sha256-img1" {
			hashWrite = true
			assert.True(t, strings.HasPrefix(c.Value, poisonedPrefix),
				"mismatch must POISON-mark the recorded hash so a future cache hit refuses")
			assert.Contains(t, c.Value, "expected-hash",
				"poison marker must carry the recorded hash so operator can identify the bytes that drifted")
		}
	}
	assert.True(t, hashWrite, "expected SetDatasetUserProperty for hash with POISON marker")
}

func TestVerifyCachedISO_PoisonMarkedRefusesFast(t *testing.T) {
	t.Parallel()

	// Stored hash is already POISON-marked. Must short-circuit before
	// any metadata work — the file is known-tainted regardless of size/mtime.
	m := &tofuStatMockClient{
		tofuMockClient: tofuMockClient{props: map[string]string{
			"org.omni:iso-sha256-img1": poisonMarker("badhash"),
		}},
	}
	p := newProvisionerWithTOFUMock(t, m)

	stat := &client.FileInfo{Size: 100, Mtime: 1.0}
	err := p.verifyCachedISO(context.Background(), zaptest.NewLogger(t),
		"tank/talos-iso", "/mnt/tank/talos-iso/x.iso", "img1",
		"org.omni:iso-sha256-img1", "org.omni:iso-size-img1", "org.omni:iso-mtime-img1",
		stat)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "POISONED")

	m.mu.Lock()
	defer m.mu.Unlock()
	assert.Empty(t, m.setCalls, "poison-marked refusal must not write anything new")
}

func TestReverifyISOBeforeAttach_MissingFileRefuses(t *testing.T) {
	t.Parallel()

	// File disappeared between cache-hit verification and CDROM attach.
	// The provisioner must NOT attach a CDROM pointing at a vanished
	// path — that would fail at boot with a confusing libvirt-relayed
	// error rather than a clear supply-chain refusal.
	m := &tofuStatMockClient{
		// statResult nil + statErr nil → mock returns NotFound (cache miss shape).
		tofuMockClient: tofuMockClient{props: map[string]string{
			"org.omni:iso-sha256-img1": "expected-hash",
			"org.omni:iso-size-img1":   "100",
			"org.omni:iso-mtime-img1":  "1.0",
		}},
	}
	p := newProvisionerWithTOFUMock(t, m)

	err := p.reverifyISOBeforeAttach(context.Background(), zaptest.NewLogger(t),
		"tank/talos-iso", "/mnt/tank/talos-iso/img1.iso", "img1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "vanished",
		"missing-file path must surface a TOCTOU-shaped error")
}

func TestReverifyISOBeforeAttach_DriftPoisons(t *testing.T) {
	t.Parallel()

	// Bytes were tampered between upload and attach. Re-verify must
	// catch the size/mtime drift, POISON the hash, and abort.
	m := &tofuStatMockClient{
		statResult: &client.FileInfo{Size: 999, Mtime: 1.0}, // size drifted
		tofuMockClient: tofuMockClient{props: map[string]string{
			"org.omni:iso-sha256-img1": "expected-hash",
			"org.omni:iso-size-img1":   "100",
			"org.omni:iso-mtime-img1":  "1.0",
		}},
	}
	p := newProvisionerWithTOFUMock(t, m)

	err := p.reverifyISOBeforeAttach(context.Background(), zaptest.NewLogger(t),
		"tank/talos-iso", "/mnt/tank/talos-iso/img1.iso", "img1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "metadata re-verification")
}

func TestVerifyCachedISO_NoStoredHashIsFirstTimeProvision(t *testing.T) {
	t.Parallel()

	// Pre-TOFU ISO from a provider version before the supply-chain hash
	// was recorded at all. Proceed (matches prior behaviour) — the next
	// download will establish the baseline.
	m := &tofuStatMockClient{
		tofuMockClient: tofuMockClient{props: map[string]string{}},
	}
	p := newProvisionerWithTOFUMock(t, m)

	stat := &client.FileInfo{Size: 100, Mtime: 1.0}
	err := p.verifyCachedISO(context.Background(), zaptest.NewLogger(t),
		"tank/talos-iso", "/mnt/tank/talos-iso/x.iso", "img1",
		"org.omni:iso-sha256-img1", "org.omni:iso-size-img1", "org.omni:iso-mtime-img1",
		stat)

	require.NoError(t, err)
}

// TestISOPoisonMarker_RoundTrip simulates the full mismatch path observable
// via the client: set a hash, detect mismatch on a new download, write a
// POISON marker. Verifies the marker is persisted and classified correctly
// on the next read.
func TestISOPoisonMarker_RoundTrip(t *testing.T) {
	t.Parallel()

	c, m := newTOFUClient(t)
	ctx := context.Background()
	key := "org.omni:iso-sha256-xyz"

	// Simulate a TOFU first-use recording.
	require.NoError(t, c.SetDatasetUserProperty(ctx, "tank/talos-iso", key, "expected-hash"))

	// Next provision: downloaded hash differs. Decision logic classifies mismatch.
	stored, err := c.GetDatasetUserProperty(ctx, "tank/talos-iso", key)
	require.NoError(t, err)
	assert.Equal(t, tofuMismatch, classifyTOFU(stored, "attacker-hash"))

	// The step then overwrites with a POISON marker.
	require.NoError(t, c.SetDatasetUserProperty(ctx, "tank/talos-iso", key, poisonMarker("attacker-hash")))

	// A subsequent provision reads the poisoned value and refuses.
	stored, err = c.GetDatasetUserProperty(ctx, "tank/talos-iso", key)
	require.NoError(t, err)
	assert.True(t, cachedISOPoisoned(stored),
		"POISON marker must survive round-trip through the dataset property store")
	assert.Equal(t, tofuPoisoned, classifyTOFU(stored, "any-new-hash"))

	// Verify the mock tracked both set calls.
	m.mu.Lock()
	defer m.mu.Unlock()
	assert.Len(t, m.setCalls, 2)
	assert.Equal(t, "expected-hash", m.setCalls[0].Value)
	assert.True(t, strings.HasPrefix(m.setCalls[1].Value, poisonedPrefix))
}
