package hasErrgroupNoTest

import "golang.org/x/sync/errgroup"

type Owner struct {
	g errgroup.Group // want `package declares a errgroup field \("g"\) but ships no \*_lifecycle_test.go`
}
