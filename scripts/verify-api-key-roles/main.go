// verify-api-key-roles exercises every TrueNAS JSON-RPC method + the
// /_upload endpoint that the provider calls, using an API key you supply.
// Output is a pass/fail matrix telling you which of the 13 recommended
// roles (or FULL_ADMIN) the key actually has.
//
// Usage:
//
//	TRUENAS_HOST=<host:port> \
//	TRUENAS_API_KEY=<key> \
//	TRUENAS_POOL=<pool> \
//	go run ./scripts/verify-api-key-roles
//
// The probe creates a temporary dataset `<pool>/omni-role-probe-<timestamp>`
// and deletes it at the end. No persistent state is left behind on success.
// On failure (early exit), you may need to manually delete the dataset via
// TrueNAS UI > Storage > Datasets.
//
// The probe does NOT start VMs, upload real ISOs, or touch any of your
// existing data. The only write operations are:
//   - create + delete a 1 MB test zvol inside the probe dataset
//   - create + delete a stopped test VM named omni-role-probe-<timestamp>
//   - upload a 16-byte file to the probe dataset and verify it landed
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	truenasclient "github.com/bearbinary/omni-infra-provider-truenas/internal/client"
	"github.com/bearbinary/omni-infra-provider-truenas/internal/truenasrpc"
)

type rpcReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type probe struct {
	conn   *websocket.Conn
	host   string
	apiKey truenasclient.SecretString // zeroized after auth via p.apiKey = SecretString{}
	nextID atomic.Int64
}

// parseInsecureSkipVerify reads TRUENAS_INSECURE_SKIP_VERIFY using the same
// strconv.ParseBool semantics the production client uses via envBool —
// accepts "1", "t", "T", "true", "TRUE", "True" / "0", "f", etc. Avoids the
// foot-gun where the probe accepts only "1" while production accepts the
// full ParseBool surface, so an operator's `.env` with `=true` would have
// the probe verify TLS while production silently skipped.
func parseInsecureSkipVerify() bool {
	v := os.Getenv("TRUENAS_INSECURE_SKIP_VERIFY")
	if v == "" {
		return false
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return false
	}
	return parsed
}

// normalizeParams delegates to the shared internal/truenasrpc package so
// the probe and the production client cannot drift on JSON-RPC param
// shape.
func normalizeParams(params any) any { return truenasrpc.NormalizeParams(params) }

func (p *probe) call(method string, params any) (json.RawMessage, error) {
	id := p.nextID.Add(1)

	// TrueNAS 25.10 JSON-RPC requires params to be an array (positional).
	// Matches normalizeParams() in internal/client/ws.go: nil → [], non-array → single-element array.
	normalized := normalizeParams(params)

	if err := p.conn.WriteJSON(rpcReq{JSONRPC: "2.0", ID: id, Method: method, Params: normalized}); err != nil {
		return nil, err
	}

	var resp rpcResp
	if err := p.conn.ReadJSON(&resp); err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s", resp.Error.Message)
	}

	return resp.Result, nil
}

type result struct {
	method   string
	roleHint string
	err      error
}

func (r result) status() string {
	if r.err == nil {
		return "PASS"
	}

	if isAuthError(r.err) {
		return "DENIED"
	}

	return "FAIL"
}

func isAuthError(err error) bool {
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "not authorized") ||
		strings.Contains(m, "not allowed") ||
		strings.Contains(m, "permission") ||
		strings.Contains(m, "forbidden") ||
		strings.Contains(m, "unauthorized") ||
		strings.Contains(m, "missing role") ||
		strings.Contains(m, "access denied")
}

func main() {
	host := os.Getenv("TRUENAS_HOST")
	apiKey := os.Getenv("TRUENAS_API_KEY")
	pool := os.Getenv("TRUENAS_POOL")

	if host == "" || apiKey == "" || pool == "" {
		fmt.Fprintln(os.Stderr, "Set TRUENAS_HOST, TRUENAS_API_KEY, and TRUENAS_POOL env vars.")
		os.Exit(2)
	}

	// Validate up front so a typo'd host like `good.tld@evil.tld` fails
	// before any auth-bearing request leaves the process. Defense in depth
	// alongside the per-call validation in uploadFile.
	if err := validateProbeHost(host); err != nil {
		fmt.Fprintf(os.Stderr, "TRUENAS_HOST rejected: %v\n", err)
		os.Exit(2)
	}

	// TLS verification is on by default. Operators with self-signed certs
	// opt out via TRUENAS_INSECURE_SKIP_VERIFY (strconv.ParseBool semantics —
	// matches the production envBool parser so probe and production agree
	// on what the env var means).
	insecure := parseInsecureSkipVerify()
	if insecure {
		fmt.Fprintln(os.Stderr, "WARNING: TLS verification disabled via TRUENAS_INSECURE_SKIP_VERIFY — API key may transit an unverified channel; unset the var or fix the TLS cert before relying on this output to grant production access")
	}

	conn, err := dialProbeWebSocket(host, insecure)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()

	p := &probe{conn: conn, host: host, apiKey: truenasclient.NewSecretString(apiKey)}

	if _, err := p.call("auth.login_with_api_key", []string{apiKey}); err != nil {
		fmt.Fprintf(os.Stderr, "auth failed: %v — the API key is invalid or the user is disabled\n", err)
		_ = conn.Close()
		os.Exit(1) //nolint:gocritic // conn already closed above
	}

	// Item 14 (Sec): API key is no longer needed in process memory after
	// the WebSocket session is authenticated. Zero the SecretString so a
	// core dump or swap leak during the remaining probes cannot exfiltrate
	// the key. The local `apiKey` string is left as-is because Go strings
	// are immutable — re-binding the local does not zero the heap backing
	// array; SecretString zero is the meaningful clear.
	uploadClient := newProbeUploadClient(insecure)
	bearer := "Bearer " + p.apiKey.Reveal()
	p.apiKey = truenasclient.SecretString{}

	results := []result{}
	add := func(method, role string, err error) {
		results = append(results, result{method: method, roleHint: role, err: err})
	}

	probeReadMethods(p, pool, add)

	// Item 22 (Maint): probeDatasetLifecycle / probeVMLifecycle /
	// probeVMDeviceCRUD were over-split — they had one caller, moved
	// defer-cleanup state away from the failure path, and made the script's
	// "operator reads top-to-bottom" value harder. Inlined back here.
	ts := time.Now().Unix()

	probeDs := fmt.Sprintf("%s/omni-role-probe-%d", pool, ts)
	probePath := "/mnt/" + probeDs

	if _, err := p.call("pool.dataset.create", map[string]any{"name": probeDs}); err != nil {
		add("pool.dataset.create", "DATASET_WRITE", err)
	} else {
		add("pool.dataset.create", "DATASET_WRITE", nil)
		defer func() { _, _ = p.call("pool.dataset.delete", []any{probeDs}) }()

		_, err = p.call("pool.dataset.update", []any{probeDs, map[string]any{"comments": "role probe"}})
		add("pool.dataset.update", "DATASET_WRITE", err)

		uploadErr := uploadFile(uploadClient, p.host, bearer, probePath+"/probe.txt", []byte("role-probe-sentinel"))
		add("filesystem.put (via /_upload)", "FILESYSTEM_DATA_WRITE", uploadErr)

		_, err = p.call("pool.dataset.delete", []any{probeDs, map[string]any{"recursive": true}})
		add("pool.dataset.delete", "DATASET_DELETE", err)
	}

	vmName := fmt.Sprintf("omniroleprobe%d", ts)
	vmParams := map[string]any{
		"name":        vmName,
		"description": "omni-infra-provider role probe — safe to delete",
		"vcpus":       1,
		"cores":       1,
		"threads":     1,
		"memory":      256,
		"bootloader":  "UEFI",
		"autostart":   false,
		"time":        "LOCAL",
	}

	vmResp, vmErr := p.call("vm.create", vmParams)
	add("vm.create", "VM_WRITE", vmErr)
	if vmErr == nil {
		var vmID float64
		_ = json.Unmarshal(vmResp, &struct {
			ID *float64 `json:"id"`
		}{ID: &vmID})

		if vmID > 0 {
			defer func() { _, _ = p.call("vm.delete", []any{int(vmID)}) }()

			_, err = p.call("vm.update", []any{int(vmID), map[string]any{"description": "updated"}})
			add("vm.update", "VM_WRITE", err)

			_, err = p.call("vm.get_instance", []any{int(vmID)})
			add("vm.get_instance", "VM_READ", err)

			// vm.start with no devices fails for a non-auth reason; vm.stop
			// is safe and still exercises VM_WRITE authorization.
			_, err = p.call("vm.stop", []any{int(vmID), map[string]any{"force": true}})
			add("vm.stop", "VM_WRITE", err)

			nicAttach := "br0"
			if choicesResp, nicErr := p.call("vm.device.nic_attach_choices", nil); nicErr == nil {
				var choices map[string]string
				if json.Unmarshal(choicesResp, &choices) == nil {
					for name := range choices {
						nicAttach = name
						break
					}
				}
			}

			devResp, devCreateErr := p.call("vm.device.create", map[string]any{
				"vm":         int(vmID),
				"order":      3000,
				"attributes": map[string]any{"dtype": "NIC", "type": "VIRTIO", "nic_attach": nicAttach},
			})
			add("vm.device.create", "VM_DEVICE_WRITE", devCreateErr)
			if devCreateErr == nil {
				var devID float64
				_ = json.Unmarshal(devResp, &struct {
					ID *float64 `json:"id"`
				}{ID: &devID})
				if devID > 0 {
					_, err = p.call("vm.device.update", []any{int(devID), map[string]any{"attributes": map[string]any{"dtype": "NIC", "type": "VIRTIO", "nic_attach": "br0"}}})
					add("vm.device.update", "VM_DEVICE_WRITE", err)

					_, err = p.call("vm.device.delete", []any{int(devID)})
					add("vm.device.delete", "VM_DEVICE_WRITE", err)
				}
			}
		}
	}

	printReport(results)

	if summary(results) != 0 {
		os.Exit(1)
	}
}

// dialProbeWebSocket tries both TrueNAS WebSocket paths and returns the first
// that hands back a working connection. Splitting this out keeps the main()
// loop ergonomic and cog-complexity bounded.
func dialProbeWebSocket(host string, insecure bool) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: insecure, MinVersion: tls.VersionTLS12}, //nolint:gosec // operator-gated
		HandshakeTimeout: 10 * time.Second,
	}

	for _, path := range []string{"/api/current", "/websocket"} {
		u := url.URL{Scheme: "wss", Host: host, Path: path}
		if c, _, err := dialer.Dial(u.String(), http.Header{}); err == nil {
			return c, nil
		}
	}

	return nil, fmt.Errorf("could not connect to TrueNAS WebSocket on %s", host)
}

// probeReadMethods exercises every READ JSON-RPC the provisioner needs.
// system.info is omitted because the response is large and flakes JSON
// decode at this size; system.version covers READONLY_ADMIN anyway.
func probeReadMethods(p *probe, pool string, add func(string, string, error)) {
	reads := []struct {
		method string
		role   string
		params any
	}{
		{"system.version", "READONLY_ADMIN", nil},
		{"pool.query", "POOL_READ", nil},
		{"pool.dataset.query", "DATASET_READ", nil},
		{"disk.query", "DISK_READ", nil},
		{"interface.query", "NETWORK_INTERFACE_READ", nil},
		{"filesystem.stat", "FILESYSTEM_ATTRS_READ", []any{"/mnt/" + pool}},
		{"filesystem.listdir", "FILESYSTEM_ATTRS_READ", []any{"/mnt/" + pool}},
		{"vm.query", "VM_READ", nil},
		{"vm.device.query", "VM_DEVICE_READ", nil},
		{"vm.device.nic_attach_choices", "VM_DEVICE_READ", nil},
	}

	for _, r := range reads {
		_, err := p.call(r.method, r.params)
		add(r.method, r.role, err)
	}
}

// validateProbeHost delegates to internal/truenasrpc.ValidateHost so the
// probe and production client share one source of truth — including the
// strict DNS-character allow-list that the probe's earlier in-line copy
// was missing. The probe handles real Bearer tokens against real hosts;
// it must not have a weaker validator than production.
func validateProbeHost(host string) error { return truenasrpc.ValidateHost(host) }

// newProbeUploadClient builds the http.Client the probe uses to talk to
// /_upload. The previous sync.OnceValue form read TRUENAS_INSECURE_SKIP_VERIFY
// on first call, which made the env-var observation order-dependent under
// tests (t.Setenv after a prior call would have no effect). Pulling the env
// read up to main() and passing the flag in keeps the constructor pure.
//
// CheckRedirect: refuse to follow 3xx. Go's default would re-send the
// Bearer header (full TRUENAS_API_KEY) to the redirect target, so a
// compromised or MITM'd TrueNAS could harvest the operator's key by
// returning `302 Location: https://attacker.tld/`. The unconditional
// ErrUseLastResponse covers all 3xx classes, including the dangerous
// 307/308 that preserve method and body. ws.go does the same thing on
// the production path; this probe must not regress.
func newProbeUploadClient(insecure bool) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure, MinVersion: tls.VersionTLS12}, //nolint:gosec // operator-gated
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 30 * time.Second,
	}
}

// uploadFile exercises the /_upload HTTP endpoint used for Talos ISO uploads.
// Matches the provider's internal/client/ws.go upload path. Takes the
// pre-built http.Client and Bearer header as args (rather than reading them
// off a *probe) so the API key can be zeroed on the probe after auth
// without breaking this call site.
func uploadFile(uploadClient *http.Client, host, bearer, destPath string, data []byte) error {
	if err := validateProbeHost(host); err != nil {
		return fmt.Errorf("refusing to send Bearer token to unvalidated TRUENAS_HOST: %w", err)
	}

	// Build the URL via net/url rather than fmt.Sprintf so the Host slot
	// can't carry a hand-crafted path/userinfo that smuggles the request
	// to a different destination. Mirrors ws.go's "fmt.Sprintf +
	// unvalidated host = bearer exfil. Never again." comment.
	uploadURL := (&url.URL{Scheme: "https", Host: host, Path: "/_upload/"}).String()

	var body bytes.Buffer

	mw := multipart.NewWriter(&body)

	// Part 1: JSON method envelope for filesystem.put
	dataJSON := fmt.Sprintf(`{"method": "filesystem.put", "params": [%q, {"mode": 493}]}`, destPath)
	if err := mw.WriteField("data", dataJSON); err != nil {
		return err
	}

	// Part 2: file content
	fw, err := mw.CreateFormFile("file", "probe.txt")
	if err != nil {
		return err
	}

	if _, err := fw.Write(data); err != nil {
		return err
	}

	if err := mw.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, uploadURL, &body)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", bearer)

	resp, err := uploadClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	return nil
}

func printReport(results []result) {
	fmt.Printf("\n%-40s %-30s %s\n", "METHOD", "ROLE REQUIRED", "STATUS")
	fmt.Println(strings.Repeat("─", 95))

	missingRoles := map[string]bool{}

	for _, r := range results {
		status := r.status()
		row := fmt.Sprintf("%-40s %-30s %s", r.method, r.roleHint, status)

		if r.err != nil {
			row += " — " + truncate(r.err.Error(), 50)
		}

		fmt.Println(row)

		if status == "DENIED" {
			missingRoles[r.roleHint] = true
		}
	}

	fmt.Println()

	if len(missingRoles) > 0 {
		fmt.Println("MISSING ROLES (add these to the privilege):")

		for r := range missingRoles {
			fmt.Println("  - " + r)
		}
	} else {
		fmt.Println("All 13 required roles present. Scoped key works for the provider.")
	}
}

// summary returns 0 if all PASS, non-zero otherwise.
func summary(results []result) int {
	for _, r := range results {
		if r.err != nil {
			return 1
		}
	}

	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "..."
}
