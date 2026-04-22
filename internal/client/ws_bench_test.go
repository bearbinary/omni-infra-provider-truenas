package client

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// BenchmarkWSCall_ChannelSemVsMutex approximates the writeRequest hot path:
// acquire + release + a minimal "write" (no-op) for a large number of
// uncontended invocations. The channel-semaphore form (shipped) should
// allocate and schedule less than the goroutine-per-call mutex form.
//
// Run with: go test -bench=BenchmarkWSCall_ChannelSem -benchmem ./internal/client/
func BenchmarkWSCall_ChannelSem(b *testing.B) {
	sem := make(chan struct{}, 1)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		<-sem
	}
}

// BenchmarkWSCall_MutexGoroutine mirrors the pre-fix pattern: a goroutine is
// spawned to acquire a sync.Mutex so the caller can race it against a ctx
// channel. Kept for quantitative comparison against the semaphore form.
func BenchmarkWSCall_MutexGoroutine(b *testing.B) {
	var mu sync.Mutex
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		locked := make(chan struct{})

		go func() {
			mu.Lock()
			close(locked)
		}()

		select {
		case <-locked:
		case <-ctx.Done():
		}

		mu.Unlock()
	}
}

// BenchmarkNextWSRequestID_Atomic measures the atomic-counter form of request
// ID generation (shipped in v0.15).
func BenchmarkNextWSRequestID_Atomic(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		_ = nextWSRequestID()
	}
}

// Ensure strings is referenced to avoid a lint warning if this file ever
// has its strings.Contains usage pruned.
var _ = strings.Contains
