# Personal-fork release template

A minimal release pipeline for someone maintaining a personal fork of
`omni-infra-provider-truenas`. Copy-paste this into your fork and you get
working `v*`-tag → `ghcr.io/<you>/omni-infra-provider-truenas:<tag>`
publishing without dragging in the upstream signed-tag / cosign / SBOM /
multi-arch machinery.

## When to use this

Use this shape if **all** of the following are true:

- You have a fork of `bearbinary/omni-infra-provider-truenas`.
- Your fork exists to ship a small number of targeted changes (a bugfix,
  a personal preference, a WIP proposal) — not to be a long-term parallel
  distribution.
- Your fork's users are yourself and maybe a handful of people who trust
  you directly.
- You are OK with a linux/amd64-only image, unsigned image, and no SBOM
  attestation.

If any of the above is false, use the upstream `.github/workflows/release.yaml`
instead. It costs more per release but gives you signed tags, keyless cosign
signing via Sigstore OIDC, SPDX SBOM attestation, `preview` / `{major}.{minor}`
tag channels, multi-arch (amd64 + arm64), and a pre-push smoke gate.

## Files

| File | Where it goes in your fork |
|---|---|
| `release.yaml.template` | Copy to `.github/workflows/release.yaml` (replace upstream file) |
| `Dockerfile.labels.example` | Reference snippet — apply the label block by hand to your fork's root `Dockerfile` |

## What this workflow does

1. `checkout` at the pushed tag ref.
2. `setup-go` using the version in `go.mod`.
3. `make test` (unit tests, race-enabled — same as upstream).
4. Build `linux/amd64` binary with `-ldflags="-X main.version=$TAG"`.
5. `docker login ghcr.io` using `GITHUB_TOKEN`.
6. `docker buildx build --push` — publishes `:<tag>` and `:latest`.
7. Smoke test: `docker pull` + `docker run --rm <image> --version`, assert
   the reported version matches the git tag.

## Testing this template before your first real release

Never let your first tag be the first CI run. Options:

1. **Scratch tag**: `git tag v0.0.0-test.1 && git push origin v0.0.0-test.1`, watch the run publish `ghcr.io/<you>/omni-infra-provider-truenas:v0.0.0-test.1`, then `gh release delete v0.0.0-test.1 --yes && git push origin :refs/tags/v0.0.0-test.1`. Note: the GHCR image is still there until you delete it via `gh api DELETE /user/packages/container/<name>/versions/<id>`.
2. **`act` locally**: `act push --eventpath scratch-tag-event.json -s GITHUB_TOKEN=$(gh auth token)` — see the [act docs](https://github.com/nektos/act) for buildx compatibility.
3. **`workflow_dispatch` variant**: add `workflow_dispatch:` under `on:` and trigger a dry run from the Actions tab.

## What this workflow does NOT do (intentional)

Compared to the upstream `bearbinary/omni-infra-provider-truenas` release
workflow:

> **Warning — still absent from this template.** Even with the hardening
> commits landed, this template does NOT run cosign keyless image signing,
> SPDX SBOM generation, or multi-arch builds. Do not use it as a
> distribution-grade pipeline for consumers who verify signatures or
> pull `linux/arm64`.

| Guarantee | Upstream | This template | If dropped, attacker with... |
|---|---|---|---|
| Signed-tag verify (`git tag --verify`) | ✅ | ✅ | your GitHub credentials can publish any release from a lightweight tag |
| Immutable GitHub Release object (`gh release view` guard) | ✅ | ❌ | write access can retract & republish release notes silently |
| Immutable GHCR tag | ❌ | ❌ | write access can overwrite `:vX.Y.Z` — consumers pulling by tag silently get the new image (footgun in both flavors) |
| CHANGELOG.md release-notes extraction | ✅ | ❌ | write access can drift the GitHub Release body from what CHANGELOG says shipped |
| Multi-arch build (amd64 + arm64) | ✅ | ❌ (amd64 only) | no attacker impact — feature gap, not a security control |
| QEMU-emulated multi-arch smoke test | ✅ | ❌ | no attacker impact — regression net for arm64 only |
| `:preview` newest-by-semver channel | ✅ | ❌ | no attacker impact — feature gap |
| `:{major}.{minor}` stable channel | ✅ | ❌ | no attacker impact — feature gap |
| Pre-release detection (suppress `:latest` on `-rc.N`) | ✅ | ✅ | write access can ship an unstable rc as `:latest` to every consumer |
| cosign keyless image signing | ✅ | ❌ | GHCR write access can push an image consumers cannot cryptographically distinguish from a legit build |
| SPDX SBOM generation + cosign attestation | ✅ | ❌ | no downstream way to audit what shipped |
| Binary signing (`cosign sign-blob` → `.sigstore.json`) | ✅ | ❌ | binary downloads have no offline verification path |
| Dockerfile invariant checks (`USER 65534:65534`, `--chmod=0755`) | ✅ | ❌ | a fork edit that reintroduces a root Dockerfile is not caught pre-release |
| Observability bundle (dashboards + alerts on release page) | ✅ | ❌ | no attacker impact — feature gap |

Each of those is a supply-chain guarantee that costs CI time to produce.
For a personal fork with a small trusted audience, the pure-feature gaps
are usually overkill; for the canonical distribution they are load-bearing.
The security-control gaps (cosign, SBOM, immutable release, Dockerfile
invariants) are the ones to think twice about before shipping to anyone
other than yourself.

## Known failure modes

- **`Smoke test failed: expected v0.16.2, got v0.16.2-dirty`** — working tree had untracked changes; ldflag path picked them up. Commit or stash.
- **`Smoke test failed: expected v0.16.2, got dev`** — the `-X main.version=` ldflag didn't take effect. Check `main.go` has `var version = "dev"` (not `const`).
- **`docker: Error response from daemon: pull access denied`** on the smoke step — GHCR package visibility defaults to private. Go to `https://github.com/users/<you>/packages/container/omni-infra-provider-truenas/settings` and set visibility to public if you want unauthenticated pulls to work.
- **Tag deleted after publish** — the GHCR image at `:<tag>` and `:latest` are NOT removed by `git push origin :refs/tags/<tag>`. Delete via `gh api DELETE /user/packages/container/omni-infra-provider-truenas/versions/<id>`. A new tag with the same name will OVERWRITE the image (GHCR tags are mutable; consumers pulling by tag will silently get the new image).
- **GHA cache poisoning** — `cache-to: type=gha,mode=max` shares scope with PR workflows. If your fork accepts PRs, an attacker with PR-write access can populate the cache with a poisoned layer that lands in the next release build. Consider `mode=min` or dropping the cache on the release job if your fork accepts collaborators.
- **`:latest` on pre-release tags** — this template gates `:latest` on stable tags only (no `-` in the version). If you disable the `Detect prerelease` step, EVERY tag including `-rc.N` will overwrite `:latest` for consumers.

## Upgrading a fork off this template

If your fork grows past "a couple of targeted patches" and starts serving
users who need signature verification or multi-arch — copy the upstream
`.github/workflows/release.yaml` back in and adopt the full pipeline. The
release-testing skill in the upstream repo (`docs/release-testing.md`)
documents the promotion workflow you should adopt in that case.

## Credits

Pattern first documented on `emil-jacero/omni-infra-provider-truenas`
commit [`009eef6`](https://github.com/emil-jacero/omni-infra-provider-truenas/commit/009eef65869e866f9c7bbb5ee95aa9929170320e)
by Emil Larsson, who reduced the upstream release workflow to this
single-arch fork-friendly shape while shipping a targeted WaitGroup-reuse
fix. This template generalizes that pattern.
