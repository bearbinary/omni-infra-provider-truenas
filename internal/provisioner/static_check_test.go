package provisioner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateConfigPatch_AlwaysUsesPatchNameHelper is an AST-level guard that
// fails any `pctx.CreateConfigPatch(ctx, "<literal>", ...)` call in the
// provisioner package. The patch name must always be composed via
// `patchName(kind, requestID)` so the resulting ConfigPatchRequest resource
// is unique per MachineRequest.
//
// This catches the v0.14.3–v0.14.5 cross-MachineRequest collision bug class
// at compile-test time: developers can add new patch kinds without re-reading
// the patchName() doc comment, and the test will fail until they thread the
// request ID through.
func TestCreateConfigPatch_AlwaysUsesPatchNameHelper(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	fset := token.NewFileSet()

	var violations []string

	for _, path := range files {
		// Skip test files — tests may legitimately use literal patch names
		// for input fixtures.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		src, err := os.ReadFile(path)
		require.NoError(t, err)

		f, err := parser.ParseFile(fset, path, src, parser.AllErrors)
		require.NoError(t, err)

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			if sel.Sel.Name != "CreateConfigPatch" {
				return true
			}

			// CreateConfigPatch signature: (ctx, name, data). We care about arg[1].
			if len(call.Args) < 2 {
				return true
			}

			if lit, ok := call.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				pos := fset.Position(call.Pos())
				violations = append(violations, fmt.Sprintf(
					"%s: CreateConfigPatch called with string literal name %s — must use patchName(kind, requestID) helper to avoid cross-MachineRequest resource collision (see v0.14.3–v0.14.5 regression).",
					pos, lit.Value))
			}

			return true
		})
	}

	assert.Empty(t, violations,
		"All CreateConfigPatch calls in the provisioner package must use the patchName() helper. "+
			"Bare string-literal names collide across MachineRequests because the SDK's CreateConfigPatch "+
			"uses the literal name as the resource ID and upserts on every reconcile. "+
			"Violations:\n%s", strings.Join(violations, "\n"))
}

// TestStepCreateVM_WiresAllExpectedPatches verifies that stepCreateVM in
// steps.go contains a CreateConfigPatch call for each patch kind we ship.
// Catches accidental deletion of a call site — the per-builder unit tests
// would still pass, but the wiring would silently disappear.
//
// AST-based: walks every call to patchName(<kind>, ...) in the package and
// collects the first-arg string literal. Resilient to helper extraction as
// long as the kind flows through patchName() somewhere — which is itself
// enforced by TestCreateConfigPatch_AlwaysUsesPatchNameHelper above.
func TestStepCreateVM_WiresAllExpectedPatches(t *testing.T) {
	t.Parallel()

	kinds := collectPatchNameKinds(t)

	cases := []struct {
		kind    string
		because string
	}{
		{
			kind:    "data-volumes",
			because: "without the data-volumes patch, additional_disks attach but show up as raw unformatted block devices in the guest — invisible to Longhorn and every other CSI driver",
		},
		{
			kind:    "longhorn-ops",
			because: "without the longhorn-ops patch (iscsi_tcp module + bind mount + sysctl), Longhorn either can't attach replicas (iSCSI broken) or silently writes data to Talos's ephemeral root partition",
		},
		{
			kind:    "nic-mtu",
			because: "without the nic-mtu patch, additional NICs with custom MTU (e.g. 9000 for storage networks) come up at the host default — jumbo-frame storage paths fragment to 1500",
		},
		{
			kind:    "advertised-subnets",
			because: "without the advertised-subnets patch, multi-NIC clusters have unstable etcd / kubelet bindings (Omni issue context for the v0.13.0 multi-NIC work)",
		},
		{
			kind:    "nic-interfaces",
			because: "without the nic-interfaces patch, additional NICs come up link-only with fe80::/64 and no IPv4 — the exact v0.15.5 regression this patch was added to fix",
		},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, kinds, tc.kind,
				"provisioner package must call patchName(%q, ...) somewhere so stepCreateVM wires the patch.\n%s",
				tc.kind, tc.because)
		})
	}
}

// collectPatchNameKinds walks every .go file (non-test) in the provisioner
// package and returns the set of first-arg string literals passed to
// patchName(). Used by wiring tests to assert presence without pinning the
// caller's file/location.
func collectPatchNameKinds(t *testing.T) map[string]struct{} {
	t.Helper()

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	kinds := map[string]struct{}{}

	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		src, err := os.ReadFile(path)
		require.NoError(t, err)

		f, err := parser.ParseFile(fset, path, src, parser.AllErrors)
		require.NoError(t, err)

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			ident, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}

			// patchName(kind, requestID): first arg is the kind string literal.
			// applyConfigPatch(ctx, pctx, kind, requestID, data): third arg
			// (index 2) is the kind string literal — the wrapper is the idiomatic
			// call site after the v0.16 telemetry refactor, so both paths count.
			var kindArgIdx int
			switch ident.Name {
			case "patchName":
				kindArgIdx = 0
			case "applyConfigPatch":
				kindArgIdx = 2
			default:
				return true
			}

			if len(call.Args) <= kindArgIdx {
				return true
			}

			lit, ok := call.Args[kindArgIdx].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}

			unquoted, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}

			kinds[unquoted] = struct{}{}

			return true
		})
	}

	return kinds
}
