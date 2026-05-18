# Six-Month Marketing Plan — omni-infra-provider-truenas

**Started**: 2026-05-15. **Owner**: Zac Clifton (solo). **Goal**: personal brand + product adoption.

## Pillars (locked)

1. **P1 — Kubernetes on TrueNAS SCALE (the canonical path)** — searchable hub + spokes.
2. **P2 — Production-grade homelab K8s** — sizing, storage, CNI, networking, backups.
3. **P3 — Build-in-public (Zac's brand pillar)** — devlogs, retrospectives, hard-parts deep dives.
4. **P4 — Comparisons & decisions** — Talos vs k3s, TrueNAS vs Proxmox, storage drivers.

## Cadence

| Slot | Monthly | Purpose |
|---|---|---|
| Long-form written | 1–2 | SEO + authority |
| Shareable post | 1 | HN/Reddit/LinkedIn fuel |
| YouTube video | 1 | Personal brand, face-recognition |
| LinkedIn post | 4 (weekly) | Brand drumbeat |
| Distribution-only (forum reply, list submission, podcast pitch) | 1 | Watering-hole presence |

---

## Month 1 — Plant the flag

**Theme**: launch channel + own the anchor search.

- **Written (P1)**: "Kubernetes on TrueNAS SCALE: the Talos + Omni Path (2026 Guide)" — hero post, canonical install.
- **Shareable (P3)**: ALREADY PUBLISHED — [TrueNAS killed Kubernetes — so I brought it back](https://dev.to/cliftonz/truenas-killed-kubernetes-so-i-brought-it-back-4n7h) on dev.to.
- **YouTube**: V1 (face-cam channel intro, 3–5min) + V2 (screencast walkthrough, 12–15min).
- **Distribution**: TrueNAS forum announcement thread, GitHub topics set, Plausible/UTMs wired.
- **LinkedIn**: 4 posts repurposing hero + V1/V2 + dev.to.

**Deliverables**: `01-month-1/01-hero-post.md`, `01-month-1/02-cross-posts-reddit-linkedin-x.md`, `01-month-1/03-youtube-v1-v2-scripts.md`.

---

## Month 2 — Comparisons (be the answer for evaluators)

**Theme**: rank for comparison searches; Zac as honest reviewer.

- **Written (P4)**: "Talos + Omni on TrueNAS vs k3s in a TrueNAS VM" — target `talos vs k3s truenas`.
- **Written (P4)**: "Running Kubernetes on TrueNAS vs Proxmox" — target `proxmox vs truenas kubernetes`.
- **Shareable (P3)**: "How a min_memory/memory mismatch silently OOMed my host" — v0.16.1 war story (draft TBD).
- **YouTube**: V3 (Talos vs k3s, 9–11min) + V4 (TrueNAS vs Proxmox, 10–12min).
- **Distribution**: 3 Reddit/Talos Slack threads where comparisons come up — answer with link.
- **LinkedIn**: weekly comparison takeaway, OOM war story, V3 clip, V4 clip.

**Deliverables**: `02-month-2/01-talos-vs-k3s.md`, `02-month-2/02-truenas-vs-proxmox.md`, `02-month-2/03-youtube-v3-v4-scripts.md`.

---

## Month 3 — Production hardening + first creator outreach

**Theme**: graduate from "cool, it boots" to "I trust it" + open doors.

- **Written (P2)**: "Sizing Talos control planes on TrueNAS" — standalone post from `docs/sizing.md`.
- **Shareable (P3)**: "A SAST sweep on a 5kLOC Go provider: what 6 findings taught me" — recent v0.16.1 work.
- **YouTube**: V5 storage deep-dive — Longhorn vs democratic-csi vs NFS.
- **Outreach**: cold but specific email to **one** creator (TechnoTim / Jim's Garage / Lawrence Systems / Craft Computing). Offer a pre-built demo cluster, not a sponsored video.
- **LinkedIn**: weekly sizing rule of thumb, SAST lesson, V5 storage video, outreach learning.

**Deliverables**: `03-month-3/01-sizing-post.md`, `03-month-3/02-sast-retro-shareable.md`, `03-month-3/03-youtube-v5-storage.md`.

---

## Month 4 — Deepen authority (technical credibility)

**Theme**: Zac as systems engineer.

- **Written (P1)**: "Upgrading a Talos cluster managed by Omni on TrueNAS, without breaking ZFS" — target `upgrade talos omni`.
- **Shareable (P3)**: "The singleton-lease pattern: leader election when your SDK doesn't have one" — code-heavy, r/golang + Go Weekly pitch.
- **YouTube**: V6 — upgrading Talos on TrueNAS live, real cluster on screen.
- **Distribution**: `public-pr-guard` flow — draft Awesome-Talos / Awesome-Kubernetes additions. Pitch one podcast (Self-Hosted Show, Changelog, Go Time).
- **LinkedIn**: weekly upgrade philosophy, leader-election snippet, V6 clip, "boring parts that matter" reflection.

---

## Month 5 — Social proof + user spotlight

**Theme**: it's not just Zac running this.

- **Written (P4 case study)**: recruit 1–2 early adopters (mine GitHub issues + TrueNAS forum thread). "How [user] runs Kubernetes on a single TrueNAS box."
- **Shareable (P3)**: "6 months shipping a niche infra tool: what worked, what didn't" — HN-bait done right with numbers.
- **YouTube**: V7 — networking for TrueNAS-hosted Kubernetes (bridges, MetalLB, DHCP, jumbo frames).
- **Outreach**: ask Sidero Labs for a community shout-out / blog mention.
- **LinkedIn**: weekly case-study highlight, "what compounded" pull-quote, networking diagram, 6-month retro thread.

---

## Month 6 — Consolidate + plan v2

**Theme**: harvest the assets. Set the next bet.

- **Written (P1 hub)**: "Self-hosted Kubernetes on TrueNAS SCALE: the complete guide" — interlinks every M1–M5 spoke.
- **Shareable (P3)**: "What an Omni provider actually is, and why I built one for a NAS" — broad explainer for K8s audiences who've never heard of Omni.
- **YouTube**: V8 — "6 months of building an infra provider in public — the numbers and the lessons."
- **Review**: analytics readout (subs, stars, issues, installs, search impressions). Decide M7+ bet (SEO depth / YouTube production / community / new pillar).
- **LinkedIn**: weekly hub launch, Omni explainer thread, V8 retro clip, "what's next" forward-look.

---

## Measurement targets (lock in M1)

| Metric | Source | M6 review |
|---|---|---|
| YouTube subs + watch time | YT Studio | growth trend |
| GitHub stars / issues / forks | repo insights | did monthly content map to spikes? |
| Search Console impressions/clicks per pillar | GSC | which pillar compounded? |
| LinkedIn follower growth + post impressions | LI analytics | personal brand trajectory |
| Repo referrals from Reddit / HN / forum / YT | Plausible/PostHog | channel ROI |
| Qualitative: forum/Discord mentions of "Zac" / "Bear Binary's provider" | manual scan | authority signal |

## Constraints (do not violate)

- Solo maintainer — 3 written + 1 video + 4 LinkedIn per month max.
- Issues-only contribution model — no PRs accepted on main repo.
- No unauthorized releases — content cycle never blocks on or assumes a release.
- No unsolicited upstream PRs — Awesome-* lists, catalog bumps go through `public-pr-guard`.
- Mermaid only for diagrams — no ASCII art.
