# Multi-arch Dockerfile that uses pre-built binaries instead of compiling inside
# the container. Go cross-compiles natively in seconds — no QEMU emulation needed.
#
# The binary is passed in via --build-arg BINARY=<path> from the CI workflow,
# which builds it with: GOOS=linux GOARCH=<arch> go build ...
#
# Distroless: no shell, no package manager, no OS vulnerabilities.
# ca-certificates are included in the static image.
# The :nonroot tag defaults to uid/gid 65532. We override to 65534 below.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:a9329520abc449e3b14d5bc3a6ffae065bdde0f02667fa10880c49b35c109fd1

LABEL org.opencontainers.image.title="omni-infra-provider-truenas" \
      org.opencontainers.image.description="TrueNAS SCALE infrastructure provider for Sidero Omni" \
      org.opencontainers.image.url="https://github.com/bearbinary/omni-infra-provider-truenas" \
      org.opencontainers.image.source="https://github.com/bearbinary/omni-infra-provider-truenas" \
      org.opencontainers.image.vendor="Bear Binary" \
      org.opencontainers.image.licenses="MIT"

ARG TARGETARCH
# --chmod=0755 is required because actions/upload-artifact + download-artifact
# package files as ZIP and strip the execute bit. Without this, the COPY
# preserves the zero-permission file and the container fails at startup with
# `exec: "/usr/local/bin/omni-infra-provider-truenas": permission denied`.
# Regression affected v0.14.1–v0.14.3 images. BuildKit (used by docker/build-push-action)
# has supported --chmod on COPY since 2020.
COPY --chmod=0755 _out/omni-infra-provider-truenas-linux-${TARGETARCH} /usr/local/bin/omni-infra-provider-truenas

# Run as uid/gid 65534 (traditional `nobody`/`nogroup`) to align with the
# owner of bind-mounted paths from the TrueNAS host (where nobody=65534 is
# the default account for non-privileged shares). The distroless :nonroot
# tag defaults to 65532, which does not collide with host `nobody`, causing
# permission errors on any volume mounted from TrueNAS without explicit
# chown. UID 65534 has no corresponding entry in the image's /etc/passwd
# (distroless only registers uid 0 and 65532) — this is fine for our
# statically-linked Go binary, which does not do username lookups.
USER 65534:65534

ENTRYPOINT ["/usr/local/bin/omni-infra-provider-truenas"]
