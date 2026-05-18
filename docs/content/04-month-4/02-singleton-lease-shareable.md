---
title: "The singleton-lease pattern: leader election when your SDK doesn't have one"
published: false
description: "How I built a distributed lease for an Omni infrastructure provider with no SDK-level leader election. ~200 lines of Go, optimistic concurrency, fencing tokens, and the upstream bug I had to route around."
tags: golang, distributed, kubernetes, leaderelection
cover_image: ""
series: "Build-in-public: omni-infra-provider-truenas"
---

**TL;DR — The Omni SDK doesn't ship leader election for infrastructure providers. Run two processes with the same `PROVIDER_ID` and they both pick up every `MachineRequest`, racing on VM creation and zvol allocation. I built a distributed lease in ~200 lines of Go on top of Omni's optimistic-concurrency state, using annotations on the `ProviderStatus` resource as the lease object. This post walks through the design, the upstream bug I had to route around, and the parts I'd build differently next time.**

I'm Zac Clifton. I maintain [`omni-infra-provider-truenas`](https://github.com/bearbinary/omni-infra-provider-truenas) — an open-source Omni infrastructure provider that runs Talos VMs on TrueNAS SCALE. The singleton lease is the chunk of code I'm proudest of in the whole project, partly because I had to build it and partly because building it taught me what distributed systems primitives are really *for*.

For project context: [TrueNAS killed Kubernetes — so I brought it back](https://dev.to/cliftonz/truenas-killed-kubernetes-so-i-brought-it-back-4n7h).

---

## The problem

The Omni SDK's `ProvisionController` is the framework for writing an Omni infrastructure provider. You give it your provider ID and a set of `provision.Step`s; it watches Omni for `MachineRequest` resources and runs your steps to fulfill each one.

There is no leader election.

If two processes register with the same `provider ID`, both receive every `MachineRequest` event. Both attempt to create the same VM. Both try to claim the same zvol name. One wins, the other gets a confusing error, and your state machine is now wedged in a way that's very hard to debug after the fact.

This is fine for a one-machine homelab where you run exactly one provider. It is not fine for:

- Production deployments that survive a config push (the new pod starts before the old one terminates)
- Any kind of HA setup
- The disaster recovery flow where you're not sure if the old provider is really dead

The only mitigation in the SDK is "make sure you only ever run one." That's a runbook entry, not a guarantee.

---

## The design constraints

I had a narrow set of working materials:

- **No additional infrastructure.** The provider is a single Go binary in a container. Adding etcd, Redis, or Consul "just for leader election" was a non-starter — the whole point of the provider is to *be* the infrastructure layer.
- **Omni's COSI resource store is the only state I can rely on.** It's already there, it's already authenticated, it has optimistic-concurrency semantics, and it's where my provider already writes status.
- **Failover should be fast but safe.** A crashed pod should release the lease quickly. A network partition should not let two providers think they're both the leader.

The constraint "no extra infra" was the design forcing function. Once I committed to that, the shape was obvious: use a COSI resource as the lease object. Use its optimistic-concurrency `version` field as the compare-and-swap primitive. Heartbeat by updating an annotation.

---

## The shape of the lease

Every Omni infrastructure provider already has a resource named `infra.ProviderStatus` — Omni reads it to know the provider is alive and what it's doing. The provider already owns that resource. I added three annotations to it:

```go
const (
    AnnotationInstanceID = "bearbinary.com/singleton-instance-id"
    AnnotationHeartbeat  = "bearbinary.com/singleton-heartbeat"
    AnnotationEpoch      = "bearbinary.com/singleton-epoch"
)
```

- **`instance-id`** — a UUID generated at process start. Identifies *this* running instance, not the provider ID.
- **`heartbeat`** — an RFC3339 timestamp updated on a tick.
- **`epoch`** — a monotonically-increasing counter that bumps on every takeover. Used as a fencing token (more on this below).

Acquiring the lease is a write to the `ProviderStatus` resource that sets all three annotations to my instance's values. The optimistic-concurrency check happens at the COSI layer: if another process has updated the resource since I last read it, my write fails with a version conflict and I retry.

```go
// Conceptually:
//   1. Read current ProviderStatus
//   2. Check if existing heartbeat is fresh (< staleAfter)
//      - if yes and not my instance-id: ErrLeaseHeld
//   3. Write new annotations with my instance-id, heartbeat=now, epoch+1
//   4. If write fails with version conflict: someone else got there first, retry
```

Tuning values that worked for my workload:

```go
const (
    DefaultRefreshInterval = 15 * time.Second  // how often I bump heartbeat
    DefaultStaleAfter      = 45 * time.Second  // when an old heartbeat counts as "dead"
)
```

`staleAfter = 3 × refreshInterval` gives me two missed heartbeats of grace before another instance steals the lease. Three missed heartbeats is the timeout. This is conservative — production systems run tighter — but provider failover within a minute is fine for my use case.

---

## The control flow

The provider's `main` blocks on `lease.Acquire()` at startup. If the lease is held by a fresh heartbeat from a *different* instance, acquire returns `ErrLeaseHeld` and the provider exits with a clear error. This is the fail-fast path: if you accidentally run two providers, the second one tells you exactly why it's refusing to start.

If the lease is unheld, or held by a *stale* heartbeat, this instance takes over. Takeover bumps the epoch. The previous holder, if it's still alive (network partition recovery, say), sees the epoch jump on its next refresh and shuts itself down.

Once the lease is held, a goroutine refreshes it every `refreshInterval`. Three consecutive refresh failures (the lease was stolen, the network broke, Omni went down) trigger a loss signal back to the main loop, and the provider shuts down gracefully — its provision controller is no longer authoritative.

```go
const maxConsecutiveRefreshErrors = 3
```

The number 3 isn't arbitrary. Two consecutive failures could just be transient. Four would let a takeover happen invisibly — by the time you noticed, you'd already be split-brained. Three is the smallest number that absorbs ordinary network noise without masking a real takeover.

---

## The fencing token (epoch)

The epoch annotation is what makes this safe under network partitions.

Imagine: instance A holds the lease. The network between A and Omni breaks, but A is still running and still trying to provision VMs. Meanwhile, B sees A's heartbeat go stale, takes over, bumps the epoch from 7 to 8.

If A's network comes back, A's next heartbeat refresh will see the epoch is now 8. A knows it's been preempted and exits.

**But what if A had a provision call in flight during the partition?** That call would land at TrueNAS *after* B has taken over. Without a fencing token, A's stale write could clobber state B has already updated.

This is the classic Chubby / lock-service problem. The fix is to tag every state-mutating operation with the epoch you held when you started it. The receiving service (TrueNAS, in my case) checks the epoch and rejects writes from a stale one.

**I haven't fully wired this up yet.** TrueNAS's API doesn't accept caller-supplied epochs (it has no concept of my lease), so the fencing has to happen client-side: the provider checks its own epoch before every write. Currently the epoch is annotation-only and observability-only — it shows up in logs, but I haven't done the work to gate every TrueNAS call on it.

This is the part I'd build differently next time. **Wire the fencing token in from day one.** Adding it later means auditing every state-mutating code path and threading the epoch through, which is exactly the kind of refactor that creates bugs.

---

## The upstream bug I had to route around

Real-world distributed systems have at least one "the framework lies to you in a subtle way" issue. Mine is upstream Omni bug [siderolabs/omni#2642](https://github.com/siderolabs/omni/issues/2642): the gRPC-gateway returns a `200 OK` response with no `Content-Type` header on certain successful state writes. The gRPC client interprets the missing header as a failed response and surfaces an error to my code — even though the write succeeded server-side.

The first time this happened, I logged "lease refresh failed" on a write that had actually succeeded. The next refresh worked fine because the server state was correct, but my error counter incremented for no reason. Three of those in a row would falsely trigger lease loss and I'd shut down a perfectly-healthy provider.

The workaround:

```go
const malformedContentTypeMarker = "malformed header: missing HTTP content-type"

func isMalformed200(err error) bool {
    if err == nil { return false }
    msg := err.Error()
    return strings.Contains(msg, malformedContentTypeMarker) &&
           strings.Contains(msg, "200")
}
```

If the error matches this shape, treat the operation as successful (the server wrote, the client just misparsed the response). Substring-matching on error strings is one of those things you'd hate in any other context — but tracking an upstream bug by error shape is the only available option when the bug doesn't surface a typed error.

It's documented in a `// matches a known Omni gRPC-gateway bug` comment in the source so future-me doesn't try to clean it up.

---

## What I'd build differently

Three things, in priority order:

1. **Wire the fencing token through every state-mutating call from day one.** The epoch exists. It logs. It is not actually preventing split-brain writes. That's a bug I haven't filed against myself yet because nobody has hit it — but it's there.

2. **Use a separate lease resource, not annotations on ProviderStatus.** Annotations were convenient because the resource already exists. The cost: every heartbeat is a write to a resource whose other contents (provider health, capabilities, last-seen) are also Omni-readable, so my heartbeat changes show up in every status diff that consumers might watch. A dedicated `Lease` resource would have cleaner semantics, at the cost of an extra resource type to register.

3. **Test partition behavior with real network faults.** I tested with mocked errors. I have not tested with `iptables -j DROP`. The two are different in ways that matter — clock-skew handling especially. This is the test I keep meaning to write.

---

## Why this pattern matters beyond my project

Three lessons that generalize:

**Lesson 1**: When your SDK doesn't ship a primitive you need, build it from the primitives you have. Optimistic concurrency on existing state is leader election if you squint right.

**Lesson 2**: Fencing tokens are not optional for safe leader election. If your "lease" doesn't tag outbound writes with a token the receiver can validate, you have a leader-*detection* mechanism, not a leader-*election* one. The difference matters under partition.

**Lesson 3**: Fail loud, fail fast, fail clearly. The biggest UX win from this work wasn't preventing split-brain — it was making the second provider's error message actually say "another instance holds the lease, here's its instance ID, here's its last heartbeat, exiting." Operators love that. Operators do not love silent races followed by mysterious data corruption.

---

## Read the source

The whole package is ~200 lines:

- [`internal/singleton/singleton.go`](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/internal/singleton/singleton.go)
- [`internal/singleton/singleton_test.go`](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/internal/singleton/singleton_test.go)
- [`internal/singleton/epoch_test.go`](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/internal/singleton/epoch_test.go)
- [`internal/singleton/malformed_200_test.go`](https://github.com/bearbinary/omni-infra-provider-truenas/blob/main/internal/singleton/malformed_200_test.go)

MIT licensed. Read it, lift it, send a PR-equivalent issue back if you spot the bug I haven't.

---

## Try the project

- **Provider repo**: [github.com/bearbinary/omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas)
- **Hero install guide**: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>)
- **Origin story**: [TrueNAS killed Kubernetes — so I brought it back](https://dev.to/cliftonz/truenas-killed-kubernetes-so-i-brought-it-back-4n7h)

If you've built leader election on top of an unusual primitive, I want to hear about it. Find me on [LinkedIn](#) or open an issue.

---

**About the author**: Zac Clifton is an infrastructure engineer building tools for self-hosters and small teams. He maintains `omni-infra-provider-truenas` and writes about the unglamorous parts of shipping infrastructure OSS. Subscribe on [YouTube](#) for monthly deep-dives.
