package application

import (
	"math"
	"math/rand"
	"sort"
	"sync"

	"hyperstrate/server/internal/modules/router/domain"
)

const defaultLatencyEMAAlpha = 0.3

func (p *featurePipeline) updateLatency(targetID string, ms float64) {
	alpha := defaultLatencyEMAAlpha
	if prev, ok := p.latencies.Load(targetID); ok {
		p.latencies.Store(targetID, alpha*ms+(1-alpha)*prev.(float64))
	} else {
		p.latencies.Store(targetID, ms)
	}
}

func selectTarget(
	router *domain.Router,
	targets []domain.RouterTarget,
	latencies *sync.Map,
	_ []float32,
) (*domain.RouterTarget, error) {
	enabled := enabledTargets(targets)
	if len(enabled) == 0 {
		return nil, domain.ErrNoTargetsAvailable
	}
	switch router.Strategy {
	case domain.RoutingStrategyRoundRobin:
		return &enabled[router.RoundRobinIndex%len(enabled)], nil
	case domain.RoutingStrategyRandom:
		return &enabled[randIntn(len(enabled))], nil
	case domain.RoutingStrategyWeighted:
		return weightedSelect(enabled)
	case domain.RoutingStrategyPercentage:
		return percentageSelect(enabled)
	case domain.RoutingStrategyFailover:
		return &enabled[0], nil
	case domain.RoutingStrategyLatencyBased:
		return latencyBasedSelect(enabled, latencies)
	default:
		return &enabled[0], nil
	}
}

func latencyBasedSelect(targets []domain.RouterTarget, latencies *sync.Map) (*domain.RouterTarget, error) {
	best, bestLat := &targets[0], math.MaxFloat64
	for i := range targets {
		if v, ok := latencies.Load(targets[i].ID); ok {
			if lat := v.(float64); lat < bestLat {
				bestLat, best = lat, &targets[i]
			}
		}
	}
	return best, nil
}

func hasFeature(features []domain.RouterFeature, t domain.RouterFeatureType) bool {
	for _, f := range features {
		if f.FeatureType == t {
			return true
		}
	}
	return false
}

func sortedEnabledFeatures(features []domain.RouterFeature) []domain.RouterFeature {
	out := make([]domain.RouterFeature, 0, len(features))
	for _, f := range features {
		if f.IsEnabled {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExecutionOrder < out[j].ExecutionOrder })
	return out
}

func unsupportedStreamingFeature(features []domain.RouterFeature) domain.RouterFeatureType {
	for _, f := range features {
		if !f.IsEnabled {
			continue
		}
		switch f.FeatureType {
		case domain.FeatureFallback,
			domain.FeatureRetry,
			domain.FeatureHedging,
			domain.FeatureQualityGate,
			domain.FeatureMCPTools,
			domain.FeatureStructuredOutput:
			return f.FeatureType
		}
	}
	return ""
}

func sortedEnabledInterceptors(interceptors []domain.RouterInterceptor) []domain.RouterInterceptor {
	out := make([]domain.RouterInterceptor, 0, len(interceptors))
	for _, ic := range interceptors {
		if ic.IsEnabled {
			out = append(out, ic)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExecutionOrder < out[j].ExecutionOrder })
	return out
}

func enabledTargets(targets []domain.RouterTarget) []domain.RouterTarget {
	out := make([]domain.RouterTarget, 0, len(targets))
	for _, t := range targets {
		if t.IsEnabled {
			out = append(out, t)
		}
	}
	return out
}

func (p *featurePipeline) filterHealthyTargets(targets []domain.RouterTarget) []domain.RouterTarget {
	out := make([]domain.RouterTarget, 0, len(targets))
	for _, t := range targets {
		if p.healthChecker.IsModelHealthy(t.ModelID) {
			out = append(out, t)
		}
	}
	return out
}

func weightedSelect(targets []domain.RouterTarget) (*domain.RouterTarget, error) {
	total := 0
	for _, t := range targets {
		total += t.Weight
	}
	if total <= 0 {
		return &targets[0], nil
	}
	pick, cumulative := randIntn(total), 0
	for i := range targets {
		cumulative += targets[i].Weight
		if pick < cumulative {
			return &targets[i], nil
		}
	}
	return &targets[len(targets)-1], nil
}

func percentageSelect(targets []domain.RouterTarget) (*domain.RouterTarget, error) {
	total := 0.0
	for i := range targets {
		total += targets[i].Percentage
	}
	if total <= 0 {
		return &targets[0], nil
	}
	pick, cumulative := rand.Float64()*total, 0.0 //nolint:gosec
	for i := range targets {
		cumulative += targets[i].Percentage
		if pick < cumulative {
			return &targets[i], nil
		}
	}
	return &targets[len(targets)-1], nil
}

func randIntn(n int) int { return rand.Intn(n) } //nolint:gosec

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func copyOptions(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
