# Base images pinned to SHA256 digest to prevent supply chain tag mutation.
# To update: docker pull <image> && docker inspect --format='{{index .RepoDigests 0}}' <image>
FROM golang:1.26-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /omni-infra-provider-truenas \
    ./cmd/omni-infra-provider-truenas

FROM alpine:3.21@sha256:c3f8e73fdb79deaebaa2037150150191b9dcbfba68b4a46d70103204c53f4709
RUN apk add --no-cache ca-certificates
COPY --from=builder /omni-infra-provider-truenas /usr/local/bin/
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/omni-infra-provider-truenas"]
