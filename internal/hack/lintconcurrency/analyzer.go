// Package lintconcurrency provides a go/analysis Analyzer that enforces the
// repo invariant "every long-lived goroutine owner ships a lifecycle test".
//
// It replaces the earlier bash implementation (hack/check-goroutine-owners.sh),
// which relied on a fragile regex over source text and missed named /
// embedded / pointer sync.WaitGroup fields, `errgroup.Group` fields, and
// (most importantly) silently exited 0 when the candidate list was empty —
// exactly the "silent no-op lint" class this analyzer is designed to
// eliminate.
//
// Detection rule: a package must ship at least one *_lifecycle_test.go,
// *_stress_test.go, or *_race_test.go file if any of its non-test .go files
// declare a struct with a field whose type is:
//
//   - `sync.WaitGroup`  (named or embedded)
//   - `*sync.WaitGroup`
//   - `errgroup.Group`  (named or embedded — matched by selector name,
//     since the ergonomic use is always golang.org/x/sync/errgroup and
//     shadowing that package is not a realistic concern here)
//   - `*errgroup.Group`
//
// The analyzer emits one diagnostic per goroutine-owning field per package;
// the exit code is non-zero if any diagnostics are emitted.
//
// Rejected functions: as of the docs/concurrency-patterns.md "Rejected
// patterns" section, a top-level function literally named `safeGo` in any
// non-test .go file is also flagged. See that doc for why the extracted
// helper is an anti-pattern here.
package lintconcurrency

import (
	"go/ast"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer is the go/analysis.Analyzer entry point.
var Analyzer = &analysis.Analyzer{
	Name: "lintconcurrency",
	Doc: "Enforces the repo invariant that every package declaring a " +
		"long-lived goroutine owner (sync.WaitGroup / errgroup.Group " +
		"field) also ships a *_lifecycle_test.go / *_stress_test.go / " +
		"*_race_test.go companion.",
	Run: run,
}

// isGoroutineOwnerType returns true if the given ast.Expr is a type that
// signals a long-lived goroutine owner: sync.WaitGroup or errgroup.Group,
// either directly or via pointer.
func isGoroutineOwnerType(expr ast.Expr) bool {
	// Peel one level of pointer indirection: `*sync.WaitGroup`, `*errgroup.Group`.
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}

	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}

	switch pkgIdent.Name {
	case "sync":
		return sel.Sel.Name == "WaitGroup"
	case "errgroup":
		return sel.Sel.Name == "Group"
	}
	return false
}

// packageHasLifecycleTest returns true if the analysis pass has visibility
// into any *_lifecycle_test.go, *_stress_test.go, or *_race_test.go file.
// We consult pass.OtherFiles too because analysistest occasionally exposes
// test files that way.
func packageHasLifecycleTest(pass *analysis.Pass) bool {
	names := make([]string, 0, len(pass.Files)+len(pass.OtherFiles))
	for _, f := range pass.Files {
		if pos := pass.Fset.File(f.Pos()); pos != nil {
			names = append(names, pos.Name())
		}
	}
	names = append(names, pass.OtherFiles...)

	for _, n := range names {
		base := filepath.Base(n)
		if strings.HasSuffix(base, "_lifecycle_test.go") ||
			strings.HasSuffix(base, "_stress_test.go") ||
			strings.HasSuffix(base, "_race_test.go") {
			return true
		}
	}
	return false
}

// findLifecycleTestOnDisk falls back to walking the package's source
// directory when the pass has no test files in view (the `go vet` /
// singlechecker driver filters *_test.go from pass.Files by default).
// If any file in the directory matches the lifecycle-suffix convention,
// we count the package as covered. This mirrors hack/check-goroutine-
// owners.sh behavior it replaces.
func findLifecycleTestOnDisk(pass *analysis.Pass) bool {
	if len(pass.Files) == 0 {
		return false
	}
	fname := pass.Fset.File(pass.Files[0].Pos()).Name()
	dir := filepath.Dir(fname)

	entries, err := filepathGlob(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		base := filepath.Base(e)
		if strings.HasSuffix(base, "_lifecycle_test.go") ||
			strings.HasSuffix(base, "_stress_test.go") ||
			strings.HasSuffix(base, "_race_test.go") {
			return true
		}
	}
	return false
}

// filepathGlob is a wrapper so tests can shim it. It returns every .go
// file directly under dir (non-recursive).
var filepathGlob = func(dir string) ([]string, error) {
	return filepath.Glob(filepath.Join(dir, "*.go"))
}

type finding struct {
	pos  ast.Node
	kind string // "waitgroup" | "errgroup" | "safego"
	name string
}

func run(pass *analysis.Pass) (any, error) {
	var findings []finding

	for _, f := range pass.Files {
		// Skip *_test.go entirely — a WaitGroup in a test file is
		// scaffolding, not a production goroutine owner.
		if fset := pass.Fset.File(f.Pos()); fset != nil {
			if strings.HasSuffix(fset.Name(), "_test.go") {
				continue
			}
		}

		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.StructType:
				if node.Fields == nil {
					return true
				}
				for _, field := range node.Fields.List {
					if !isGoroutineOwnerType(field.Type) {
						continue
					}
					kind := "waitgroup"
					// errgroup.Group vs sync.WaitGroup — walk the selector.
					t := field.Type
					if star, ok := t.(*ast.StarExpr); ok {
						t = star.X
					}
					if sel, ok := t.(*ast.SelectorExpr); ok {
						if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "errgroup" {
							kind = "errgroup"
						}
					}
					name := "<embedded>"
					if len(field.Names) > 0 {
						name = field.Names[0].Name
					}
					findings = append(findings, finding{
						pos:  field,
						kind: kind,
						name: name,
					})
				}
			case *ast.FuncDecl:
				// Reject the extracted safeGo helper (docs/concurrency-
				// patterns.md → Rejected patterns). This is a static-name
				// check because the whole point is that the ceremony must
				// stay inline at the goroutine site.
				if node.Name != nil && node.Name.Name == "safeGo" {
					findings = append(findings, finding{
						pos:  node.Name,
						kind: "safego",
						name: "safeGo",
					})
				}
			}
			return true
		})
	}

	// Emit safeGo diagnostics unconditionally.
	for _, f := range findings {
		if f.kind == "safego" {
			pass.Reportf(f.pos.Pos(),
				"function `safeGo` is a rejected pattern per docs/concurrency-patterns.md — "+
					"the recover/log/metric ceremony must stay inline at each goroutine site")
		}
	}

	// Non-safego findings only trip a diagnostic if the package lacks
	// a lifecycle test companion.
	var hasOwner bool
	for _, f := range findings {
		if f.kind != "safego" {
			hasOwner = true
			break
		}
	}
	if !hasOwner {
		return nil, nil
	}

	if packageHasLifecycleTest(pass) || findLifecycleTestOnDisk(pass) {
		return nil, nil
	}

	for _, f := range findings {
		if f.kind == "safego" {
			continue
		}
		pass.Reportf(f.pos.Pos(),
			"package declares a %s field (%q) but ships no *_lifecycle_test.go / *_stress_test.go / *_race_test.go — "+
				"see docs/concurrency-patterns.md",
			f.kind, f.name)
	}

	return nil, nil
}
