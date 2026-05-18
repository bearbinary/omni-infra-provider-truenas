# Product Marketing Context — omni-infra-provider-truenas

This file feeds the marketing skills (content-strategy, copywriting, social, ad-creative, launch, video, etc.) so they don't have to re-ask foundational questions. Keep it short, factual, and current.

---

## Person & Brand

**Maintainer**: Zac Clifton — infrastructure engineer. Solo author of this project. Operates as **Bear Binary** for repo / org branding, but the marketing goal is to grow **Zac's personal brand**, with Bear Binary as the shop he ships under.

**Authority position**: pragmatic, opinionated builder of self-hosted Kubernetes infrastructure. Lives in the Talos / Omni / TrueNAS SCALE / ZFS world. Daily-drives the stack he writes about (TrueNAS SCALE 25.10.1 + Omni cloud).

**Voice**: technical, direct, honest, no hype. Names tradeoffs. Doesn't pretend hobby gear is enterprise. Comfortable writing build-in-public devlogs that admit failure.

**Opinions to lean on (already-formed)**:
- Talos is immutable — replace, don't rollback. Velero for PVCs.
- Storage: democratic-csi primary; Longhorn alt; nfs-subdir untrusted.
- Niche infra tools should fail loud at the schema/UX boundary, not at runtime.
- Issues-only contribution model > PR firehose for a single-maintainer project.
- Pre-release soak windows are non-negotiable for anything shipped via a third-party catalog.

---

## Product

**Name**: `omni-infra-provider-truenas`
**One-liner**: open-source Omni provider that turns a TrueNAS SCALE 25.04+ box into a Talos Linux VM fleet — managed Kubernetes on the NAS you already own.
**License**: MIT. **Repo**: GitHub (Bear Binary org). **Distribution**: TrueNAS apps community catalog (`truenas/apps` → `ix-dev/community/`).
**Tech**: Go 1.26+, JSON-RPC 2.0 over WebSocket, COSI resources, Sidero Labs Omni SDK.
**Current version**: v0.16.1 line (as of 2026-05).

---

## ICP

**Primary**: Homelab + small-team Kubernetes operators who already own a TrueNAS SCALE 25.04+ box, want managed Talos via Sidero Omni, and don't want a second hypervisor (Proxmox/ESXi) just to run K8s. Tech-comfortable, opinionated, value transparency.

**Secondary**: Sidero Omni users evaluating non-cloud providers; Talos enthusiasts; r/selfhosted "production-at-home" crowd; small infra teams ($0–$5k/mo infra budget) standardizing on Talos.

**Out of scope for now**: enterprise Omni shops, cloud-only Kubernetes users, k3s-on-Raspberry-Pi audience (different problem).

---

## Goals

1. **Personal brand** — Zac Clifton recognized as an authority on self-hosted Kubernetes infrastructure, specifically the Talos + Omni + TrueNAS path.
2. **Catalog adoption** — installs from the TrueNAS apps catalog.
3. **Community signal** — GitHub stars, real issues opened (issues-only model), forum/Discord mentions.
4. **Authority assets** — a canonical hub page that ranks for `kubernetes on truenas scale`.

---

## Channels (current, ranked by ICP fit)

1. **YouTube** (new, planned) — face-cam + screencast. Zac as host.
2. **r/selfhosted, r/homelab, r/kubernetes, r/truenas** — primary watering hole.
3. **TrueNAS Community Forum** (`forums.truenas.com/c/developer/27`) — already a referenced channel.
4. **Sidero Labs Talos Slack / Discord** — small but exact-fit ICP.
5. **Hacker News** — reserved for strong build-in-public + comparison pieces.
6. **LinkedIn** — primary text-social for personal brand to recruiters / engineering leaders.
7. **Mastodon + Bluesky** — homelab tags, low effort.
8. **GitHub topics** — repo discovery.

No existing creator/podcast relationships yet (M3 of plan adds outreach).

---

## Constraints

- **Solo maintainer** — content budget ≈ 3 written pieces + 1 YouTube video + ≈4 LinkedIn posts per month.
- **Issues-only contribution model** — explicitly no PRs accepted on the main repo. Communicate this clearly so the OSS-curious don't bounce.
- **No unauthorized releases** — content cycle never blocks on or assumes a release; releases require explicit per-version authorization per user feedback memory.
- **No unsolicited upstream PRs** — submissions to Awesome-* lists / catalogs go through the `public-pr-guard` flow (draft handed to maintainer, never auto-submitted). TrueNAS apps catalog is off the table for unsolicited PRs.
- **Mermaid only** for any diagrams — no ASCII art.

---

## Differentiators (already-validated, lean on these)

- Only Omni provider that targets TrueNAS SCALE specifically — Sidero's own list does not include one.
- WebSocket-only (no socket fallback) since TrueNAS 25.10 removed implicit Unix-socket auth — Zac shipped this transition.
- Singleton-lease leader election implemented in-product because the Omni SDK has none.
- Cassette-based integration tests (42 cassettes) — CI runs against recorded TrueNAS responses without hardware. Story-worthy.
- Recent v0.16.1 work surfaced host-OOM bugs via min_memory / memory schema validation that fails loud at the boundary.

---

## Pillars (locked from 6-month plan, 2026-05)

1. **P1 — Kubernetes on TrueNAS SCALE (the canonical path)** — searchable hub + spokes.
2. **P2 — Production-grade homelab K8s** — sizing, storage, CNI, networking, backups.
3. **P3 — Build-in-public (Zac's brand pillar)** — devlogs, retrospectives, hard-parts deep dives.
4. **P4 — Comparisons & decisions** — Talos vs k3s, TrueNAS vs Proxmox, storage drivers.

---

## Update policy

When the product positioning shifts (new pillar, ICP change, channel added, opinion changes), update this file before running the next marketing skill — don't let context drift live only in chat.
