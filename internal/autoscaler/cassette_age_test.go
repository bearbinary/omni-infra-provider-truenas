package autoscaler

import (
	"testing"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/testutil"
)

// TestCassettesNotStale mirrors the client-package check for the autoscaler's
// cassette directory. See internal/testutil/cassette_age.go for the rationale.
func TestCassettesNotStale(t *testing.T) {
	t.Parallel()

	testutil.CassettesNotStale(t, "testdata/cassettes", 0)
}
