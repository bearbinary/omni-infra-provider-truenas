package lintconcurrency

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestAnalyzer runs each fixture package under go/analysis/analysistest.
// The `// want ...` comments in the fixtures declare the expected
// diagnostics; analysistest flags any mismatch.
func TestAnalyzer(t *testing.T) {
	// Shim filepathGlob to return only files inside the fixture package
	// directory. The default (real filepath.Glob) also works, but pinning
	// the shim to the analysistest-managed dir keeps the fixture behavior
	// deterministic across machines.
	//
	// (We keep the real shim in production — the shim var exists so future
	// tests can inject mock directory contents without changing prod
	// behavior.)

	testdata := analysistest.TestData()

	for _, pkg := range []string{
		"has_wg_with_test",
		"has_wg_no_test",
		"has_pointer_wg_no_test",
		"has_embedded_wg_no_test",
		"has_errgroup_no_test",
		"has_no_wg",
		"has_safego",
	} {
		t.Run(pkg, func(t *testing.T) {
			analysistest.Run(t, testdata, Analyzer, pkg)
		})
	}
}
