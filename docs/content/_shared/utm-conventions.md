# UTM Conventions

UTMs let you attribute repo / docs traffic back to the channel that drove it. Be consistent or analytics is noise.

## Format

```
?utm_source=<channel>&utm_medium=<surface>&utm_campaign=<piece>
```

- **`utm_source`**: the platform (devto, linkedin, x, reddit, youtube, forum, podcast, email)
- **`utm_medium`**: the surface within that platform (post, social, thread, comment, video, description, organic)
- **`utm_campaign`**: the specific piece (hero-2026-05, talos-vs-k3s-2026-06, sast-retro-2026-07, etc.)

## Source values

| Source | Use for |
|---|---|
| `devto` | dev.to posts (canonical URLs) |
| `linkedin` | LinkedIn native posts + comments |
| `x` | X (Twitter) threads and single posts |
| `reddit` | Reddit posts — add `utm_medium=<subreddit>` to distinguish |
| `youtube` | YouTube video descriptions and end-screen links |
| `forum-truenas` | TrueNAS Community Forum threads |
| `slack-talos` | Talos Slack drops |
| `discord-sidero` | Sidero Discord drops |
| `podcast-<name>` | Specific podcast episode (e.g., `podcast-selfhosted-show`) |
| `awesome-<list>` | Awesome-list inbound (e.g., `awesome-talos`) |
| `direct` | Default GitHub README / docs site direct |

## Medium values

| Medium | Use for |
|---|---|
| `post` | Native text post |
| `social` | Generic social referral (when source = linkedin/x and you're not distinguishing thread vs post) |
| `thread` | Specifically a thread (X) |
| `comment` | First-comment link drop (LinkedIn, Reddit, etc.) |
| `video` | Video description |
| `description` | YouTube video description card |
| `organic` | Organic traffic from search/discovery |
| `<subreddit>` | When source = reddit, use sub name as medium: `selfhosted`, `homelab`, `kubernetes`, `truenas` |

## Campaign values

Convention: `<piece-slug>-<year>-<month>`.

Examples:
- `hero-2026-05` — the M1 hero post
- `talos-vs-k3s-2026-06` — the M2 Talos vs k3s comparison
- `truenas-vs-proxmox-2026-06` — the M2 TrueNAS vs Proxmox comparison
- `sizing-2026-07` — the M3 sizing post
- `sast-retro-2026-07` — the M3 SAST retrospective
- `storage-video-2026-07` — the M3 V5 storage YouTube video

## Worked examples

### M1 hero post (dev.to)

URL inside the post → repo:
```
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=devto&utm_medium=post&utm_campaign=hero-2026-05
```

### M1 LinkedIn first-comment link

```
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=linkedin&utm_medium=comment&utm_campaign=hero-2026-05
```

### M1 X thread T10 link

```
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=x&utm_medium=thread&utm_campaign=hero-2026-05
```

### M1 Reddit r/truenas link

```
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=reddit&utm_medium=truenas&utm_campaign=hero-2026-05
```

### M1 V2 YouTube description link

```
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=youtube&utm_medium=description&utm_campaign=hero-2026-05
```

### M2 Talos vs k3s on dev.to

```
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=devto&utm_medium=post&utm_campaign=talos-vs-k3s-2026-06
```

### M2 V3 YouTube description

```
https://github.com/bearbinary/omni-infra-provider-truenas?utm_source=youtube&utm_medium=description&utm_campaign=talos-vs-k3s-2026-06
```

## Rules

1. **Never reuse a campaign for a different piece.** Each piece gets its own slug.
2. **Always lowercase, dash-separated.** No spaces, no underscores, no caps.
3. **Always include all three params.** Missing `utm_medium` is the most common mistake.
4. **The repo's GitHub homepage strips query strings on display** but the params still reach your analytics provider if you've redirected the repo URL through a tracking-aware host (e.g., docs site at bearbinary.dev → repo) — see `analytics-setup.md`. If you don't have a tracking host yet, point UTMs at the docs site URL instead of the bare repo.
5. **Never UTM a link inside the canonical dev.to post itself.** UTMs in the canonical hurts SEO (looks like duplicate content). Only UTM cross-posts/social references.

## Tracking sheet

Maintain a simple tracking sheet (Google Sheets or local) with one row per (campaign × source × medium). After each piece runs, paste the clicks/visits + downstream events (issues opened, stars). Review monthly.

Columns:

| campaign | source | medium | URL | published_at | clicks | conv | notes |
|---|---|---|---|---|---|---|---|
| hero-2026-05 | devto | post | <full URL> | 2026-05-15 | | | |
| hero-2026-05 | linkedin | comment | <full URL> | 2026-05-16 | | | |
| ... | | | | | | | |

Don't over-engineer this. A spreadsheet is fine.
