// Package depspin holds test-only assertions that pin the versions of
// direct dependencies against unauthorized drift. The package intentionally
// exports no runtime code; the go.mod is the source of truth. Any bump
// must land here in the same commit, forcing a diff-review and a CHANGELOG
// entry that documents the behavioral impact.
package depspin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/mod/modfile"
)

// wantDirect pins each direct require line in go.mod to a specific version.
// A test failure here means either:
//
//  1. Someone bumped a direct dep without updating this map. Update the map
//     AND add a CHANGELOG entry describing what behavior changed (API,
//     wire format, defaults, error semantics). "Deps bump" alone is not
//     an acceptable CHANGELOG note — nine deps in one sentence is how we
//     ended up with the CP-OOM war story.
//
//  2. `go mod tidy` swept a version. Investigate before re-pinning.
//
// Indirect deps are intentionally NOT pinned here; the go.sum plus the
// direct-dep pins is enough to make a bad transitive land visibly in
// review (via the go.mod diff) without adding maintenance friction.
var wantDirect = map[string]string{
	"github.com/cosi-project/runtime":                                   "v1.16.2",
	"github.com/google/uuid":                                            "v1.6.0",
	"github.com/gorilla/websocket":                                      "v1.5.4-0.20250319132907-e064f32e3674",
	"github.com/grafana/pyroscope-go":                                   "v1.4.1",
	"github.com/joho/godotenv":                                          "v1.5.1",
	"github.com/siderolabs/omni/client":                                 "v1.9.3",
	"github.com/stretchr/testify":                                       "v1.11.1",
	"go.opentelemetry.io/contrib/bridges/otelzap":                       "v0.19.0",
	"go.opentelemetry.io/otel":                                          "v1.44.0",
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc":       "v0.20.0",
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp":       "v0.20.0",
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc": "v1.44.0",
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp": "v1.44.0",
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc":   "v1.44.0",
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp":   "v1.44.0",
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog":               "v0.20.0",
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric":            "v1.44.0",
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace":             "v1.44.0",
	"go.opentelemetry.io/otel/log":                                      "v0.20.0",
	"go.opentelemetry.io/otel/metric":                                   "v1.44.0",
	"go.opentelemetry.io/otel/sdk":                                      "v1.44.0",
	"go.opentelemetry.io/otel/sdk/log":                                  "v0.20.0",
	"go.opentelemetry.io/otel/sdk/metric":                               "v1.44.0",
	"go.opentelemetry.io/otel/trace":                                    "v1.44.0",
	"go.uber.org/zap":                                                   "v1.28.0",
	"golang.org/x/mod":                                                  "v0.38.0",
	"golang.org/x/sync":                                                 "v0.22.0",
	"google.golang.org/grpc":                                            "v1.82.1",
	"google.golang.org/protobuf":                                        "v1.36.12-0.20260120151049-f2248ac996af",
	"gopkg.in/yaml.v3":                                                  "v3.0.1",
}

// TestDirectDepVersions asserts that every direct require line in the
// repo's go.mod matches wantDirect. Extra directs (in go.mod but not
// wantDirect) and missing directs (in wantDirect but not go.mod) both
// fail the test — the map is the source of truth for what we ship.
func TestDirectDepVersions(t *testing.T) {
	t.Parallel()

	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}

	mf, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatalf("parse go.mod: %v", err)
	}

	gotDirect := map[string]string{}

	for _, r := range mf.Require {
		if r.Indirect {
			continue
		}

		gotDirect[r.Mod.Path] = r.Mod.Version
	}

	for path, want := range wantDirect {
		got, ok := gotDirect[path]
		if !ok {
			t.Errorf(
				"direct dep %s is missing from go.mod but pinned in wantDirect. "+
					"If this dep was intentionally dropped, remove it from "+
					"internal/depspin/versions_test.go in the same commit.",
				path,
			)

			continue
		}

		if got != want {
			t.Errorf(
				"direct dep %s is at %s, want %s. "+
					"If this is an intentional bump, update the wantDirect map "+
					"and add a CHANGELOG entry documenting the behavioral impact.",
				path, got, want,
			)
		}
	}

	for path, got := range gotDirect {
		if _, ok := wantDirect[path]; !ok {
			t.Errorf(
				"direct dep %s (at %s) is in go.mod but not pinned in wantDirect. "+
					"Add it to internal/depspin/versions_test.go with a matching "+
					"CHANGELOG note.",
				path, got,
			)
		}
	}
}

// repoRoot walks up from this test file's location until it finds go.mod.
func repoRoot() (string, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}

		dir = parent
	}
}
