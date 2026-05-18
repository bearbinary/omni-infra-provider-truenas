# Awesome-List Submissions (M4 distribution)

**Hard rule** (per `public-pr-guard`): never auto-submit to third-party catalogs. Every submission below is a **draft for Zac to hand to the maintainer**, not a PR Claude opens. Research the list's contribution policy *first*. If the list doesn't accept unsolicited additions, drop it.

---

## Target lists (research before submitting)

### Tier 1 — directly relevant, well-maintained

| List | Repo | Why fit |
|---|---|---|
| **Awesome Talos** | search `siderolabs/awesome-talos` or community equivalents | Provider is directly a Talos-ecosystem tool |
| **Awesome Sidero Omni** | likely doesn't exist as a standalone list; check Sidero docs for community resources page | Same reasoning |
| **Awesome Self-Hosted** | `awesome-selfhosted/awesome-selfhosted` | Self-hosted Kubernetes is in scope |
| **Awesome Kubernetes** | `ramitsurana/awesome-kubernetes` | Broadest reach; competitive |

### Tier 2 — adjacent

| List | Repo | Why fit |
|---|---|---|
| **Awesome TrueNAS** | community-maintained, smaller | Direct TrueNAS audience |
| **Awesome Go** | `avelino/awesome-go` | Project is Go; could land under "Infrastructure" or "Kubernetes" subsection |
| **Awesome Homelab** | `awesome-foss/awesome-sysadmin-homelab` or `morpheus65535/awesome-self-hosted` style lists | Homelab audience |

### Skip

- Generic "awesome" lists with no clear scope (e.g., `awesome-awesomeness`) — low signal, often auto-rejecting.
- Lists that haven't merged a PR in 12+ months.

---

## Pre-submission research checklist (run for each list)

Before drafting anything for a specific list, verify:

1. **Is the list active?** Last commit < 90 days. Open PRs being responded to.
2. **What's the contribution policy?** Almost every awesome list has a `CONTRIBUTING.md` and a PR template. Read both.
3. **What's the format?** Most use `- [Name](link) - One-line description.` Some require additional fields (license, language, stars). Match exactly.
4. **What section does this fit in?** Browse the existing list. If there's no obvious section, the list may not be the right fit.
5. **Are there gatekeeping rules?** Some lists require N stars, N contributors, or 6+ months of activity. Check before drafting.
6. **Is auto-submission discouraged?** Some lists explicitly say "no PRs from project authors." If so, do not submit. Flag for organic discovery instead.

If any of those checks fail, **don't submit to that list**. Note it in this file's "deferred" section below.

---

## Draft entries (format-agnostic — adjust to each list's format)

Pick the description that fits the list's tone. Long for self-hosted/homelab audiences, short for Awesome-K8s where conciseness is enforced.

### Short form (≤120 chars)

```
- [omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas) - Omni infrastructure provider for TrueNAS SCALE — runs Talos Linux VMs as Kubernetes nodes. MIT.
```

### Medium form (≤200 chars)

```
- [omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas) - Open-source Omni infrastructure provider that creates Talos Linux VMs on TrueNAS SCALE 25.04+ for managed Kubernetes clusters. WebSocket JSON-RPC, ZFS-backed zvols, immutable nodes. MIT.
```

### Long form (≤400 chars, only where allowed)

```
- [omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas) - Open-source Omni infrastructure provider for TrueNAS SCALE. Turns a TrueNAS host into a fleet of Talos Linux VMs that Sidero Omni manages as a real multi-node Kubernetes cluster — no separate hypervisor, ZFS-backed VM disks, optional Longhorn data disks, MIT licensed, cassette-based integration tests, listed on the TrueNAS apps community catalog.
```

### Tags / keywords (where supported)

`kubernetes` `truenas` `talos` `omni` `homelab` `self-hosted` `golang` `infrastructure-provider`

---

## Hand-off process

For each list that passes the research checklist:

1. **Fork the list's repo.**
2. **Branch**: `add-omni-infra-provider-truenas`.
3. **Edit**: insert the appropriate-form entry in the correct section, alphabetically (most awesome lists enforce alphabetical order).
4. **Run** any pre-commit hooks (`awesome-lint` is common — usually `npx awesome-lint`).
5. **Commit message**: `Add omni-infra-provider-truenas` — match the list's existing commit message style.
6. **PR description**: short. State what the project is, why it fits the list's scope, link to the canonical install guide on dev.to. Do not over-pitch — awesome-list maintainers don't read pitches, they check the entry shape.

**Sample PR description template**:

```
Adding omni-infra-provider-truenas — an open-source Omni infrastructure provider for TrueNAS SCALE.

It turns a TrueNAS box into a fleet of Talos Linux VMs managed by Sidero Omni — letting users run a real multi-node Kubernetes cluster on hardware they already own.

- MIT licensed
- Active maintenance (current version: v0.16.1, regular releases)
- Listed on the TrueNAS apps community catalog
- 42 cassette-based integration tests in CI

I'm the maintainer. Happy to adjust the entry's wording, section, or format to match this list's conventions — just let me know.
```

---

## Deferred / blocked (track here)

When a list fails the research checklist, log it here so we don't reconsider it prematurely.

| List | Why deferred | Revisit when |
|---|---|---|
| <example> Awesome-X | No PRs merged in 18 months | Q4 2026 |
| | | |

---

## Cadence

- **M4 week 2**: research the 4 tier-1 lists. Note which pass the checklist.
- **M4 week 3**: submit to **one** list that passed. Don't bundle — if the first submission is rejected for a fixable reason, you want the lesson before you've burned other lists with the same error.
- **M4 week 4**: based on outcome, submit to one more. Or address feedback on the first.
- **M5+**: revisit deferred lists if they become active. Don't chase.

---

## What success looks like

Realistic outcomes:

- **Best case** (~30% odds): one acceptance, ~5–20 unique repo visitors from the list per week, persistent.
- **Median case**: PR sits open for 6–8 weeks, eventually merged with minor wording changes.
- **Worst case**: rejection or silent close. No problem. Awesome-list maintainers are volunteers under heavy submission load.

Don't optimize the marketing plan around awesome-list acceptance. Treat it as a low-effort long-tail bet.
