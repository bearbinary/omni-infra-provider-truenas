# Integration Test CI — Feasibility & Cost Analysis

Research into running integration tests against a real TrueNAS instance in CI (GitHub Actions).

---

## Current State

- **CI** ([`ci.yaml`](../.github/workflows/ci.yaml)): Runs `make build`, `make test` (unit only), vet, and format check on `ubuntu-latest`. No integration tests.
- **Integration tests exist** in `internal/client/integration_test.go` and `internal/cleanup/integration_test.go` (build tag `integration`). They test against a real TrueNAS via WebSocket or Unix socket.
- **Phase 1 tests don't need nested virt** — VMs are created with `autostart: false`, never boot. They need: a ZFS pool, a network interface, and API access. Minimum TrueNAS: 2 cores, 8 GB RAM, 32 GB boot + 2x20 GB data disks.
- **Phase 2 (E2E) needs nested virt** — VMs actually boot Talos. Much heavier: 8+ cores, 32 GB RAM.

---

## Architecture Options

### Option A: Persistent TrueNAS Instance (CI connects remotely)

A TrueNAS box runs 24/7 (or on-demand). CI jobs use `TRUENAS_TEST_HOST` + `TRUENAS_TEST_API_KEY` as GitHub secrets and connect via WebSocket.

**Pros**: Simple CI workflow (just add env vars), fast (no boot overhead), identical to production
**Cons**: Ongoing cost, state leaks between runs (mitigated by `omni-inttest-*` cleanup), single point of failure

### Option B: Ephemeral TrueNAS per CI Run

Boot TrueNAS inside the CI runner using QEMU/KVM. Clean slate every run.

**Pros**: No persistent infra, clean state
**Cons**: 5-10 min boot overhead per run, needs nested virt on the runner, complex setup (automated pool creation, API key, bridge config)

---

## Cost Comparison

### Phase 1 (API-only, no nested virt needed on TrueNAS)

| Option | Setup Cost | Ongoing Cost | Notes |
|---|---|---|---|
| **Existing TrueNAS box** | $0 | $0 | Just add GitHub secrets. Best starting point. |
| **Hetzner CPX31 cloud VM** | $0 | ~$13/mo | 4 vCPU, 8 GB. Phase 1 only, no nested virt. |
| **Hetzner AX42 bare metal** | ~$50 setup | ~$50/mo | Ryzen 5 3600, 64 GB RAM. Covers Phase 1 + 2. Best value for dedicated instance. |
| **AWS m5.xlarge on-demand** | $0 | ~$0.19/hr (~$140/mo 24/7) | Good if you stop/start on schedule. |
| **AWS m5.xlarge spot** | $0 | ~$0.06-0.08/hr | Cheapest cloud, but can be interrupted. |
| **GitHub 8-core larger runner** (ephemeral) | $0 | ~$1.92/hr (~$0.50/run) | Requires Team/Enterprise plan. Must boot TrueNAS in QEMU each run (+5-10 min). |
| **Self-hosted NUC/mini PC** | $200-500 once | ~$5-10/mo electric | Cheapest long-term. You maintain it. |

### Phase 2 (nested virt required for E2E)

Only bare metal or nested-virt-capable hosts work:

| Option | Cost | Notes |
|---|---|---|
| **Hetzner AX42** | ~$50/mo | Best value |
| **AWS `.metal` instances** | $4+/hr | Overkill and expensive |
| **Self-hosted hardware** | One-time $200-500 | Cheapest long-term |

---

## Recommended Approach

### Phase 1 (do now): Persistent instance + GitHub secrets

1. Use your existing TrueNAS instance (or a Hetzner CPX31 at ~$13/mo for isolation)
2. Add `TRUENAS_TEST_HOST` and `TRUENAS_TEST_API_KEY` as GitHub repository secrets
3. Add an integration test job to `ci.yaml`:

```yaml
integration:
  runs-on: ubuntu-latest
  # Only on pushes to main (fork PRs can't access secrets)
  if: github.event_name == 'push' || github.event_name == 'workflow_dispatch'
  needs: [test]
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version-file: go.mod
    - name: Integration tests
      env:
        TRUENAS_TEST_HOST: ${{ secrets.TRUENAS_TEST_HOST }}
        TRUENAS_TEST_API_KEY: ${{ secrets.TRUENAS_TEST_API_KEY }}
        TRUENAS_TEST_POOL: ${{ secrets.TRUENAS_TEST_POOL || 'tank' }}
        TRUENAS_TEST_NIC_ATTACH: ${{ secrets.TRUENAS_TEST_NIC_ATTACH || 'br0' }}
      run: make test-integration
```

**Cost: $0-13/mo** depending on whether you use an existing instance.

### Phase 2 (later): Hetzner AX42 for nested virt E2E

When full E2E tests are needed (VMs that boot Talos and join Omni):
- Get a Hetzner AX42 (~$50/mo)
- Install TrueNAS SCALE 25.04+
- Either run a GitHub self-hosted runner on it, or connect remotely via WebSocket

### What NOT to do

- **Don't use GitHub larger runners for ephemeral TrueNAS** — boot overhead and setup complexity aren't worth it when a persistent $13-50/mo instance is simpler and faster
- **Don't use AWS bare metal** — $4+/hr is overkill
- **Don't skip integration tests entirely** — unit tests mock the JSON-RPC layer, so real API drift won't be caught

---

## Security Considerations

- The TrueNAS test instance should be **isolated** (dedicated pool, no production data)
- API key should have **minimal privileges** (or use a dedicated test user)
- GitHub secrets are not available to PR workflows from forks (handled by the `if:` condition above)
- Consider a **firewall rule** allowing only GitHub Actions IPs, or use a VPN/Tailscale tunnel to the test instance

---

## TL;DR

| Question | Answer |
|---|---|
| **Is it feasible?** | Yes. Straightforward for Phase 1, Phase 2 needs bare metal. |
| **Cheapest option?** | Use your existing TrueNAS box ($0) or Hetzner cloud VM (~$13/mo) |
| **Best option?** | Hetzner AX42 bare metal (~$50/mo) — covers both Phase 1 and Phase 2 |
| **CI changes needed?** | Add one job to `ci.yaml` + 2-4 GitHub secrets |
| **Time to set up?** | ~1 hour for Phase 1 |
