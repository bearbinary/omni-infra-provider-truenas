---
title: "My TrueNAS host OOMed three times before I tracked down a memory-config bug"
published: false
description: "How a quiet schema-validation gap caused my homelab host to reboot overnight, and what it taught me about fail-loud-at-the-boundary."
tags: kubernetes, truenas, golang, infrastructure
cover_image: ""
series: "Build-in-public: omni-infra-provider-truenas"
---

**TL;DR — My TrueNAS host rebooted overnight three times across two weeks before I tracked down the bug. It wasn't a kernel issue, a power blip, or a thermal problem. It was my own open-source provider accepting a memory configuration the host couldn't actually allocate. The fix in v0.16.1 was three lines of schema validation. The lesson is bigger than three lines: for infrastructure tools, validate at the boundary the user touches, not at the runtime where the failure surfaces.**

I'm Zac Clifton. I maintain [`omni-infra-provider-truenas`](https://github.com/bearbinary/openni-infra-provider-truenas) — an open-source Omni infrastructure provider that runs Talos VMs on TrueNAS. This is the post-mortem on a bug that bit users (including future-me) and what I changed to make sure it stops biting.

---

## The bug, in one sentence

The provider's MachineClass config accepted `memory` (max RAM) and `min_memory` (optional soft floor) as independent fields, with no enforcement that `min_memory <= memory`. The runtime would dutifully ask TrueNAS to start a VM whose minimum memory exceeded its maximum, TrueNAS would try to lock more RAM than the VM was allowed to use, QEMU would die, the host kernel would thrash, and the host would eventually reboot.

The triggering misconfiguration was honest. People set values like:

```yaml
memory: 4096
min_memory: 8192   # typo — they meant 4096 or 2048
```

Schema accepted it. Provider accepted it. TrueNAS accepted it. The first sign of a problem was the host rebooting at 3am.

---

## How it surfaced

I got three independent reports within two weeks. They didn't look like the same bug.

**Report 1**: "TrueNAS rebooted overnight. Logs don't show anything." (Vague. Could be hardware.)

**Report 2**: "Cluster nodes disappeared. NAS came back up, cluster came back up, but something's wrong." (Symptom on the K8s side, not the host side.)

**Report 3**: "I'm seeing OOM kills in dmesg for QEMU and my VM never starts. Did I configure something wrong?" (This was the report that made it clickable.)

The third reporter's MachineClass had a min_memory typo. Once I saw it, the pattern shape was obvious — but I'd already spent hours on the first two reports chasing host-level theories. ZFS ARC. Misbehaving kubelets. PCI passthrough issues. None of them.

The cost of a vague initial bug report on infrastructure software is real. **Reporters describe what they see, not the root cause.** When users see "host rebooted," they don't think "the provider's schema let me misconfigure memory." They think it's TrueNAS, or the kernel, or the hardware.

---

## What the fix looks like

Three lines of schema validation. Twenty lines of Go.

```go
// In the MachineClass schema:
// memory: int (required), min: 1024
// min_memory: int (optional), min: 1024

// New validation in config_validate.go:
func validateMemory(spec *Spec) error {
    if spec.MinMemory == 0 {
        return nil  // unset is fine — defaults to memory
    }
    if spec.MinMemory > spec.Memory {
        return fmt.Errorf(
            "min_memory (%d MiB) must be ≤ memory (%d MiB). "+
            "min_memory is the soft floor that's reserved at VM start; "+
            "memory is the hard cap. The host cannot lock more than memory.",
            spec.MinMemory, spec.Memory,
        )
    }
    return nil
}
```

That's it. The actual fix.

But the **shape** of the fix is the lesson. Where this validation lives, and why, is the part worth talking about.

---

## Three places this validation could live (and why I picked one)

### Option 1: At the runtime, right before the VM start call

The provider's `createVM` step could check `min_memory <= memory` immediately before calling TrueNAS's `vm.start`. If the check fails, return a clear error to Omni.

**Why this is wrong**: the failure surfaces *after* the user has already authored a MachineClass, applied it, watched Omni try to provision against it, and waited for the failure to bubble up. The error message is correct but late. The user has already spent 5 minutes wondering why their cluster won't bootstrap.

### Option 2: At Omni's resource-write boundary

When the MachineClass YAML is applied via `omnictl apply`, Omni's validation hooks could check `min_memory <= memory` and reject the write.

**Why this is wrong**: Omni doesn't know about the semantic constraint. From Omni's perspective, both fields are just integers. The constraint is provider-specific, and the provider isn't in the apply-time validation path by default.

### Option 3: At the provider's schema layer

The provider registers a JSON Schema with Omni at startup. That schema describes the shape of every MachineClass field. JSON Schema's `dependencies` and conditional clauses can encode "if min_memory is set, it must be ≤ memory." Apply-time validation by Omni against the schema rejects the bad config before anything tries to run it.

**Why this is right**: it fails at the boundary the user actually touches. When `omnictl apply` lands a bad MachineClass, Omni rejects it immediately with a clear error message that names both values and the rule. The user never gets to runtime. The host never gets a chance to OOM.

This is what v0.16.1 ships.

---

## The principle

The general form: **validate at the boundary, not at the runtime**, where:

- The **boundary** is the point at which the user's intent enters the system (a YAML apply, a form submission, an API call).
- The **runtime** is the point at which that intent is realized as a side effect (a VM starting, a database write, an external API call).

Validation at the boundary fails fast. The user gets feedback in the same breath they made the decision in. Validation at the runtime fails late, often catastrophically, and almost always with a worse error message because by the time you've reached runtime you've already lost context about what the user actually meant.

For *infrastructure* tools specifically, this principle has extra weight. The runtime failure isn't a 500 status or a stack trace — it's a host rebooting, a database getting corrupted, a network getting partitioned. The cost of late failure is asymmetrically larger.

---

## The honesty post-mortem

Three things I should have caught earlier.

**1. I had two fields with a semantic relationship and no constraint.** Whenever two configuration fields have a meaningful relationship — `min < max`, `start < end`, `quota < limit` — there should be a validation rule. If I'm not writing one, I'm telling the user "trust me to never get this wrong." Users don't, and they're right not to.

**2. I designed `min_memory` as optional convenience.** I added `min_memory` for the case where someone wants to over-commit host RAM (start with a small reservation, balloon up to the cap if the host has free RAM). It was a quality-of-life feature. The optional-ness meant I treated it as low-priority for validation. Mistake. Optional fields can still create constraints with required fields, and those constraints deserve the same enforcement effort.

**3. The error messages at the runtime were inscrutable.** Even when the provider eventually errored out at `vm.start`, the message was the TrueNAS API's terse failure, not anything that pointed at memory misconfiguration. If a runtime failure does slip through schema validation, the error should at least say "this looks like it might be a min_memory > memory situation." I didn't write that fallback either.

The schema fix solves the main problem. The error-message improvement is on the M5 backlog.

---

## What this means for OSS infra maintainers

If you maintain an infrastructure tool that users configure via YAML, JSON, or any other declarative interface — three takeaways:

1. **Audit your config schema for semantic relationships.** Any two fields with a numeric or logical constraint between them is a validation candidate. Write the check. Even if "no one will get this wrong" feels true, someone will.

2. **Validate at the apply boundary, not at the runtime.** JSON Schema's conditional clauses (or your framework's equivalent) are the right home for cross-field constraints. Don't push these into runtime code where they fail late.

3. **Treat host-affecting bugs as a different severity class.** A misconfiguration that crashes a VM is recoverable. A misconfiguration that crashes the host the VM is on is not. Validation effort should be weighted by blast radius.

---

## What it feels like, on the maintainer side

I'll be honest: the worst part of this bug wasn't the technical work. It was the realization that for two weeks, three users had hosts rebooting in their homelabs because of something I shipped. The fix was small. The accountability wasn't.

Open-source infra OSS has a weird emotional contract. Users install your code and trust it not to do anything stupid to hardware they paid for. When you discover that it did — even accidentally, even through a config they authored — there's no satisfying remediation. You ship the fix, you write the post-mortem, you tighten the validation, and you hope the next bug is smaller.

This one was, ultimately, smaller than it could have been. v0.16.1 closes the schema gap. The runtime fallback comes next. Future-me won't ship a `min_memory` without a paired validation again, and the principle generalizes well past this specific case.

---

## Try the project

- **Provider repo**: [github.com/bearbinary/omni-infra-provider-truenas](https://github.com/bearbinary/omni-infra-provider-truenas)
- **v0.16.1 release notes**: [github.com/bearbinary/omni-infra-provider-truenas/releases/tag/v0.16.1](#)
- **Hero install guide**: [Kubernetes on TrueNAS SCALE: the Talos + Omni Path](https://dev.to/cliftonz/<hero-post-slug>)

If you've hit a similar "fail-loud-at-the-boundary" bug in your own project, I want to hear it. Open an issue on the repo or find me on [LinkedIn](#).

---

**About the author**: Zac Clifton is an infrastructure engineer building tools for self-hosters and small teams. He maintains `omni-infra-provider-truenas` and writes about the unglamorous parts of shipping infrastructure OSS. Subscribe on [YouTube](#) for monthly deep-dives.
