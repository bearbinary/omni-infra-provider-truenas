package hasEmbeddedWgNoTest

import "sync"

type Owner struct {
	sync.WaitGroup // want `package declares a waitgroup field \("<embedded>"\) but ships no \*_lifecycle_test.go`
}
