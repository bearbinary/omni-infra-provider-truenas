package client

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// knownTrueNASMethods is the committed list of TrueNAS JSON-RPC methods the
// provider invokes. Every `c.call(ctx, "X.Y", …)` or `c.call(ctx, methodX, …)`
// in the client package must resolve to a method on this list; the test
// below enforces that. A new method in the code that isn't here is either a
// typo or a deliberate new integration point — either way, require an
// explicit update so the full set of methods we depend on is auditable in
// one place.
//
// When adding a method:
//  1. Verify against a real TrueNAS that it accepts our params and returns
//     the expected shape.
//  2. Add a wire-shape pin in `wire_shape_test.go` for any method that
//     takes options (so schema drift fails a unit test, not just a live
//     integration test).
//  3. Record a cassette under `testdata/cassettes/TestContract_<Method>.json`
//     if the response shape is meaningful to the provider.
var knownTrueNASMethods = map[string]struct{}{
	"auth.login_with_api_key":      {},
	"disk.query":                   {},
	"filesystem.listdir":           {},
	"filesystem.put":               {},
	"filesystem.stat":              {},
	"interface.query":              {},
	"pool.dataset.create":          {},
	"pool.dataset.delete":          {},
	"pool.dataset.lock":            {},
	"pool.dataset.query":           {},
	"pool.dataset.unlock":          {},
	"pool.dataset.update":          {},
	"pool.query":                   {},
	"system.info":                  {},
	"system.version":               {},
	"vm.create":                    {},
	"vm.delete":                    {},
	"vm.device.create":             {},
	"vm.device.delete":             {},
	"vm.device.nic_attach_choices": {},
	"vm.device.query":              {},
	"vm.device.update":             {},
	"vm.get_instance":              {},
	"vm.query":                     {},
	"vm.start":                     {},
	"vm.stop":                      {},
	"vm.update":                    {},
}

// constRe captures `ident = "method.name"` style constant bindings so
// `c.call(ctx, identifier, …)` references can be resolved back to a
// method-name string.
var constRe = regexp.MustCompile(`(?m)^\s*(method[A-Z]\w*)\s*=\s*"([a-z][a-z0-9_.]*)"`)

// callSiteRe captures both direct-literal and constant-reference call
// sites: `c.call(ctx, "X", …)` and `c.call(ctx, methodX, …)`.
// Group 1: quoted literal or bare identifier.
var callSiteRe = regexp.MustCompile(`c\.call\(ctx,\s*(?:"([a-z][a-z0-9_.]*)"|(method[A-Z]\w*))`)

// specialMethodLiteralRe captures method-name strings used outside c.call —
// currently only `Method: "X"` struct-literal assignments in the recording
// transport and the filesystem.put pipe path. Both produce real TrueNAS
// method invocations through non-c.call code paths.
var specialMethodLiteralRe = regexp.MustCompile(`Method:\s*"([a-z][a-z0-9_.]*)"`)

// TestKnownTrueNASMethods_MatchCode enforces the invariant in both
// directions:
//
//  1. Every call site in non-test client code must reference a method on
//     the allowlist (detected via direct literal, method constant, or the
//     Method: "X" struct-literal pattern used for non-JSON-RPC methods
//     like filesystem.put).
//  2. Every method on the allowlist must be referenced somewhere in the
//     non-test code — stale entries let a typo (e.g., `vm.deletee`) slip
//     past review by masking the mistake with a neighbor.
func TestKnownTrueNASMethods_MatchCode(t *testing.T) {
	t.Parallel()

	// First pass: collect all method-name constants so we can resolve
	// identifier references in c.call sites.
	constants := make(map[string]string) // ident -> "method.name"

	// Second pass: collect every method name actually invoked.
	found := make(map[string]string) // method -> first file:line reference

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if filepath.Ext(path) != ".go" {
			return nil
		}

		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, m := range constRe.FindAllSubmatch(data, -1) {
			constants[string(m[1])] = string(m[2])
		}

		for _, m := range callSiteRe.FindAllSubmatch(data, -1) {
			var method string

			if len(m[1]) > 0 {
				method = string(m[1])
			} else if len(m[2]) > 0 {
				if resolved, ok := constants[string(m[2])]; ok {
					method = resolved
				} else {
					// Identifier we can't resolve yet — constants may live in
					// a later-walked file. Record the unresolved name so we
					// can fail with a useful message below rather than
					// silently miss the call.
					method = "<unresolved:" + string(m[2]) + ">"
				}
			}

			if method == "" {
				continue
			}

			if _, already := found[method]; !already {
				found[method] = path
			}
		}

		for _, m := range specialMethodLiteralRe.FindAllSubmatch(data, -1) {
			method := string(m[1])

			if _, already := found[method]; !already {
				found[method] = path
			}
		}

		return nil
	})
	require.NoError(t, err)

	// Second-chance resolution: some call sites reference constants defined
	// in files walked later. Retry the unresolved set now that we have the
	// full constants map.
	for m, path := range found {
		if strings.HasPrefix(m, "<unresolved:") {
			ident := strings.TrimSuffix(strings.TrimPrefix(m, "<unresolved:"), ">")

			if resolved, ok := constants[ident]; ok {
				delete(found, m)
				found[resolved] = path
			}
		}
	}

	var unknown []string

	for m, path := range found {
		if _, ok := knownTrueNASMethods[m]; !ok {
			unknown = append(unknown, m+" (first seen in "+path+")")
		}
	}

	sort.Strings(unknown)

	if len(unknown) > 0 {
		t.Fatalf("client code uses %d TrueNAS method(s) not on knownTrueNASMethods:\n  %s\n"+
			"Add the method to the allowlist AFTER verifying against a real TrueNAS that "+
			"it accepts our params and returns the expected shape.",
			len(unknown), strings.Join(unknown, "\n  "))
	}

	var orphans []string

	for m := range knownTrueNASMethods {
		if _, used := found[m]; !used {
			orphans = append(orphans, m)
		}
	}

	sort.Strings(orphans)

	if len(orphans) > 0 {
		t.Fatalf("knownTrueNASMethods contains %d entries not referenced in the client code:\n  %s\n"+
			"Remove them — dead entries mask typos on neighboring methods during review.",
			len(orphans), strings.Join(orphans, "\n  "))
	}
}
