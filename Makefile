BINARY := omni-infra-provider-truenas
IMAGE := ghcr.io/bearbinary/$(BINARY)
TAG ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test test-v test-integration test-e2e test-record test-stress lint lint-concurrency lint-helm scan setup-hooks image clean

build:
	CGO_ENABLED=0 go build -o _out/$(BINARY) ./cmd/$(BINARY)

test:
	go test -race -vet=all ./... -count=1

test-v:
	go test -race -vet=all ./... -v -count=1

test-stress:  ## Deterministic race stress: iterate concurrency-heavy packages 30× to surface probabilistic races (Emil's WaitGroup class)
	# No -run filter: the previous `Test.*(Concurrent|Race|Lifecycle|Stress)` regex
	# quietly excluded real concurrency tests (ReaderGoroutineExitsOnClose,
	# CallAfterCloseReturnsErrTransportClosed, WSChaos_*, every noderotation test).
	# Iteration is the sole source of truth: `-count=30` catches Emil-class
	# regressions >99% (per test-body comments) at a fraction of the prior
	# 100× wall time. Package list stays scoped to the concurrency-heavy
	# subtrees so the job stays under ~45 min on GHA runners.
	go test -race -count=30 -timeout=1200s \
		./internal/client/... \
		./internal/singleton/... \
		./internal/noderotation/...

test-integration:  ## Run client integration tests against a real TrueNAS
	go test -tags=integration ./internal/client/... -v -count=1 -timeout=120s

test-singleton:  ## Run singleton integration tests against a real Omni (requires OMNI_ENDPOINT + OMNI_SERVICE_ACCOUNT_KEY)
	go test -v -count=1 -timeout=120s -run "TestIntegration_" ./internal/singleton/

test-e2e:  ## Run all integration + cleanup tests against a real TrueNAS
	go test -tags=integration ./internal/... -v -count=1 -timeout=300s -p 1

test-record:  ## Re-record cassettes from live TrueNAS (requires TRUENAS_TEST_HOST + TRUENAS_TEST_API_KEY)
	RECORD_CASSETTES=1 go test ./internal/... -v -count=1 -timeout=300s -p 1 -run "TestIntegration_|TestContract_|TestStepOrchestration_|TestStepOrchestration_MaybeResizeZvol"

lint:
	golangci-lint run ./...

lint-concurrency:  ## Enforce: every long-lived goroutine owner (sync.WaitGroup / errgroup.Group on struct) has a *_lifecycle_test.go / *_stress_test.go / *_race_test.go companion
	go run ./internal/hack/lintconcurrency/cmd/lintconcurrency ./...

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
