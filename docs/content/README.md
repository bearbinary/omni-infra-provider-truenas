# Marketing Workspace — omni-infra-provider-truenas

Personal-brand marketing plan for Zac Clifton + the `omni-infra-provider-truenas` project. 6-month content roadmap, all drafts, distribution materials, and tracking.

**Location**: `docs/content/` inside the repo. **Not** published as part of the mkdocs documentation site (`mkdocs.yml` has `exclude_docs: content/` so this whole tree is excluded from the build).

**Why colocated with the repo**: version controlled, single source of truth, accessible from any clone, links to source code (e.g., `internal/singleton/singleton.go`) stay valid.

## Folder structure

```
docs/content/
├── README.md                       ← you are here
├── 00-context/                     ← always read first
│   ├── product-marketing.md        ← positioning, ICP, voice, constraints
│   └── six-month-plan.md           ← month-by-month roadmap
├── 01-month-1/                     ← Plant the flag (hero post + channel launch)
│   ├── 01-hero-post.md             ← "Kubernetes on TrueNAS SCALE: the Talos + Omni Path"
│   ├── 02-cross-posts-reddit-linkedin-x.md
│   └── 03-youtube-v1-v2-scripts.md
├── 02-month-2/                     ← Comparisons (be the answer for evaluators)
│   ├── 01-talos-vs-k3s.md
│   ├── 02-truenas-vs-proxmox.md
│   ├── 03-youtube-v3-v4-scripts.md
│   ├── 04-host-oom-war-story.md    ← shareable: schema-validation lesson from v0.16.1
│   └── 05-cross-posts.md           ← Reddit + Lemmy drops, milestone-framed
├── 03-month-3/                     ← Production hardening + first creator outreach
│   ├── 01-sizing-post.md
│   ├── 02-sast-retro-shareable.md
│   ├── 03-youtube-v5-storage.md
│   ├── 04-storage-deep-dive-written.md  ← written companion to V5
│   └── 05-cross-posts.md           ← Reddit + Lemmy drops, milestone-framed
├── 04-month-4/                     ← Deepen authority + first podcast outreach
│   ├── 01-upgrade-post.md
│   ├── 02-singleton-lease-shareable.md
│   ├── 03-youtube-v6-upgrade.md
│   ├── 04-awesome-list-submissions.md
│   ├── 05-podcast-pitches.md
│   └── 06-cross-posts.md           ← Reddit + Lemmy drops, milestone-framed
├── 05-month-5/                     ← Social proof
│   ├── 01-case-study-template.md
│   ├── 02-six-month-retro-shareable.md
│   ├── 03-youtube-v7-networking.md
│   └── 04-cross-posts.md           ← Reddit + Lemmy drops, milestone-framed
├── 06-month-6/                     ← Consolidate + plan v2
│   ├── 01-hub-page.md
│   ├── 02-omni-explainer-shareable.md
│   ├── 03-youtube-v8-retro.md
│   └── 04-cross-posts.md           ← Reddit + Lemmy drops, milestone-framed
└── _shared/
    ├── linkedin-drumbeat.md             ← weekly LinkedIn templates (M1–M6, 24 weeks)
    ├── reddit-lemmy-post-patterns.md    ← milestone framing playbook (READ FIRST)
    ├── x-communities.md                 ← X Communities + hashtag strategy
    ├── lemmy-communities.md             ← Lemmy communities + cross-post cadence
    ├── utm-conventions.md               ← UTM format + per-channel codes
    ├── analytics-setup.md               ← Plausible/PostHog + GSC + YT Studio setup
    └── publishing-readiness-checklist.md  ← gate every piece on this before publishing
```

## How to use

1. Read `00-context/product-marketing.md` first if you're picking this up after a break — that's the foundation.
2. Work month-by-month. Don't jump ahead. Each month's content builds on the last.
3. Within a month, ship in the order indicated by the filename prefix (`01-`, `02-`, `03-`).
4. Before publishing **any** piece: replace all `<placeholder>` strings (live URLs, dev.to slugs).
5. After publishing: come back to the file and replace placeholders with live URLs so future cross-promotion finds the right links.

## Cadence summary (from `00-context/six-month-plan.md`)

| | Per month |
|---|---|
| Long-form written | 1–2 |
| Shareable post (devlog / retro / comparison) | 1 |
| YouTube video | 1 |
| LinkedIn post | ~4 (weekly) |
| Reddit / forum drops | staggered, 1/week max |

## Publishing rules

- **Personal brand first**: bylined "Zac Clifton" on every piece. Bear Binary is the repo, Zac is the face.
- **No simultaneous cross-posting**: stagger Reddit by 3+ days per sub. r/truenas first to tune hooks.
- **No unsolicited upstream PRs**: any catalog/Awesome-list submission goes through `public-pr-guard` flow.
- **No releases as part of marketing**: content cycle never blocks on or triggers a release.

## Tracking

See `_shared/analytics-setup.md` for the tooling. See `_shared/utm-conventions.md` for the URL convention. At the end of M3 and M6, review what compounded.

## Before publishing anything

Run `_shared/publishing-readiness-checklist.md` first. It tracks every global prerequisite + per-piece placeholder + per-publish pre-flight + post-publish action. Don't ship a piece with any unchecked item in its column.

## mkdocs exclusion

This entire tree (`docs/content/`) is excluded from the public docs site via `exclude_docs: content/` in the repo's `mkdocs.yml`. Verify after any mkdocs.yml change by running `mkdocs serve` locally and confirming none of these files appear in the built navigation or search index.
