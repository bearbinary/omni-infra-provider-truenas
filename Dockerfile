# Multi-arch Dockerfile that uses pre-built binaries instead of compiling inside
# the container. Go cross-compiles natively in seconds — no QEMU emulation needed.
#
# The binary is passed in via --build-arg BINARY=<path> from the CI workflow,
# which builds it with: GOOS=linux GOARCH=<arch> go build ...
#
# Distroless: no shell, no package manager, no OS vulnerabilities.
# ca-certificates are included in the static image.
# nonroot tag runs as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:a9329520abc449e3b14d5bc3a6ffae065bdde0f02667fa10880c49b35c109fd1

LABEL org.opencontainers.image.title="omni-infra-provider-truenas" \
      org.opencontainers.image.description="TrueNAS SCALE infrastructure provider for Sidero Omni" \
      org.opencontainers.image.url="https://github.com/bearbinary/omni-infra-provider-truenas" \
      org.opencontainers.image.source="https://github.com/bearbinary/omni-infra-provider-truenas" \
      org.opencontainers.image.vendor="Bear Binary" \
      org.opencontainers.image.licenses="MIT"

ARG TARGETARCH
COPY _out/omni-infra-provider-truenas-linux-${TARGETARCH} /usr/local/bin/omni-infra-provider-truenas

ENTRYPOINT ["/usr/local/bin/omni-infra-provider-truenas"]
