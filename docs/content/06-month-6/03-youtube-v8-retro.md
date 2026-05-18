# YouTube Script: V8 — 6-Month Retrospective (M6)

The retrospective. Different shape from every other video on the channel — face-cam dominant, slide-deck style, numbers on screen. This is the bookend that makes the whole 6-month series feel like a complete arc.

**Channel conventions**: same as V1–V7 (cold open, lower-third on first appearance, 20s end screen, pinned comment).

**Recording window**: after M6 analytics review is complete. **Do not record this video without the real numbers in hand.** The whole credibility of the retro is honest data.

---

## V8 — "6 months of building an infra provider in public — the numbers and the lessons"

**Working title**: `6 months of building an open-source infra tool in public — the actual numbers, the honest lessons`
**Length target**: 14:00–18:00.
**Format**: face-cam dominant, slide deck on screen for numbers + charts, occasional cluster/dashboard cutaway, no live cluster footage required.
**Thumbnail text**: "6 MONTHS, REAL NUMBERS" + face + a small "📊" or simple chart icon.

### Title options

1. `6 months of building an open-source infra tool in public — the actual numbers, the honest lessons` ← SEO-friendly + curiosity hook
2. `What 6 months of shipping a niche infra OSS project actually looks like (numbers, lessons, regrets)`
3. `I marketed my open-source project intentionally for 6 months. Here's what worked.`

### Description

```
6 months ago I decided to stop hiding my open-source TrueNAS Kubernetes provider and start marketing it intentionally. This video is the honest retrospective: the actual numbers, what compounded, what fizzled, what I'd do differently if I started over.

For solo maintainers of niche infra OSS wondering whether marketing is worth their time, this is the most useful 15 minutes I can give you.

Chapters:
00:00 — What I shipped and why this retro exists
01:30 — The actual numbers (stars, subs, search, traffic)
04:30 — What worked: 5 things I'd absolutely do again
08:30 — What didn't: 5 things I'd skip or change
12:00 — What I'd do differently if starting over
14:30 — The meta-lesson: consistency vs cleverness

Links:
— Written retro (the full version with numbers): https://dev.to/cliftonz/<retro-slug>
— Provider repo: https://github.com/bearbinary/omni-infra-provider-truenas
— Hero install guide: https://dev.to/cliftonz/<hero-post-slug>

#OpenSource #Marketing #DeveloperRelations #Homelab #BuildInPublic
```

### Script

**[0:00–0:30 — COLD OPEN, face-cam]**

> Six months ago I decided to stop hiding my open-source project and start marketing it intentionally. Today I'm sharing every number — every star, every subscriber, every page view — and what I learned about marketing niche infrastructure software. If you maintain something nobody's heard of and you're wondering whether marketing is worth your time, this is the video I wish I'd had six months ago.

**[0:30–1:00 — Title card + intro tag]**

> [Title card: "6 months in public — the numbers and the lessons"]
> I'm Zac. I maintain `omni-infra-provider-truenas`, an open-source Omni provider for TrueNAS. If you've watched this channel before — thank you. If you're new — this is video eight of eight in a 6-month series. Subscribe and you can watch the rest in any order.

**[1:00–1:30 — What I shipped, slide deck]**

> [SLIDE: list of pieces shipped]
> What 6 months looked like:
> [Read each line]
> 9 long-form written posts on dev.to.
> 8 YouTube videos including this one.
> Roughly 24 LinkedIn posts.
> 16 cross-posts to Reddit, Lemmy, and the TrueNAS forum.
> 1 user case study.
> 3 podcast pitches sent.
> 4 awesome-list submissions drafted.
> [Face-cam]
> Solo maintainer. Side project. No team. No marketing budget. Roughly 8 hours a week.

**[1:30–4:30 — The numbers, slide deck with charts]**

> [SLIDE: numbers table with M1 → M6 deltas]
> [PLACEHOLDER — fill in actual numbers from M6 review before recording]
> The numbers. GitHub stars went from [X] to [Y]. That's a [Z×] increase.
> Repo forks from [X] to [Y]. Issues filed — and this is the metric I actually care about — from [X] to [Y].
> YouTube subscribers — zero at the start, [N] today. Total watch hours: [M].
> LinkedIn followers from [X] to [Y]. Dev.to followers from [X] to [Y].
> [SLIDE: weekly stars chart]
> Here's weekly star growth. The bump there is the hero post. The bump there is the comparison videos.
> [SLIDE: search console screenshot]
> And Google Search Console — top organic query, "[query]", N impressions, M clicks per week, average position [position]. Six months ago, my project didn't rank for anything.
> [Face-cam, direct]
> Are these numbers big? No. Are they real, growing, and worth the time? Yes. If you're building niche, the absolute numbers will look small forever. The trajectory is what matters.

**[4:30–8:30 — What worked, face-cam + on-screen overlays]**

> [Overlay: "What worked #1"]
> Five things I'd absolutely do again.
> One — the hero post compounded. The canonical install guide is the single most important piece of marketing I shipped. It ranks for the keyword. It cross-links into everything else. It's responsible for roughly [percentage] of all repo referrals. Write the canonical answer to the question your users type. That's the work.
> [Overlay: "What worked #2"]
> Two — comparison posts beat tutorials. The "Talos versus k3s" and "TrueNAS versus Proxmox" posts converted to repo clicks at roughly [N×] the rate of any tutorial. People reading comparisons are deeper in the funnel. They've decided to do something. They're picking which thing.
> [Overlay: "What worked #3"]
> Three — smallest community first. I posted to r/truenas before r/selfhosted, every time. Smaller audience, tighter ICP, better signal. And the comments from r/truenas helped me tune the hook before bigger subs saw it. Free QA.
> [Overlay: "What worked #4"]
> Four — YouTube is slow and worth it. Subscriber count is modest. But the face-recognition compounds elsewhere — when someone sees me in a Reddit thread and replies "oh you're the guy from YouTube," that recognition lowers the friction on every future touch. YouTube is a brand investment, not a discovery channel. Treat it that way.
> [Overlay: "What worked #5"]
> Five — telling people what success looks like. Every CTA said "issues over stars." People listened. The issue tracker has [N] substantive issues — bug reports, feature requests, hardware-specific questions. That's worth more than [N] stars by any measure I care about.

**[8:30–12:00 — What didn't, face-cam + on-screen overlays]**

> [Overlay: "What didn't #1"]
> Five things I'd skip or change.
> One — X, formerly Twitter. Lowest-ROI channel by a wide margin. The audience for niche infra has migrated off X for two years and that trend continued during this window. I'll keep cross-posting threads because the cost is near zero. I will not invest in growing X presence specifically.
> [Overlay: "What didn't #2"]
> Two — pitched the wrong podcast first. Cold-pitched the biggest show first because it was the biggest. Never replied. Should have led with Self-Hosted Show — smaller audience, exact ICP fit, way more likely to bite.
> [Overlay: "What didn't #3"]
> Three — LinkedIn audience was different than I thought. Assumed engineering leaders would find the project and bring it to their teams. The real audience was other solo developers, infra engineers, OSS maintainers. My peers. That's fine — peer engagement compounds in its own way — but I wrote early posts for the wrong audience.
> [Overlay: "What didn't #4"]
> Four — should have started looking at analytics earlier. Wired up Plausible in M1, didn't actually read the dashboard with intention until M3. The early-window data is gone. Snapshot weekly from week one.
> [Overlay: "What didn't #5"]
> Five — case study was harder to source than expected. Self-hosters are private. Took [N] outreach attempts to get one yes. Should have started outreach in M3, not M5. Build user relationships before you need the artifact.

**[12:00–14:30 — What I'd do differently from scratch, face-cam + on-screen overlay]**

> [Overlay: "From-scratch playbook"]
> If I started over from M0:
> Write the hero post before any marketing cadence starts. It's load-bearing. Don't release the project without it.
> Start YouTube in M0 too. Even one channel-intro video. Face-recognition compounds for months before "real" sub counts show up.
> Pick three channels and ignore the rest. Mine are dev.to canonical, YouTube, Reddit. LinkedIn was a fourth that earned its slot. X was a fifth that didn't.
> Build a tracking sheet on day one. Update it weekly. Don't over-engineer.
> Treat issues filed as the primary metric, not stars. Tell your audience this explicitly.
> Pre-write the first 3 months of content before publishing anything. The cognitive load of publish-plus-write-plus-reply gets hard around M2. Front-loading saves you.

**[14:30–16:00 — The meta-lesson, face-cam]**

> [Face-cam, direct]
> Here's what I actually believe after six months.
> Niche infra OSS doesn't need a marketing team. It needs five things.
> One canonical answer to the question your users type into Google. A face people can recognize. Honest, well-named tradeoffs against the alternatives. A clear story about what success looks like — issues over stars. And enough consistency that six months from now there's still something new.
> Most of what I did wasn't clever. It was just consistent.
> [Brief pause]
> Six months of "good enough" beats one viral post almost every time.

**[16:00–16:45 — Forward-look + CTA, face-cam]**

> What's next for me — three bets. Deeper SEO on the hub page. Two more case studies. And one focused experiment with paid distribution to see whether the project's cost per repo click pencils out.
> Channel keeps going. Same monthly cadence. Same focus on the parts of self-hosted Kubernetes nobody else writes about. Subscribe if you haven't.
> [Face-cam, direct, warm]
> If you maintain something niche and you've been telling yourself "I should market this someday" — today is fine. Start with the canonical post. The rest builds on it.
> Thanks for watching this far. Genuinely.

**[16:40–17:00 — End screen: V1 channel intro + hub page card]**

### Production notes
- **Real numbers only.** Do not record this video before the M6 analytics review is done. The credibility of every other moment in the video depends on the data being accurate.
- **Slide deck on screen, face-cam in corner or PIP.** This is the only video in the series with this format. Use it deliberately — it signals "this one is different, this is the retro."
- **Don't apologize for the small numbers.** Niche tool, niche audience, small absolute numbers are expected. The trajectory is what matters and the trajectory is real.
- **The meta-lesson beat at 14:30 is the most-clipped moment.** Pace it. Pause after "Six months of good enough beats one viral post almost every time." Give it room.
- This video is also the most-likely candidate for a YouTube Shorts spinoff. Pre-edit a 60-second "5 things I'd do again" cut from the 4:30–8:30 segment and drop it as a Short the same day.

---

## Cross-promotion plumbing (M6 specific)

After V8 ships:

- **Written retro post**: replace the "[Video version](#)" placeholder with V8 URL.
- **Hub page** (`06-month-6/01-hub-page.md`): under YouTube channel section, V8 = video 8 of 8.
- **LinkedIn week 1 of M7**: "I just published the 6-month retro on YouTube — here are the three lessons that surprised me most." Native long post, V8 link in first comment.
- **Hacker News submission**: the retro is the M6 piece most likely to land on HN. Submit V8 *and* the written retro on the same morning, but at different times — submit written ~9am ET, video ~3pm ET. Stagger lets either pick up on its own merits.

---

## Open placeholders to fill before record

- **All bracketed [X], [Y], [N], [percentage] numbers** must be real values from M6 analytics. Pull them from the tracking sheet (`_shared/analytics-setup.md`).
- Slide deck: build it after the numbers are finalized. Keep slides minimal — number + one-line label. Don't bury the data in design.
- Confirm the "what didn't" section is what *actually* didn't work, not what I'm guessing in advance. The honesty is the differentiator.
- The "forward-look" three bets at 16:00 are placeholders — replace with whatever you've actually decided to invest in for M7+.
