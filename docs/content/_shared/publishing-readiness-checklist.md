# Publishing-Readiness Checklist

One place to track every placeholder, URL, and prerequisite that must be resolved before each piece can go live. Don't publish a piece with any unchecked item in its column.

---

## Global prerequisites (one-time setup, do before M1 publishes)

These need to be resolved before *any* piece in the campaign publishes. Resolve once, reuse across all pieces.

- [ ] **bearbinary.dev domain live** — every "About the author" block + several CTAs reference it. Either resolve to a real site or strip the references.
- [ ] **LinkedIn URL** — every piece has `[LinkedIn](#)` in the author block. Replace with real profile URL.
- [ ] **YouTube channel URL** — every piece has `[YouTube](#)` and `[channel link]` in the author block + CTAs. Replace with real channel URL.
- [ ] **Plausible (or PostHog) installed** on whatever domain serves the destination links. Per `_shared/analytics-setup.md`.
- [ ] **Google Search Console verified** for the same domain.
- [ ] **YouTube Studio email digest** configured (weekly).
- [ ] **Tracking spreadsheet created** per `_shared/analytics-setup.md` Tab 1/2/3 structure.
- [ ] **GitHub repo topics set**: `kubernetes`, `talos`, `truenas`, `omni`, `homelab`, `self-hosted`, `golang`, `infrastructure-provider`.
- [ ] **UTM convention locked** per `_shared/utm-conventions.md`. Confirm the docs site or repo redirect actually preserves query strings.
- [ ] **Recording setup tested** for YouTube — OBS configured, mic levels at -14 LUFS, lighting consistent.
- [ ] **Channel conventions locked** — cold open template, lower-third name plate, end-screen template, pinned-comment template.
- [ ] **Secret cleared from** `~/Library/Application Support/JetBrains/GoLand2026.1/scratches/scratch_1.txt` (Unifi API key on lines 7 + 12 from earlier session).

---

## Per-piece placeholders (resolve before each piece publishes)

Each piece has its own row. Mark checked when the placeholder is replaced with the real value across **every** file that references it.

### Files cross-reference each other heavily

Most cross-references are forward — e.g., the M1 hero post links to the (future) M3 sizing post. When you publish a later piece, **come back and update the earlier pieces** to swap the placeholder for the real link.

### Placeholder index — what needs replacement per piece

#### M1 — Hero post + cross-posts + V1/V2 scripts
- [ ] `<hero-post-slug>` → real dev.to URL slug, throughout the entire workspace (used in ~30 files)
- [ ] V1 YouTube URL captured + placed in `01-hero-post.md` + `02-cross-posts-reddit-linkedin-x.md` + `03-youtube-v1-v2-scripts.md`
- [ ] V2 YouTube URL captured + placed in same files + the description for V3 onward
- [ ] LinkedIn week-1 first-comment URLs ready (hero post + repo)

#### M2 — Comparisons + host-OOM + V3/V4 + cross-posts
- [ ] `<talos-vs-k3s-slug>` → real dev.to URL slug
- [ ] `<truenas-vs-proxmox-slug>` → real dev.to URL slug
- [ ] `<host-oom-war-story-slug>` → real dev.to URL slug
- [ ] V3 YouTube URL captured + placed in companion written post + cross-posts + drumbeat
- [ ] V4 YouTube URL captured + placed in companion written post + cross-posts + drumbeat
- [ ] v0.16.1 release notes URL real (in host-OOM post)

#### M3 — Sizing + SAST + storage + V5 + cross-posts
- [ ] `<sizing-post-slug>` → real dev.to URL slug
- [ ] `<sast-retro-slug>` → real dev.to URL slug
- [ ] `<storage-deep-dive-slug>` → real dev.to URL slug
- [ ] V5 YouTube URL captured
- [ ] M3 outreach email drafted + sent to one creator (TechnoTim / Jim's Garage / Lawrence Systems / Craft Computing) — pick before W12

#### M4 — Upgrade + singleton-lease + V6 + awesome-lists + podcast pitches + cross-posts
- [ ] `<upgrade-post-slug>` → real dev.to URL slug
- [ ] `<singleton-lease-slug>` → real dev.to URL slug
- [ ] V6 YouTube URL captured + cross-promo to the upgrade post live
- [ ] Awesome-Talos / Awesome-Kubernetes / Awesome-Selfhosted research checklist run **before** drafting submissions (per `04-awesome-list-submissions.md`)
- [ ] Self-Hosted Show pitch sent (W13)
- [ ] Changelog main pitch sent (W15)
- [ ] Go Time pitch sent (W16) — only after singleton-lease post publishes so the link in the pitch is real

#### M5 — Case study + 6-month retro + V7 + cross-posts
- [ ] **Real user recruited + interviewed** for the case study (per `01-case-study-template.md`). Start outreach in M3, not M5.
- [ ] All `[user]`, `[hardware]`, `[cluster shape]`, `[Pattern 1/2/3]` placeholders in `01-case-study-template.md` replaced with real interview content
- [ ] User has reviewed + approved the draft before public publish
- [ ] `<case-study-slug>` → real dev.to URL slug
- [ ] **All `[X]`, `[Y]`, `[N]`, `[Z]`, `[percentage]` placeholders in `02-six-month-retro-shareable.md`** replaced with real M6 analytics numbers
- [ ] `<retro-slug>` → real dev.to URL slug (the retro is the HN-bait piece — submit Tuesday morning ET, separate from Reddit)
- [ ] V7 YouTube URL captured
- [ ] M5 outreach: Sidero Labs community shout-out / blog mention request sent

#### M6 — Hub + Omni explainer + V8 + cross-posts
- [ ] `<hub-page-slug>` → real dev.to URL slug (hub page sets `canonical_url` to its own URL)
- [ ] `<omni-explainer-slug>` → real dev.to URL slug
- [ ] V8 YouTube URL captured (only record V8 after M6 analytics review is complete — the retro needs real numbers in slides)
- [ ] M6 analytics review done (per `_shared/analytics-setup.md`)
- [ ] Hub page failure-modes table updated to reflect current release
- [ ] Omni explainer submitted to HN (Tuesday morning ET, separate submission from Reddit/Lemmy)
- [ ] M7+ direction decided based on M6 review

---

## Per-publish pre-flight (run before *every* drop)

Quick gate before clicking publish on anything:

- [ ] **Read the piece aloud once.** Catches awkward phrasing.
- [ ] **Every placeholder `<...>` and `[...]` replaced** with real value.
- [ ] **UTMs appended** to outbound links per `_shared/utm-conventions.md`.
- [ ] **Add row to tracking spreadsheet** Tab 1 (Piece log).
- [ ] **Set canonical_url** if cross-posting to multiple platforms.
- [ ] **Pre-share with one trusted reader** for any post you're nervous about (especially the retro and the host-OOM war story).

### YouTube-specific pre-flight
- [ ] Chapter timestamps in the description match the actual timeline.
- [ ] Description's "Links" block has real URLs (not `<URL>` placeholders).
- [ ] Thumbnail uploaded.
- [ ] End-screen card pointing at next video set up.
- [ ] Pinned-comment text drafted and ready to drop in first 60 seconds after upload.

### LinkedIn-specific pre-flight
- [ ] First 2 lines = the hook (visible above the "see more" cutoff).
- [ ] Body under 1300 chars.
- [ ] No external link in body.
- [ ] Hashtags trimmed to 3–5.
- [ ] First-comment link ready to drop within 60 seconds.

### X-specific pre-flight
- [ ] Thread numbering removed (it shows up as cruft).
- [ ] Each tweet under 280 chars.
- [ ] Links unwrapped (X auto-cards).
- [ ] Thread reads cleanly when scrolled top-to-bottom in the preview.

### Reddit-specific pre-flight
- [ ] **Milestone framing confirmed** per `_shared/reddit-lemmy-post-patterns.md` — title leads with lesson, body delivers value before link.
- [ ] Sub's pinned rules + last week of top posts skimmed.
- [ ] Correct flair selected.
- [ ] Posted in the lowest-fit-tier sub first (r/truenas → r/selfhosted → r/homelab → r/kubernetes).
- [ ] Calendar block reserved for next 6 hours to be in comments.

### Lemmy-specific pre-flight
- [ ] **Milestone framing confirmed** (same as Reddit).
- [ ] URL field set to dev.to canonical (so OG card renders).
- [ ] Body delivers value, not pitch.
- [ ] First post to `!selfhosted@lemmy.world` before cross-posting to other communities.
- [ ] 24-hour gap planned before each subsequent cross-post.

---

## Post-publish actions (run after *every* drop)

- [ ] **Reply to every comment for first 4 hours** (LinkedIn, Reddit, Lemmy).
- [ ] **Reply to every YouTube comment for first 24 hours** then daily for the first week.
- [ ] **Update the tracking spreadsheet weekly** — Monday 9am, 15 minutes.
- [ ] **Come back to earlier pieces** and replace forward-reference placeholders (e.g., when V3 publishes, update M1's hero post link to "Companion video" with the real V3 URL).

---

## "Don't publish until" hard gates

A piece is **blocked from publishing** if any of these are true:

- [ ] A placeholder `<...>` or `[...]` is still in the post body.
- [ ] The piece references a URL that resolves to 404 (run a quick link-check before publishing).
- [ ] The piece claims a number you can't back up with real data (especially in the retro).
- [ ] The piece is a case study and the named user hasn't approved the draft.
- [ ] The piece references a release version that hasn't shipped yet.
- [ ] You don't have time to be in the comments for the first 6 hours after publish (delay to a day when you do).

---

## Quarterly: campaign health check

Every 3 months, run a quick audit:

- [ ] Are the placeholders in the tracking spreadsheet trending toward "replaced"?
- [ ] Are the analytics being updated weekly, or has the discipline slipped?
- [ ] Are pieces shipping on the planned cadence, or has work stalled?
- [ ] Is the audience growing on each channel, or has one stagnated?
- [ ] Are there issues open in the repo that came from cross-post readers?
- [ ] Has any external surface (TrueNAS apps catalog, Awesome-list, podcast) given inbound signal?

If 3+ of those answers are "no," that's a signal to either rebalance effort or revisit the plan. Don't grind through 6 months of work on a plan that isn't producing.
