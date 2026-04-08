# Base images pinned to SHA256 digest to prevent supply chain tag mutation.
# To update: docker pull <image> && docker inspect --format='{{index .RepoDigests 0}}' <image>
FROM golang:1.26-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS builder

ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" \
    -o /omni-infra-provider-truenas ./cmd/omni-infra-provider-truenas

# Distroless: no shell, no package manager, no OS vulnerabilities.
# ca-certificates are included in the static image.
# nonroot tag runs as uid 65532 (same security posture as previous USER 65534:65534).
FROM gcr.io/distroless/static-debian12:nonroot@sha256:a9329520abc449e3b14d5bc3a6ffae065bdde0f02667fa10880c49b35c109fd1

LABEL org.opencontainers.image.title="omni-infra-provider-truenas" \
      org.opencontainers.image.description="TrueNAS SCALE infrastructure provider for Sidero Omni" \
      org.opencontainers.image.url="https://github.com/bearbinary/omni-infra-provider-truenas" \
      org.opencontainers.image.source="https://github.com/bearbinary/omni-infra-provider-truenas" \
      org.opencontainers.image.vendor="Bear Binary" \
      org.opencontainers.image.licenses="MIT"

COPY --from=builder /omni-infra-provider-truenas /usr/local/bin/

# No HEALTHCHECK: distroless has no shell to exec health commands.
# Health is checked by the Omni SDK (WithHealthCheckFunc) and Kubernetes probes.

ENTRYPOINT ["/usr/local/bin/omni-infra-provider-truenas"]
