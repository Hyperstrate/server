package application

import (
	"sync"
	"sync/atomic"
	"time"
)

// routerStats holds per-router counters and latency tracking.
type routerStats struct {
	counts   sync.Map // status string → *atomic.Int64
	latency  sync.Mutex
	totalMs  float64
	totalReq int64
}

func (s *routerStats) inc(status string) {
	v, _ := s.counts.LoadOrStore(status, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

func (s *routerStats) recordLatency(d time.Duration) {
	s.latency.Lock()
	s.totalMs += float64(d.Milliseconds())
	s.totalReq++
	s.latency.Unlock()
}

// RouterMetricSnapshot is a read-only view of a router's counters.
type RouterMetricSnapshot struct {
	RouterID  string           `json:"routerId"      validate:"required"`
	Counts    map[string]int64 `json:"counts"        validate:"required"`
	AvgLatMs  float64          `json:"avgLatencyMs"  validate:"required"`
	TotalReqs int64            `json:"totalRequests" validate:"required"`
}

// pipelineMetrics aggregates runtime counters for all routers.
// It is safe for concurrent use.
type pipelineMetrics struct {
	routers sync.Map // routerID → *routerStats
}

func newPipelineMetrics() *pipelineMetrics {
	return &pipelineMetrics{}
}

func (m *pipelineMetrics) statsFor(routerID string) *routerStats {
	v, _ := m.routers.LoadOrStore(routerID, &routerStats{})
	return v.(*routerStats)
}

// record increments the named status counter and records the request latency.
func (m *pipelineMetrics) record(routerID, status string, duration time.Duration) {
	s := m.statsFor(routerID)
	s.inc(status)
	s.recordLatency(duration)
}

// Snapshot returns a point-in-time read of metrics for all routers.
func (m *pipelineMetrics) Snapshot() []RouterMetricSnapshot {
	var out []RouterMetricSnapshot
	m.routers.Range(func(key, val any) bool {
		routerID := key.(string)
		s := val.(*routerStats)

		counts := map[string]int64{}
		s.counts.Range(func(k, v any) bool {
			counts[k.(string)] = v.(*atomic.Int64).Load()
			return true
		})

		s.latency.Lock()
		avg := float64(0)
		total := s.totalReq
		if total > 0 {
			avg = s.totalMs / float64(total)
		}
		s.latency.Unlock()

		out = append(out, RouterMetricSnapshot{
			RouterID:  routerID,
			Counts:    counts,
			AvgLatMs:  avg,
			TotalReqs: total,
		})
		return true
	})
	return out
}
