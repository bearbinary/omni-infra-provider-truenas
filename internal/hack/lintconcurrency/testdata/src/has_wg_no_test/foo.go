package hasWgNoTest

import "sync"

type Owner struct {
	wg sync.WaitGroup // want `package declares a waitgroup field \("wg"\) but ships no \*_lifecycle_test.go`
}

func (o *Owner) Wait() { o.wg.Wait() }
