# Storage Position Note — 2026-06-03

## What changed

democratic-csi was **dropped from the supported storage list** on
2026-06-03. Reason: insufficient project movement and no active
development.

Current supported set:

- **Longhorn** — recommended default for new clusters
- **NFS auto-storage** — simple alternative for read-heavy / shared-file workloads

Operators currently running democratic-csi can continue at their own
risk. The provider team will not test against it, will not ship
rotation drain-modes that target it, and will not accept issues that
depend on it.

Canonical position is in `docs/storage.md`. This file exists so
content drafts that pre-date the change are clearly flagged.

## Affected drafts

These unpublished marketing drafts still reference democratic-csi as
a recommended option. They MUST be re-edited before publication to
match the supported list above.

- `00-context/six-month-plan.md`
- `00-context/product-marketing.md`
- `01-month-1/01-hero-post.md`
- `01-month-1/02-cross-posts-reddit-linkedin-x.md`
- `01-month-1/03-youtube-v1-v2-scripts.md`
- `02-month-2/01-talos-vs-k3s.md`
- `02-month-2/02-truenas-vs-proxmox.md`
- `02-month-2/03-youtube-v3-v4-scripts.md`
- `02-month-2/05-cross-posts.md`
- `03-month-3/03-youtube-v5-storage.md` — **major rewrite needed** (storage-focused)
- `03-month-3/04-storage-deep-dive-written.md` — **major rewrite needed** (storage-focused)
- `03-month-3/05-cross-posts.md`
- `04-month-4/07-multi-host-cluster.md`
- `06-month-6/01-hub-page.md`
- `06-month-6/04-cross-posts.md`
- `_shared/linkedin-drumbeat.md`
- `_shared/reddit-lemmy-post-patterns.md`

## Rewrite guidance for the two storage-centric drafts

`03-month-3/04-storage-deep-dive-written.md` and
`03-month-3/03-youtube-v5-storage.md` lead with a three-way comparison
that places democratic-csi as a peer of Longhorn. They cannot be
published as-is.

Two viable rewrite shapes:

1. **Two-way comparison (Longhorn vs NFS).** Cleaner. The story
   becomes "block-replicated-in-cluster vs shared-file-from-NAS,"
   which is the actual decision new users face today.
2. **Three-way with democratic-csi as a cautionary tale.** Lead the
   democratic-csi section with the project-movement issue and frame
   it as "what happens when a CSI driver stops shipping releases" —
   a useful infra-decision-making lesson, but a much longer post and
   tonally different.

Default to (1) unless the publish calendar specifically wants a
contrarian / opinionated take.
