package application

import (
	"fmt"
	"math"
	"sync"
)

// fingerprintStats tracks rolling response length statistics per router.
// Used to detect anomalous response patterns (model drift, injection campaigns).
type fingerprintStats struct {
	mu        sync.Mutex
	window    []int // ring buffer of recent response lengths
	pos       int   // write position in ring buffer
	count     int   // number of entries added (capped at cap(window))
	sum       float64
	sumSq     float64
	maxWindow int
}

func newFingerprintStats(windowSize int) *fingerprintStats {
	if windowSize <= 0 {
		windowSize = 100
	}
	return &fingerprintStats{
		window:    make([]int, windowSize),
		maxWindow: windowSize,
	}
}

// record adds a new response length observation.
// Returns (anomaly, detail) — anomaly is true when the observation is more
// than alertThreshold standard deviations from the rolling mean.
func (s *fingerprintStats) record(length int, alertThreshold float64) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update ring buffer
	if s.count < s.maxWindow {
		s.count++
	} else {
		// Evict oldest entry
		old := float64(s.window[s.pos])
		s.sum -= old
		s.sumSq -= old * old
	}
	s.window[s.pos] = length
	s.pos = (s.pos + 1) % s.maxWindow
	s.sum += float64(length)
	s.sumSq += float64(length) * float64(length)

	if s.count < 10 {
		return false, "" // not enough data for anomaly detection
	}

	mean := s.sum / float64(s.count)
	variance := s.sumSq/float64(s.count) - mean*mean
	if variance < 0 {
		variance = 0
	}
	stddev := math.Sqrt(variance)
	if stddev < 1 {
		return false, "" // near-zero variance, nothing to detect
	}

	deviation := math.Abs(float64(length)-mean) / stddev
	if deviation > alertThreshold {
		return true, fmt.Sprintf("response length %d deviates %.1fσ from mean %.0f (±%.0f)", length, deviation, mean, stddev)
	}
	return false, ""
}

// getOrCreateFingerprint returns the fingerprint stats for routerID.
func (p *featurePipeline) getOrCreateFingerprint(routerID string, windowSize int) *fingerprintStats {
	v, loaded := p.fingerprints.Load(routerID)
	if loaded {
		return v.(*fingerprintStats)
	}
	s := newFingerprintStats(windowSize)
	actual, _ := p.fingerprints.LoadOrStore(routerID, s)
	return actual.(*fingerprintStats)
}
