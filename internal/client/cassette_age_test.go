package client

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestCassettesNotStale fails when any cassette under
// `internal/client/testdata/cassettes/` has a mtime older than the staleness
// threshold. Cassettes are snapshots of TrueNAS behavior at record time; if
// the production API shape changes (param rename, new required field,
// response field removal, new error code), stale cassettes will keep
// replaying the old shape and silently hide the regression.
//
// The orphan-cleanup bug that shipped in v0.15.0 (see v0.15.3 changelog) went
// undetected partly because its integration cassette predated the VM-name
// format change and kept passing against mocks that happened to match the
// broken code's expectations.
//
// To refresh cassettes:
//
//	make test-record    # requires TRUENAS_TEST_HOST + TRUENAS_TEST_API_KEY
//
// Override the staleness window for CI experiments or long-range investigations:
//
//	CASSETTE_MAX_AGE_DAYS=365 go test ./internal/client/...
//
// Set CASSETTE_MAX_AGE_DAYS=0 to disable the gate entirely. The default
// (90 days) is conservative — shorter would cause noise on a quiet week
// without meaningfully earlier detection.
func TestCassettesNotStale(t *testing.T) {
	t.Parallel()

	maxAgeDays := 90
	if v := os.Getenv("CASSETTE_MAX_AGE_DAYS"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("CASSETTE_MAX_AGE_DAYS=%q: not an integer", v)
		}

		maxAgeDays = parsed
	}

	if maxAgeDays == 0 {
		t.Skip("CASSETTE_MAX_AGE_DAYS=0 disables the cassette-age gate")
	}

	cutoff := time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)

	root := "testdata/cassettes"

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read cassette dir %q: %v", root, err)
	}

	var stale []string

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}

		full := filepath.Join(root, e.Name())

		info, err := e.Info()
		if err != nil {
			t.Fatalf("stat %q: %v", full, err)
		}

		if info.ModTime().Before(cutoff) {
			age := int(time.Since(info.ModTime()).Hours() / 24)
			stale = append(stale, full+" ("+strconv.Itoa(age)+" days old)")
		}
	}

	if len(stale) > 0 {
		t.Fatalf(
			"cassettes older than %d days — TrueNAS API shape may have drifted since they were recorded. "+
				"Re-record with `make test-record` against a live TrueNAS, or bump CASSETTE_MAX_AGE_DAYS "+
				"for a one-off override.\n\nStale:\n  %v",
			maxAgeDays, stale,
		)
	}
}
