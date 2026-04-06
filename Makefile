BINARY := omni-infra-provider-truenas
IMAGE := ghcr.io/bearbinary/$(BINARY)
TAG ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test test-v test-integration lint image clean

build:
	CGO_ENABLED=0 go build -o _out/$(BINARY) ./cmd/$(BINARY)

test:
	go test ./... -count=1

test-v:
	go test ./... -v -count=1

test-integration:  ## Run integration tests against a real TrueNAS (requires TRUENAS_TEST_URL + TRUENAS_TEST_API_KEY)
	go test -tags=integration ./internal/client/... -v -count=1 -timeout=120s

lint:
	golangci-lint run ./...

image:
	docker build -t $(IMAGE):$(TAG) .

generate:
	protoc --go_out=. --go_opt=paths=source_relative api/specs/specs.proto

clean:
	rm -rf _out/
