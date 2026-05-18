# Case Study — Template + Outreach Process (M5)

Real social proof matters more in M5 than any post you write yourself. One user case study is worth three of your own opinion pieces. This file is the framework — you fill it in once you've recruited a real user.

---

## Who to recruit

Look in (in this order):

1. **GitHub issues on the repo** — find someone who filed a non-trivial issue, got a fix, and reported back positively. They've already shown investment.
2. **TrueNAS forum announcement thread** (from M1) — anyone who replied with "I'm running this in my homelab" is a candidate.
3. **Reddit comments on hero post / V2 video** — same thing, lower investment but easier to reach.
4. **Lemmy threads from M1/M2 cross-posts** — same.
5. **Sidero Discord / Talos Slack** — DM polite asks to anyone who's mentioned your provider organically.

**What good looks like**:

- Running it for ≥ 2 months (long enough to have real opinions)
- Multi-node, not single-node (richer story)
- Comfortable being named publicly + sharing screenshots (not everyone is)
- Has a story angle — switched from k3s, GPU passthrough adventure, weird hardware, etc.

**What you skip**:

- Anyone who's just installed it and is enthusiastic but hasn't actually run anything on it
- Anyone who insists on anonymity (still valuable for product feedback, weaker case study material)

---

## Outreach template

```
Subject: Quick ask — your TrueNAS + Talos setup

Hi [name],

I'm Zac, the maintainer of omni-infra-provider-truenas. Saw your [issue / forum post / Reddit comment / Discord message] about your cluster setup and wanted to ask — would you be open to being featured in a case-study post on dev.to?

Format would be light: a 30-minute conversation (async over email or live, whatever's easier), I write up the post draft, you review and approve before I publish. You get attribution (or stay anonymous, your call). Plenty of "credit where it's due" framing — the project gets a lot of issues from users like you who are running it harder than I ever planned for.

The post would cover roughly:
- Your hardware + cluster shape
- Why you picked this path (vs Proxmox / k3s / TrueNAS apps)
- What worked, what didn't
- What you'd tell someone considering the same setup

If you're open to it, just reply with a few rough notes on your setup and we can take it from there. If you'd rather not, no worries at all — your feedback through the issue tracker is already valuable.

Thanks either way.

Zac
```

**Honest disclosure** in the email: this is marketing material. Don't dress it up as anything else. Self-hosters appreciate the honesty.

---

## Interview structure (~30 minutes)

If they say yes, send these questions. Async over email works fine for most people.

### Section 1: The setup

1. What's the hardware? (NAS model, CPU, RAM, disks, network)
2. What's the cluster shape? (CP replicas, worker count, what each runs)
3. What pool layout? ZFS vdev topology, SLOG?
4. What CNI? What storage class?
5. What workloads do you run on it?

### Section 2: The path here

6. What were you running before this?
7. Why did you switch? Or: why did you pick this over the alternatives?
8. How long did setup take?
9. What's your overall happiness with it on a 1–10? Why?

### Section 3: Pain points

10. What surprised you (good or bad)?
11. What broke? How did you fix it?
12. Anything you wish you'd known before starting?
13. Any features you wish the provider had?

### Section 4: Advice

14. What would you tell someone considering the same setup?
15. What hardware would you tell them to budget for?
16. Any "don't make this mistake" stories?

### Section 5: Permissions

17. OK to use your real name? Or pseudonym?
18. OK to share a screenshot of your Omni cluster page (with sensitive info redacted)?
19. OK to link to your blog/Twitter/LinkedIn if you have one?
20. OK to publish on a specific date, or any flex?

---

## Post template — fill this in

```markdown
---
title: "How [Name/Pseudonym] runs Kubernetes on a single TrueNAS box"
published: false
description: "[One-line specific detail — e.g., '12-core TrueNAS Mini X+ running a 5-node Talos cluster with Longhorn and Velero, in [their location]']"
tags: kubernetes, truenas, talos, homelab
cover_image: ""
series: "Self-hosted Kubernetes on TrueNAS"
---

**TL;DR — [One paragraph: who, what hardware, why they picked this path, what they're running, and the most interesting honest opinion.]**

I (Zac Clifton) maintain `omni-infra-provider-truenas`. This is a case study of one of the people running it — [name], a [their day job / homelab context] in [their location]. They've been running [their cluster shape] for [duration] and agreed to write up what it looks like in practice.

For the install guide if you want to follow along: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>).

---

## The setup

[Their hardware. Specific model numbers. ZFS pool topology. Network shape. Be precise — readers will copy this.]

[Mermaid diagram of their topology if it's interesting.]

---

## Why this path

[The story of how they got here. What they were running before. What broke or didn't fit. What pulled them toward Talos + Omni + TrueNAS-only.]

> [Pull quote from the interview — usually their "the moment I knew" line.]

---

## What they're running

[Workload list. Numbers if possible — pods, namespaces, PVC count, what's on what hardware.]

| Workload | Why it's there |
|---|---|
| [Workload 1] | [Reason] |
| [Workload 2] | [Reason] |
| ... | |

---

## What worked

[3–5 specific wins. Quoted directly from the interview.]

---

## What didn't (and how they fixed it)

[3–5 specific pain points. Be honest. This is the section that makes the case study credible.]

> [Pull quote — usually their "I wasted a weekend on X" line. This is the most-shared paragraph in any case study, so make it good.]

---

## Their advice for someone starting today

[3–5 bullets. Their voice, not yours.]

1. [Advice 1]
2. [Advice 2]
3. [Advice 3]

---

## What I learned from this conversation

[Your reflection. What did you learn about how your project gets used? What features do they want that you hadn't considered? What documentation gaps did this surface?]

This is part of the value of running an open-source infra tool in public: users teach you what your tool actually is, vs. what you thought you were building.

---

## Try the path they took

- **Provider repo**: [github.com/bearbinary/omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas)
- **Hero install guide**: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>)
- **[Name]'s blog/Twitter** (if applicable): [link]

If you're running this in your homelab and would like to be the next case study — or if reading this gave you ideas you want to share — file an issue on the repo or find me on [LinkedIn](#).

---

**About the author**: Zac Clifton is an infrastructure engineer building tools for self-hosters and small teams. He maintains `omni-infra-provider-truenas` and writes about pragmatic homelab Kubernetes. Subscribe on [YouTube](#) for monthly deep-dives.
```

---

## Distribution for the case study

Different from your own posts — the user's story is the hook, the project is incidental.

| Channel | Adjustment |
|---|---|
| **dev.to (canonical)** | Standard publish. Tag the user if they're on dev.to. |
| **LinkedIn** | Native post quoting their best line. Tag them. They reshare = 5–10× reach. |
| **Reddit r/selfhosted** | "Featured a community member's TrueNAS cluster on dev.to — interesting setup" — frame as community spotlight |
| **TrueNAS forum** | Post in their announcement thread (from M1) — "here's someone running this in production" |
| **X / Lemmy** | Similar community-spotlight framing |

**Critical**: the user gets to share the post first if they want. Send them a draft link 24h before public. Their network is the first wave of reach.

---

## Risk: nobody says yes

If no users agree to be featured (possible — self-hosters are private), pivot to a self-interview format:

> "What I'd answer if a hypothetical user gave this interview about my own homelab"

Still useful content. Less authority signal. Better than skipping M5's social-proof slot entirely.

If even that feels weak, swap M5's case study for an additional comparison post (the "TrueNAS-only vs PVE provider" matchup is still missing) and slot the case study into M7+ when more users have organically surfaced.
