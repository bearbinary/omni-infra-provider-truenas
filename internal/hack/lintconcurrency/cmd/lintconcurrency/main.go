// Command lintconcurrency is the standalone singlechecker driver for the
// lintconcurrency Analyzer. Invoked from `make lint-concurrency`.
//
// Usage: `go run ./internal/hack/lintconcurrency/cmd/lintconcurrency ./...`
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/bearbinary/omni-infra-provider-truenas/internal/hack/lintconcurrency"
)

func main() {
	singlechecker.Main(lintconcurrency.Analyzer)
}
