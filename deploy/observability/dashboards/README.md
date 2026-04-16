# Grafana Dashboards — Marketplace Descriptions

Ready-to-submit descriptions for publishing the bundled dashboards on [grafana.com/grafana/dashboards](https://grafana.com/grafana/dashboards/). Each section maps to one JSON file and matches the fields of the Grafana Labs dashboard upload form (Name, Summary, Description, Tags, Required data sources).

All four dashboards scrape metrics emitted by the [`omni-infra-provider-truenas`](https://github.com/bearbinary/omni-infra-provider-truenas) service — a Sidero Omni infrastructure provider for TrueNAS SCALE. Metrics are prefixed `truenas_*` and exposed via OpenTelemetry on `/metrics`. A `$job` template variable scopes all queries to the provider job, and dashboards cross-link with exemplars from Prometheus into Tempo traces, Loki logs, and Pyroscope profiles.

## Common Requirements

- **Prometheus** — scraping `truenas_*` metrics from the provider (service name `omni-infra-provider-truenas`).
- **Loki** (optional, for log panels) — with `service_name="omni-infra-provider-truenas"` label.
- **Tempo** (optional, for trace panels) — exemplars linked by `traceID`.
- **Pyroscope** (optional, for flamegraph panels) — CPU, alloc, goroutine, and mutex profiles from the same service.
- **Provider version**: v0.14.x or later (all metrics referenced here are emitted).

---

## 1. Overview — `overview.json`

**Name:** Omni TrueNAS Provider — Overview

**Summary:** High-level health of the Sidero Omni TrueNAS infrastructure provider: running VMs, ZFS pool health, provisioning activity, and singleton lease status at a glance.

**Description:**

A single-pane-of-glass dashboard for operators running the `omni-infra-provider-truenas` service. Shows whether the provider is healthy, whether the TrueNAS host it manages has capacity, and what the VM fleet is doing right now. Use this as the home dashboard for your Omni provider deployment — drill into **Provisioning**, **TrueNAS API Performance**, or **Cleanup & Maintenance** for detail.

**Panels:**

- **Provider Status row** — Running VMs, total VMs provisioned, VM errors, health-check errors, singleton lease held, host CPU cores, host memory, total disks, provider version stat.
- **Pool Health row** — ZFS pool healthy indicator, pool usage %, pool free space over time.
- **VM Activity row** — Provision / deprovision / error rate, running VMs over time.
- **Logs & Profiles row** — Recent provider logs (Loki) and CPU flamegraph (Pyroscope).

**Tags:** `truenas`, `omni`, `overview`, `kubernetes`, `talos`, `zfs`, `virtualization`

**Required data sources:** Prometheus (required), Loki and Pyroscope (optional, for the bottom row).

---

## 2. VM Provisioning — `provisioning.json`

**Name:** Omni TrueNAS Provider — VM Provisioning

**Summary:** Step-by-step VM provisioning performance for the Omni TrueNAS provider — latency percentiles, per-step breakdown, error categories, and Talos ISO cache efficiency.

**Description:**

Deep-dive dashboard for operators debugging slow or failing VM creation. Splits end-to-end provision and deprovision latency into the four provider steps (`createSchematic`, `uploadISO`, `createVM`, `healthCheck`) so you can pinpoint which step regressed. Surfaces error category distribution, ISO cache hit rate, singleton lease takeovers, and zvol resize activity. Linked traces and flamegraphs let you follow a slow provision from metric → span → flame profile.

**Panels:**

- **Provision Duration row** — Provision and deprovision p50 / p95 / p99 latency.
- **Step Breakdown row** — Per-step p95 latency for each of the four provision steps and both deprovision steps.
- **Error Breakdown row** — Provision errors by category (rate + distribution), zvol resizes, VMs auto-replaced, VM error rate.
- **Singleton & Disks row** — Singleton lease refresh errors and takeovers, count of additional data disks.
- **ISO Management row** — ISO cache hit-rate gauge, hits vs misses over time, ISO download p95, ISO upload bytes/s.
- **Traces & Logs row** — Recent provision traces (Tempo) and error/warn logs (Loki).
- **Profiling row** — CPU and allocation flamegraphs scoped to provision hot paths (Pyroscope).

**Tags:** `truenas`, `omni`, `provisioning`, `kubernetes`, `talos`, `performance`

**Required data sources:** Prometheus (required), Tempo / Loki / Pyroscope (optional, for the bottom rows).

---

## 3. TrueNAS API Performance — `api-performance.json`

**Name:** Omni TrueNAS Provider — TrueNAS API Performance

**Summary:** TrueNAS JSON-RPC 2.0 API call latency, call rates, rate-limit queue depth, and WebSocket reconnects from the Omni infrastructure provider.

**Description:**

Observability for the WebSocket JSON-RPC 2.0 client the provider uses to drive TrueNAS SCALE (25.04+ Fangtooth). Shows aggregate latency percentiles, per-method breakdowns (so you can see which TrueNAS call is slow), rate-limiter queue depth (so you know when you're back-pressuring TrueNAS), and WebSocket reconnect counts (so you catch transport flapping early). Traces and Loki logs are wired up for deep-dive on individual slow calls.

**Panels:**

- **API Latency row** — Aggregate p50 / p95 / p99 call duration, call rate, WebSocket reconnects, health-check errors, rate-limit queue size.
- **Per-Method Breakdown row** — p95 by method, call rate by method, top-10 slowest methods by average duration (last 5 min).
- **Connection Health row** — Rate-limit queue depth over time, WebSocket reconnect rate.
- **Traces & Logs row** — Slowest API traces (> 1 s, Tempo) and API error logs (Loki).
- **Profiling row** — Goroutine and mutex-contention flamegraphs (Pyroscope).

**Tags:** `truenas`, `omni`, `api`, `jsonrpc`, `websocket`, `performance`

**Required data sources:** Prometheus (required), Tempo / Loki / Pyroscope (optional).

---

## 4. Cleanup & Maintenance — `cleanup.json`

**Name:** Omni TrueNAS Provider — Cleanup & Maintenance

**Summary:** Background cleanup outcomes for the Omni TrueNAS provider: stale ISO removal, orphan VM / zvol reaping, and graceful-vs-forced shutdown ratios.

**Description:**

Dashboard for keeping the TrueNAS host tidy. The provider runs periodic reconciliation sweeps that reap stale Talos ISOs, orphan VMs (VMs on TrueNAS with no matching Omni MachineRequest), and orphan zvols (zvols with no owning VM). Use this dashboard to confirm those sweeps are running, to watch trends (a rising orphan rate means something upstream is leaking), and to measure how often VM shutdowns hit the graceful ACPI path vs. the force-stop fallback — a key indicator of `qemu-guest-agent` health inside Talos VMs.

**Panels:**

- **Cleanup Totals row** — Cumulative counters for stale ISOs removed, orphan VMs removed, orphan zvols removed, deprovisioned VMs.
- **Cleanup Rates row** — Cleanup operations (ISO / orphan VM / orphan zvol) over time, deprovision rate.
- **Graceful Shutdown row** — Graceful vs. forced pie chart, rate over time, graceful-shutdown-rate gauge (% of shutdowns that completed gracefully).
- **Traces & Logs row** — Recent cleanup and deprovision traces (Tempo), cleanup/orphan/deprovision/shutdown log stream (Loki).

**Tags:** `truenas`, `omni`, `cleanup`, `maintenance`, `kubernetes`, `talos`

**Required data sources:** Prometheus (required), Tempo / Loki (optional).

---

## Publishing Notes

When uploading to grafana.com:

1. Export each JSON via Grafana (**Share → Export → Save to file**) or use the file directly from this directory.
2. Paste the matching section above into the **Description** field (Grafana renders markdown).
3. Copy the **Tags** line into the tag selector.
4. Set the **Requires** field to the data sources listed (Prometheus is required; Loki, Tempo, and Pyroscope are optional and only affect specific panel rows).
5. Pin the dashboard to a specific provider version by noting the metric set (v0.14.x+) in the description.
