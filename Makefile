BINARY := omni-infra-provider-truenas
IMAGE := ghcr.io/bearbinary/$(BINARY)
TAG ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test test-v test-integration test-e2e test-record lint lint-helm scan setup-hooks image clean

build:
	CGO_ENABLED=0 go build -o _out/$(BINARY) ./cmd/$(BINARY)

test:
	go test -race ./... -count=1

test-v:
	go test -race ./... -v -count=1

test-integration:  ## Run client integration tests against a real TrueNAS
	go test -tags=integration ./internal/client/... -v -count=1 -timeout=120s

test-e2e:  ## Run all integration + cleanup tests against a real TrueNAS
	go test -tags=integration ./internal/... -v -count=1 -timeout=300s -p 1

test-record:  ## Re-record cassettes from live TrueNAS (requires TRUENAS_TEST_HOST + TRUENAS_TEST_API_KEY)
	RECORD_CASSETTES=1 go test ./internal/... -v -count=1 -timeout=300s -p 1 -run "TestIntegration_|TestContract_|TestStepOrchestration_|TestStepOrchestration_MaybeResizeZvol"

lint:
	golangci-lint run ./...

lint-helm:  ## Lint and validate Helm chart
	helm lint deploy/helm/omni-infra-provider-truenas
	helm template test deploy/helm/omni-infra-provider-truenas --namespace omni-infra-provider > /dev/null

scan:  ## Scan for secrets with betterleaks
	betterleaks git --baseline-path .betterleaks-baseline.json --verbose --exit-code 1

setup-hooks:  ## Install git hooks (pre-push secret scanning)
	@cp scripts/pre-push .git/hooks/pre-push
	@chmod +x .git/hooks/pre-push
	@echo "Git hooks installed"

image:
	docker build --build-arg VERSION=$(TAG) -t $(IMAGE):$(TAG) .

generate:
	protoc --go_out=. --go_opt=paths=source_relative api/specs/specs.proto

clean:
	rm -rf _out/
