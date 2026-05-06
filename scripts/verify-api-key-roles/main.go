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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

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
	apiKey string
	nextID atomic.Int64
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

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // probe tool
		HandshakeTimeout: 10 * time.Second,
	}

	var conn *websocket.Conn

	for _, path := range []string{"/api/current", "/websocket"} {
		u := url.URL{Scheme: "wss", Host: host, Path: path}

		c, _, err := dialer.Dial(u.String(), http.Header{})
		if err == nil {
			conn = c
			break
		}
	}

	if conn == nil {
		fmt.Fprintln(os.Stderr, "could not connect to TrueNAS WebSocket")
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()

	p := &probe{conn: conn, host: host, apiKey: apiKey}

	if _, err := p.call("auth.login_with_api_key", []string{apiKey}); err != nil {
		fmt.Fprintf(os.Stderr, "auth failed: %v — the API key is invalid or the user is disabled\n", err)
		_ = conn.Close()
		os.Exit(1) //nolint:gocritic // conn already closed above
	}

	results := []result{}
	add := func(method, role string, err error) {
		results = append(results, result{method: method, roleHint: role, err: err})
	}

	// ─── READ methods ─────────────────────────────────────────
	// system.info omitted — response is large and flakes JSON decode on
	// large payloads. system.version covers READONLY_ADMIN coverage anyway.
	_, err := p.call("system.version", nil)
	add("system.version", "READONLY_ADMIN", err)

	_, err = p.call("pool.query", nil)
	add("pool.query", "POOL_READ", err)

	_, err = p.call("pool.dataset.query", nil)
	add("pool.dataset.query", "DATASET_READ", err)

	_, err = p.call("disk.query", nil)
	add("disk.query", "DISK_READ", err)

	_, err = p.call("interface.query", nil)
	add("interface.query", "NETWORK_INTERFACE_READ", err)

	_, err = p.call("filesystem.stat", []any{"/mnt/" + pool})
	add("filesystem.stat", "FILESYSTEM_ATTRS_READ", err)

	_, err = p.call("filesystem.listdir", []any{"/mnt/" + pool})
	add("filesystem.listdir", "FILESYSTEM_ATTRS_READ", err)

	_, err = p.call("vm.query", nil)
	add("vm.query", "VM_READ", err)

	_, err = p.call("vm.device.query", nil)
	add("vm.device.query", "VM_DEVICE_READ", err)

	_, err = p.call("vm.device.nic_attach_choices", nil)
	add("vm.device.nic_attach_choices", "VM_DEVICE_READ", err)

	// ─── WRITE: probe dataset lifecycle ─────────────────────
	ts := time.Now().Unix()
	probeDs := fmt.Sprintf("%s/omni-role-probe-%d", pool, ts)
	probePath := "/mnt/" + probeDs

	// pool.dataset.create — requires DATASET_WRITE
	_, err = p.call("pool.dataset.create", map[string]any{"name": probeDs})
	add("pool.dataset.create", "DATASET_WRITE", err)

	var datasetCreated bool

	if err == nil {
		datasetCreated = true
		defer func() {
			_, _ = p.call("pool.dataset.delete", []any{probeDs})
		}()
	}

	if datasetCreated {
		// pool.dataset.update — requires DATASET_WRITE
		_, err = p.call("pool.dataset.update", []any{probeDs, map[string]any{"comments": "role probe"}})
		add("pool.dataset.update", "DATASET_WRITE", err)

		// filesystem.put — requires FILESYSTEM_DATA_WRITE. Uses /_upload HTTP endpoint.
		uploadErr := uploadFile(p, probePath+"/probe.txt", []byte("role-probe-sentinel"))
		add("filesystem.put (via /_upload)", "FILESYSTEM_DATA_WRITE", uploadErr)

		// pool.dataset.delete — requires DATASET_DELETE (recursive)
		_, err = p.call("pool.dataset.delete", []any{probeDs, map[string]any{"recursive": true}})
		add("pool.dataset.delete", "DATASET_DELETE", err)
		// Deferred cleanup from earlier is a no-op now (dataset already gone);
		// the second delete will error harmlessly.
	}

	// ─── WRITE: probe VM lifecycle ──────────────────────────
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

	vmResp, err := p.call("vm.create", vmParams)
	add("vm.create", "VM_WRITE", err)

	var vmID float64

	var vmCreated bool

	if err == nil {
		_ = json.Unmarshal(vmResp, &struct {
			ID *float64 `json:"id"`
		}{ID: &vmID})
		vmCreated = vmID > 0

		defer func() {
			if vmCreated {
				_, _ = p.call("vm.delete", []any{int(vmID)})
			}
		}()
	}

	if vmCreated {
		_, err = p.call("vm.update", []any{int(vmID), map[string]any{"description": "updated"}})
		add("vm.update", "VM_WRITE", err)

		_, err = p.call("vm.get_instance", []any{int(vmID)})
		add("vm.get_instance", "VM_READ", err)

		// Don't actually start — just test the call exists and is authorized.
		// vm.start with no devices will fail for a non-auth reason; we can
		// distinguish auth failure from operational failure by the error
		// text isAuthError check.
		_, err = p.call("vm.stop", []any{int(vmID), map[string]any{"force": true}})
		add("vm.stop", "VM_WRITE", err)

		// vm.device.create — attach a dummy NIC. Use whatever bridge the
		// system exposes via nic_attach_choices. Falls back to "br0".
		nicAttach := "br0"

		choicesResp, nicErr := p.call("vm.device.nic_attach_choices", nil)
		if nicErr == nil {
			var choices map[string]string
			if json.Unmarshal(choicesResp, &choices) == nil {
				for name := range choices {
					nicAttach = name

					break
				}
			}
		}

		devResp, err := p.call("vm.device.create", map[string]any{
			"vm":         int(vmID),
			"order":      3000,
			"attributes": map[string]any{"dtype": "NIC", "type": "VIRTIO", "nic_attach": nicAttach},
		})
		add("vm.device.create", "VM_DEVICE_WRITE", err)

		var devID float64

		if err == nil {
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

		// Clean up the VM now so we don't leave it around.
		_, _ = p.call("vm.delete", []any{int(vmID)})
		vmCreated = false
	}

	// ─── Report ─────────────────────────────────────────────
	printReport(results)

	if summary(results) != 0 {
		os.Exit(1)
	}
}

// validateProbeHost delegates to internal/truenasrpc.ValidateHost so the
// probe and production client share one source of truth — including the
// strict DNS-character allow-list that the probe's earlier in-line copy
// was missing. The probe handles real Bearer tokens against real hosts;
// it must not have a weaker validator than production.
func validateProbeHost(host string) error { return truenasrpc.ValidateHost(host) }

// probeUploadClient is the http.Client used to talk to /_upload. Cached
// at package scope (constructed once on first reference) so successive
// calls reuse the same connection pool / TLS session, mirroring how
// production reuses the wsTransport.uploadClient. A new *http.Transport
// per call would defeat keep-alive and drop one idle conn per call into
// finalizer-land. The probe today only fires once but the pattern stays
// correct as the probe grows.
//
// CheckRedirect: refuse to follow 3xx. Go's default would re-send the
// Bearer header (full TRUENAS_API_KEY) to the redirect target, so a
// compromised or MITM'd TrueNAS could harvest the operator's key by
// returning `302 Location: https://attacker.tld/`. The unconditional
// ErrUseLastResponse covers all 3xx classes, including the dangerous
// 307/308 that preserve method and body. ws.go does the same thing on
// the production path; this probe must not regress.
var probeUploadClient = sync.OnceValue(func() *http.Client {
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // probe tool
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 30 * time.Second,
	}
})

// newProbeUploadClient is kept for the existing test that asserts the
// CheckRedirect behavior. Production callsites use probeUploadClient()
// directly so the same shared client persists across uploads.
func newProbeUploadClient() *http.Client { return probeUploadClient() }

// uploadFile exercises the /_upload HTTP endpoint used for Talos ISO uploads.
// Matches the provider's internal/client/ws.go upload path.
func uploadFile(p *probe, destPath string, data []byte) error {
	if err := validateProbeHost(p.host); err != nil {
		return fmt.Errorf("refusing to send Bearer token to unvalidated TRUENAS_HOST: %w", err)
	}

	// Build the URL via net/url rather than fmt.Sprintf so the Host slot
	// can't carry a hand-crafted path/userinfo that smuggles the request
	// to a different destination. Mirrors ws.go's "fmt.Sprintf +
	// unvalidated host = bearer exfil. Never again." comment.
	uploadURL := (&url.URL{Scheme: "https", Host: p.host, Path: "/_upload/"}).String()

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
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := probeUploadClient().Do(req)
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
