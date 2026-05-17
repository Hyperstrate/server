package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// coalescer deduplicates concurrent inference requests with the same key.
//
// When N requests arrive with the same prompt within the coalescing window,
// the first one (the "leader") makes the upstream call; the rest (waiters)
// block on the leader's done channel. When the leader completes it broadcasts
// the result to all waiters. Each request in the coalesced group receives
// an identical, fresh response — there is no stale data risk unlike caching.
//
// If the upstream call exceeds window_ms, late-joining waiters fall through to
// their own upstream calls (graceful degradation, not a thundering herd).
type coalescer struct {
	mu       sync.Mutex
	inflight map[string]*coalescedGroup
}

type coalescedGroup struct {
	done       chan struct{}
	result     *ModelInferResult
	err        error
	waiters    int32 // current number of waiters (atomic)
	maxWaiters int32 // 0 = unlimited
}

func newCoalescer() *coalescer {
	return &coalescer{inflight: make(map[string]*coalescedGroup)}
}

// Do executes fn exactly once for key. Concurrent callers with the same key
// join the in-flight group and receive the same result.
//
// windowMs: maximum time a waiter blocks for the leader's result (default 200).
// maxWaiters: max concurrent waiters; excess callers make their own calls (0 = unlimited).
//
// Returns (result, wasCoalesced, err).
// wasCoalesced=true means this caller was a waiter (no upstream cost incurred).
func (c *coalescer) Do(
	ctx context.Context,
	key string,
	windowMs, maxWaiters int,
	fn func() (*ModelInferResult, error),
) (*ModelInferResult, bool, error) {
	if windowMs <= 0 {
		windowMs = 200
	}
	window := time.Duration(windowMs) * time.Millisecond

	c.mu.Lock()
	if g, ok := c.inflight[key]; ok {
		// An in-flight request exists for this key.
		// Enforce max_waiters limit before joining.
		if g.maxWaiters > 0 && atomic.LoadInt32(&g.waiters) >= g.maxWaiters {
			c.mu.Unlock()
			// Too many waiters — make our own call.
			result, err := fn()
			return result, false, err
		}
		atomic.AddInt32(&g.waiters, 1)
		c.mu.Unlock()

		defer atomic.AddInt32(&g.waiters, -1)
		select {
		case <-g.done:
			if g.err != nil {
				return nil, true, g.err
			}
			r := *g.result // copy so callers can't mutate shared state
			return &r, true, nil
		case <-time.After(window):
			// Leader is taking too long — fall through to our own call.
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
		result, err := fn()
		return result, false, err
	}

	// No in-flight group — we become the leader.
	g := &coalescedGroup{
		done:       make(chan struct{}),
		maxWaiters: int32(maxWaiters),
	}
	c.inflight[key] = g
	c.mu.Unlock()

	result, err := fn()

	g.result = result
	g.err = err
	close(g.done) // broadcast to all waiters

	// Keep the group in the map briefly so any requests that read the key just
	// before we closed done can still receive the result without going upstream.
	go func() {
		time.Sleep(10 * time.Millisecond)
		c.mu.Lock()
		if c.inflight[key] == g {
			delete(c.inflight, key)
		}
		c.mu.Unlock()
	}()

	return result, false, err
}

// coalesceKey returns a stable hash over the inputs that uniquely identify an
// upstream inference call: router, target model, and prompt fields.
func coalesceKey(routerID, modelID string, fields map[string]string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00", routerID, modelID)
	b, _ := json.Marshal(fields)
	h.Write(b)
	return fmt.Sprintf("%x", h.Sum(nil))
}
