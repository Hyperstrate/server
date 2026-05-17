package application

import (
	"sync"
	"time"

	"hyperstrate/server/internal/modules/router/domain"
)

type circuitState int

const (
	circuitClosed   circuitState = iota
	circuitOpen
	circuitHalfOpen
)

type circuitBreaker struct {
	mu               sync.Mutex
	state            circuitState
	failures         int
	successes        int
	openUntil        time.Time
	failureThreshold int
	successThreshold int
	resetTimeout     time.Duration
}

func newCircuitBreaker(failureThreshold, successThreshold int, resetTimeout time.Duration) *circuitBreaker {
	if failureThreshold <= 0 { failureThreshold = 5 }
	if successThreshold <= 0 { successThreshold = 2 }
	if resetTimeout <= 0    { resetTimeout = 30 * time.Second }
	return &circuitBreaker{
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		resetTimeout:     resetTimeout,
	}
}

// Allow returns true when the request should be forwarded.
func (cb *circuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == circuitOpen {
		if time.Now().After(cb.openUntil) {
			cb.state = circuitHalfOpen
			cb.successes = 0
			return true
		}
		return false
	}
	return true
}

func (cb *circuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	if cb.state == circuitHalfOpen {
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.state = circuitClosed
		}
	}
}

func (cb *circuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.state == circuitHalfOpen || cb.failures >= cb.failureThreshold {
		cb.state = circuitOpen
		cb.openUntil = time.Now().Add(cb.resetTimeout)
		cb.successes = 0
	}
}

func (cb *circuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state == circuitOpen && time.Now().Before(cb.openUntil)
}

// ── Pool of breakers, one per target ─────────────────────────────────────────

type circuitBreakerPool struct {
	mu       sync.Mutex
	breakers map[string]*circuitBreaker
}

func newCircuitBreakerPool() *circuitBreakerPool {
	return &circuitBreakerPool{breakers: make(map[string]*circuitBreaker)}
}

func (p *circuitBreakerPool) get(targetID string) *circuitBreaker {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cb, ok := p.breakers[targetID]; ok {
		return cb
	}
	cb := newCircuitBreaker(5, 2, 30*time.Second)
	p.breakers[targetID] = cb
	return cb
}

// Cleanup removes breakers for target IDs that are no longer active,
// preventing unbounded growth when targets are frequently added and deleted.
func (p *circuitBreakerPool) Cleanup(activeTargetIDs map[string]struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id := range p.breakers {
		if _, active := activeTargetIDs[id]; !active {
			delete(p.breakers, id)
		}
	}
}

// filterHealthy removes targets whose circuit is open, falling back to all
// targets if every target is tripped (degrade gracefully rather than returning
// "no targets available").
func (p *circuitBreakerPool) filterHealthy(targets []domain.RouterTarget) []domain.RouterTarget {
	var live []domain.RouterTarget
	for _, t := range targets {
		if !p.get(t.ID).IsOpen() {
			live = append(live, t)
		}
	}
	if len(live) == 0 {
		return targets
	}
	return live
}
