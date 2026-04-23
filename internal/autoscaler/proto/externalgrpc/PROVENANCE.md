# externalgrpc protos — provenance

Source: `cluster-autoscaler/cloudprovider/externalgrpc/protos/externalgrpc.proto`
in `kubernetes/autoscaler`, copyright 2022 The Kubernetes Authors,
Apache-2.0 licensed.

Vendored here (rather than imported via a Go module) because the upstream
protos live inside the full `k8s.io/autoscaler/cluster-autoscaler` module,
and importing that pulls the entire cluster-autoscaler binary's dependency
tree (client-go, kube-scheduler, etc.) into our lean provisioner binary.
Vendored alongside Justin Rothgar's `omni-node-autoscaler` PoC, which did
the same thing for the same reason.

## When to refresh

The external-gRPC contract evolves slowly but does evolve. Re-vendor when:

1. A new cluster-autoscaler release changes the proto (check
   `cluster-autoscaler/cloudprovider/externalgrpc/protos/CHANGELOG.md`
   upstream).
2. We want to support a new CloudProvider RPC method for
   feature parity with AWS/GCP/etc.
3. The test assertion
   `TestExternalGRPCProtoContract` (see `internal/autoscaler/contract_test.go`)
   starts failing on a new cluster-autoscaler sidecar version.

## How to refresh

```bash
# Pull the latest proto:
curl -L \
  https://raw.githubusercontent.com/kubernetes/autoscaler/master/cluster-autoscaler/cloudprovider/externalgrpc/protos/externalgrpc.proto \
  -o internal/autoscaler/proto/externalgrpc/externalgrpc.proto

# Re-apply the go_package override:
sed -i '' \
  's|option go_package = .*|option go_package = "github.com/bearbinary/omni-infra-provider-truenas/internal/autoscaler/proto/externalgrpc";|' \
  internal/autoscaler/proto/externalgrpc/externalgrpc.proto

# Regenerate Go stubs (requires protoc + protoc-gen-go + protoc-gen-go-grpc
# from the same toolchain Makefile's `generate` target uses):
protoc \
  --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  internal/autoscaler/proto/externalgrpc/externalgrpc.proto
```

Run `go build ./...` and `go test ./internal/autoscaler/...` after
refreshing. Any breaking change in the proto surfaces as a compile error
in `internal/autoscaler/server.go`.
