package hasWgWithTest

import "sync"

type Owner struct {
	wg sync.WaitGroup // OK — companion lifecycle test present.
}

func (o *Owner) Wait() { o.wg.Wait() }
