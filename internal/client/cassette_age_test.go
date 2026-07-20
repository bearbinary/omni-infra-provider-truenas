package client

import (
	"testing"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/testutil"
)

// TestCassettesNotStale fails when any cassette under
// `internal/client/testdata/cassettes/` was introduced by a git commit
// older than the staleness threshold. Cassettes are snapshots of TrueNAS
// behavior at record time; if the production API shape changes (param
// rename, new required field, response field removal, new error code),
// stale cassettes will keep replaying the old shape and silently hide
// the regression.
//
// The orphan-cleanup bug that shipped in v0.15.0 (see v0.15.3 changelog) went
// undetected partly because its integration cassette predated the VM-name
// format change and kept passing against mocks that happened to match the
// broken code's expectations.
//
// Age is measured against `git log -1 --format=%ct -- <path>`, not the
// filesystem mtime — the latter reflects checkout wall-clock and would
// mark every fresh clone as 0 days old.
//
// To refresh cassettes:
//
//	make test-record    # requires TRUENAS_TEST_HOST + TRUENAS_TEST_API_KEY
//
// Override the staleness window for CI experiments or long-range investigations:
//
//	CASSETTE_MAX_AGE_DAYS=365 go test ./internal/client/...
//
// Set CASSETTE_MAX_AGE_DAYS=0 to disable the gate entirely.
func TestCassettesNotStale(t *testing.T) {
	t.Parallel()

	testutil.CassettesNotStale(t, "testdata/cassettes", 0)
}
