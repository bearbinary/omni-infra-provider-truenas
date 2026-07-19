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

## What this workflow does NOT do (intentional)

Compared to the upstream `bearbinary/omni-infra-provider-truenas` release
workflow:

| Guarantee | Upstream | This template |
|---|---|---|
| Signed-tag verify (`git tag --verify`) | ✅ | ❌ |
| Immutable-release guard (`gh release view`) | ✅ | ❌ |
| CHANGELOG.md release-notes extraction | ✅ | ❌ |
| Multi-arch build (amd64 + arm64) | ✅ | ❌ (amd64 only) |
| QEMU-emulated multi-arch smoke test | ✅ | ❌ |
| `:preview` newest-by-semver channel | ✅ | ❌ |
| `:{major}.{minor}` stable channel | ✅ | ❌ |
| Pre-release detection (suppress `:latest` on `-rc.N`) | ✅ | ❌ |
| cosign keyless image signing | ✅ | ❌ |
| SPDX SBOM generation + cosign attestation | ✅ | ❌ |
| Binary signing (`cosign sign-blob` → `.sigstore.json`) | ✅ | ❌ |
| Dockerfile invariant checks (`USER 65534:65534`, `--chmod=0755`) | ✅ | ❌ |
| Observability bundle (dashboards + alerts on release page) | ✅ | ❌ |

Each of those is a supply-chain guarantee that costs CI time to produce.
For a personal fork with a small trusted audience, they are usually
overkill; for the canonical distribution they are load-bearing.

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
