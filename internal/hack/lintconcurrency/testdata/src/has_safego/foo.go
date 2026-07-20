package hasSafego

// safeGo is a rejected pattern per docs/concurrency-patterns.md; the
// analyzer flags it regardless of whether the package ships a lifecycle
// test.
func safeGo(fn func()) { // want `function .safeGo. is a rejected pattern`
	go func() {
		defer func() { _ = recover() }()
		fn()
	}()
}
