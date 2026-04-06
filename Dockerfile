FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /omni-infra-provider-truenas \
    ./cmd/omni-infra-provider-truenas

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /omni-infra-provider-truenas /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/omni-infra-provider-truenas"]
