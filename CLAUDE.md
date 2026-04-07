# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`omni-infra-provider-truenas` is a TrueNAS SCALE infrastructure provider for the Sidero Labs Omni platform, developed by Bear Binary. Licensed under MIT. It manages Talos Linux VMs on TrueNAS SCALE via the JSON-RPC 2.0 API.

**Requires TrueNAS SCALE 25.04+ (Fangtooth).** The legacy REST v2.0 API is NOT supported.

## Prerequisites

- **Go 1.26+**
- **golangci-lint v2+** — must be built with the same major Go version. If you see `unsupported version` errors from the linter, upgrade: `brew upgrade golangci-lint`

## Build & Test Commands

```bash
make build            # Build binary to _out/
make test             # Run all unit tests
make test-v           # Verbose tests
make test-integration # Integration tests (requires TRUENAS_TEST_HOST + TRUENAS_TEST_API_KEY)
make lint             # Run golangci-lint (requires v2+)
make image            # Build Docker image
make generate         # Regenerate protobuf from api/specs/specs.proto
```

## Architecture

Uses the standard Omni VM provider pattern with `infra.NewProvider()` + `provision.Step`:

- **Entry point**: `cmd/omni-infra-provider-truenas/main.go` — env var config, auto-detects transport (Unix socket or WebSocket), `infra.NewProvider()` registration with health check
- **TrueNAS JSON-RPC client**: `internal/client/` — Transport interface with Unix socket (zero-auth) and WebSocket (API key) implementations. VM CRUD, device attachment, storage operations via JSON-RPC 2.0 methods.
- **Provisioner**: `internal/provisioner/` — 3 provision steps (`createSchematic`, `uploadISO`, `createVM`) + `Deprovision()`
- **COSI resources**: `internal/resources/machine.go` — Machine typed resource backed by protobuf `api/specs/specs.proto`
- **Provider metadata**: `internal/resources/meta/meta.go` — `ProviderID = "truenas"`

### Transport auto-detection
1. Unix socket (`/var/run/middleware/middlewared.sock`) — most secure, no API key needed, used when running as TrueNAS app
2. WebSocket (`wss://<host>/websocket`) — for remote deployments, requires API key

### Key SDK packages
- `github.com/siderolabs/omni/client/pkg/infra` — Provider registration and lifecycle
- `github.com/siderolabs/omni/client/pkg/infra/provision` — Provision step framework
- `github.com/cosi-project/runtime` — COSI resource types

### Provision flow
1. `createSchematic` — Generate Talos image factory schematic with qemu-guest-agent extension
2. `uploadISO` — Download Talos nocloud ISO from Image Factory, upload to TrueNAS (SHA-256 dedup via singleflight)
3. `createVM` — Create zvol, VM, attach CDROM+DISK+NIC, start VM, poll for RUNNING

### Configuration
All via environment variables (`.env` file loaded automatically). Key ones: `OMNI_ENDPOINT`, `OMNI_SERVICE_ACCOUNT_KEY`, `TRUENAS_HOST` (remote only), `TRUENAS_API_KEY` (remote only), `DEFAULT_POOL`, `DEFAULT_NIC_ATTACH`. See `.env.example`.

## Documentation Map

- **`README.md`** — Project overview, badges, quick start, configuration reference, usage guide
- **`AGENT.md`** — Step-by-step setup guide designed for AI assistants to walk users through deployment
- **`CONTRIBUTING.md`** — Issues-only contribution model (no PRs accepted)
- **`SECURITY.md`** — Vulnerability disclosure policy
- **`CHANGELOG.md`** — Curated release history (v0.1.0 through current)
- **`llms.txt`** — Structured plaintext for AI/LLM discoverability
- **`docs/architecture.md`** — Detailed architecture with Mermaid diagrams (system context, provision sequence, transport detection)
- **`docs/troubleshooting.md`** — Common issues and solutions (startup failures, provisioning, debugging)
- **`docs/testing.md`** — Unit, integration, and E2E test setup
- **`docs/storage.md`** — CSI storage options guide: NAS-backed (NFS, iSCSI, democratic-csi) vs node-local (Longhorn, Ceph, Mayastor) with recommendations
- **`docs/cni.md`** — CNI selection guide: Flannel (default), Cilium, Calico with Talos-specific setup steps
- **`docs/networking.md`** — Networking guide: bridges, DHCP reservations, MetalLB, VIP, UniFi/pfSense/OPNsense/Mikrotik setup
- **`docs/backlog.md`** — Feature roadmap and backlog items
- **`docs/getting-started.md`** — Beginner tutorial: NAS to running Kubernetes cluster, no prior experience needed
- **`docs/upgrading.md`** — Version upgrade guide and breaking changes
- **`docs/provider-research.md`** — Internal research: analysis of all existing Omni providers (architecture patterns, boot methods, SDK usage)

## Contribution Model

This project uses an issues-only contribution model. No pull requests are accepted. Contributors open issues describing what they want, and can optionally prototype in a fork. The team handles merges.
