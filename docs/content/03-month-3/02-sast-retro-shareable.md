---
title: "A SAST sweep on a 5kLOC Go infra provider: 6 findings, 6 lessons"
published: false
description: "I ran a static analysis sweep on my Go-based Omni provider. Here's what the findings were, what was real, and what I'd do differently next time."
tags: golang, security, sast, kubernetes
cover_image: ""
series: "Build-in-public: omni-infra-provider-truenas"
---

**TL;DR — I ran a SAST (static application security testing) sweep on `omni-infra-provider-truenas`, a ~5kLOC Go project I maintain. Six findings, six lessons. None of them were CVE-class. All of them taught me something. This post is the honest retro — what tooling caught, what it missed, and what I'd build differently next time.**

I'm Zac Clifton. I daily-drive this stack and I ship it under MIT to a community catalog. That means strangers install my code, run it as root-equivalent on their NAS, and trust it not to do anything stupid. SAST sweeps aren't optional at that point — they're table stakes.

For the project itself: [github.com/bearbinary/omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas). For the origin story (why this provider exists): [TrueNAS killed Kubernetes — so I brought it back](https://dev.to/cliftonz/truenas-killed-kubernetes-so-i-brought-it-back-4n7h).

---

## The setup

The project: Go 1.26+, ~5,000 LOC across the provider proper plus internal packages (`internal/client`, `internal/provisioner`, `internal/singleton`, `internal/resources`). Talks to TrueNAS over JSON-RPC 2.0 via WebSocket. Talks to Omni via the standard SDK. Runs as a container, usually on the TrueNAS host itself.

Surface area worth auditing:

- **Outbound HTTP**: ISO downloads from Sidero's Image Factory.
- **WebSocket transport**: JSON-RPC to TrueNAS, with API key auth.
- **Multipart upload**: streaming ISO bytes to TrueNAS's `/_upload` endpoint.
- **Secret handling**: TrueNAS API key, Omni service account key — both flow through env vars and into request headers.
- **External-command-like behavior**: the provider doesn't shell out, but it does ask TrueNAS to execute things (VM lifecycle, dataset creation).

The sweep used a combination of static analyzers plus targeted manual review focused on the surfaces above. I'm not going to name the specific tooling here because the lesson isn't "tool X is good" — it's "what the findings actually were."

---

## The six findings, ranked by what I learned

### Finding 1: Sensitive value logged in DEBUG path

**What it was**: A debug log line in the WebSocket transport printed the raw outbound JSON-RPC message — which, on auth methods, included the API key. Only in `LOG_LEVEL=debug`. Almost certainly never enabled in production.

**Why it was real**: Almost-certainly-never isn't never. The first time someone files a support issue and pastes their debug log into a GitHub issue, the key leaks publicly.

**The fix**: Redact known auth fields at the marshaling layer, not at the log call site. Log call sites are too easy to forget. Marshaling-layer redaction means *every* debug log path is safe.

**Lesson**: "But only in debug mode" is not a security argument. If the data is sensitive, it's sensitive in every log level.

---

### Finding 2: HTTP client without explicit timeout

**What it was**: One of the HTTP code paths used `http.DefaultClient` instead of a configured client. `http.DefaultClient` has no timeout. A misbehaving upstream could hang the goroutine forever.

**Why it was real**: This is a denial-of-self attack, not a denial-of-service-to-others. But the provider runs as a long-lived process — goroutine leaks accumulate, RAM grows, eventually the container OOMs. I'd seen mysterious slow leaks in long-running test cassettes and never connected them to this.

**The fix**: Replace every `http.DefaultClient` with a shared configured client at package init. Connect timeout, request timeout, and idle connection timeout all set explicitly. Also added `http.Transport.MaxIdleConnsPerHost` while I was in there.

**Lesson**: `http.DefaultClient` exists for one-off scripts and Go playground snippets. Production code should never see it. There's a `revive` / `golangci-lint` rule for this — I should have caught it earlier.

---

### Finding 3: Insecure-by-default TLS path

**What it was**: An older code path supported `TRUENAS_INSECURE_SKIP_VERIFY=true`. Useful for localhost where TrueNAS uses a self-signed cert. The problem: the flag was being read as a generic "insecure mode" everywhere it landed, not just for TrueNAS. A future code path adding a different HTTPS dependency would inherit the skip-verify by accident.

**Why it was real**: Composition risk. The bug was that the global insecure flag could be silently broadened.

**The fix**: Plumbed an explicit per-TLS-config insecure boolean instead of a global. Added a docs note explicitly saying "this flag is for localhost TrueNAS only — don't repurpose it."

**Lesson**: Insecure flags must be narrowly scoped to the surface they're for. Naming them generically invites future misuse.

---

### Finding 4: Path-traversal-shaped surface in ISO cache

**What it was**: The ISO cache logic took a Talos image identifier and constructed a filesystem path on the TrueNAS host. The identifier came from upstream config (a `MachineRequest`), which is technically attacker-controlled if Omni itself were compromised. A crafted identifier with `../` could in theory traverse out of the cache directory.

**Why it was *not* the worst thing*ever**: The cache lives inside a ZFS dataset the provider owns, and the writes go through the TrueNAS API (which has its own path normalization). I traced the actual exploit and the impact ceiling was "spam files into a sibling dataset I also own." Not catastrophic. Still bad.

**The fix**: Validate the identifier against a strict allowlist regex *before* it ever touches a path construction. Reject anything that doesn't match. Belt-and-suspenders even though the path doesn't currently exit our control plane.

**Lesson**: "The attacker would have to compromise Omni first" is not a fix. Defense in depth means assuming each layer can fail. Validate at every trust boundary.

---

### Finding 5: Unbounded retry loop on a transient error

**What it was**: When the provider hit a 5xx from the TrueNAS API, the retry loop would back off and retry — without a max retry count. In theory, an attacker could cause TrueNAS to return 5xx indefinitely (e.g., by overloading it) and the provider would loop forever, consuming CPU and never propagating the error to Omni.

**Why it was real**: Same denial-of-self category as the timeout finding. In practice, the provider would burn CPU and stop making progress on real provision work. Users would see "stuck" `MachineRequest`s and have no signal that the provider was the problem.

**The fix**: Bounded exponential backoff with a max retry count (default 5, env-overridable). After max retries, the error propagates to Omni so the failure is visible in the UI instead of silent.

**Lesson**: Retry loops without bounds are a control-flow security issue, not just a robustness one. They prevent legitimate failure signals from surfacing.

---

### Finding 6: Sensitive log redaction in error paths

**What it was**: Several error wrappers concatenated request context (including URLs) into error messages. The URLs sometimes contained query-string auth tokens (this surface has since been removed, but it existed at one point). Errors propagate freely through logs and through the SDK back to Omni.

**Why it was real**: Same family as finding 1. Errors are logged everywhere. If sensitive data leaks into an error, it leaks into every log on the path.

**The fix**: Centralized error-wrapping helper that strips auth from URLs (query string, basic auth in URL, fragment). Used the helper everywhere instead of bare `fmt.Errorf("%w: %s", err, req.URL.String())`.

**Lesson**: Error messages are part of the security surface. Treat them like you'd treat structured logs.

---

## What the sweep did *not* catch

Worth naming honestly: SAST is a floor, not a ceiling. Things SAST couldn't have caught that I had to think about separately:

- **Authorization semantics**: Does the provider correctly scope what it asks TrueNAS to do? SAST can't see "this code path correctly only creates VMs in the user's namespace." That's a manual-review concern.
- **Cryptographic protocol misuse**: The provider doesn't implement custom crypto, so this wasn't relevant. But SAST tools don't know that — they'll flag anything that looks like crypto regardless.
- **Logic bugs that are also security bugs**: The recent `min_memory <= memory` schema fix in v0.16.1 was a host-OOM safety bug. SAST didn't catch it. A type signature checker can't see that "minimum memory" and "maximum memory" have a semantic relationship.
- **Supply chain**: Dependencies, build provenance, image signing. SAST runs on your source; supply chain risk lives upstream. Different problem, different tools.

If you treat SAST as a one-and-done, you'll feel safer than you are. Treat it as a code review with infinite patience and a narrow lens.

---

## What I'd do differently next time

1. **Run SAST on every PR, not just on a sweep.** The findings above could have been caught at write-time instead of months later. Integrate it into CI.
2. **Add linting rules for the patterns SAST found.** `http.DefaultClient` shouldn't reach review. `fmt.Errorf` with raw URLs shouldn't either. Lint catches these earlier than SAST.
3. **Write the threat model first.** I went into the sweep without a written threat model and ranked findings emotionally. With a model, I'd have ranked them against actual attacker capability.
4. **Pair SAST with fuzz testing on parsers.** The provider parses JSON-RPC responses from TrueNAS. SAST can't tell you what happens when TrueNAS returns garbage; a fuzzer can.
5. **Document the redaction conventions.** Future-me, or future contributors, won't know which fields are sensitive unless I tell them. A `SECURITY.md` section on redaction rules is now on the list.

---

## What this is, and what this isn't

This isn't a "I made my code secure" post. Code is never secure — it's only currently-not-failing in the ways you've thought to check.

This *is* the kind of work I think OSS infra maintainers should do publicly. Six findings on 5kLOC is a *normal* result for a project that hadn't been swept before. The project is healthier for it. The next sweep will find something else, and that's the point.

If you're maintaining a small Go project that handles secrets or talks to APIs that affect production systems — and you haven't run SAST on it — that's the next thing you should do this week.

---

## Try the provider

- **Repo**: [github.com/bearbinary/omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas)
- **Install guide**: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>)
- **Origin story**: [TrueNAS killed Kubernetes — so I brought it back](https://dev.to/cliftonz/truenas-killed-kubernetes-so-i-brought-it-back-4n7h)

Disagree with any of these calls? Find a different category of finding I should have run? File an issue on the repo or find me on [LinkedIn](#).

---

**About the author**: Zac Clifton is an infrastructure engineer building tools for self-hosters and small teams. He maintains `omni-infra-provider-truenas` and writes about pragmatic homelab Kubernetes and the unglamorous parts of shipping infra OSS. Subscribe on [YouTube](#) for monthly deep-dives.
