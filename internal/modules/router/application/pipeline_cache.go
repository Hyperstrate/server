package application

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

func (p *featurePipeline) exactCacheKey(routerID string, fields map[string]string) string {
	b, _ := json.Marshal(fields)
	h := sha256.Sum256(append([]byte(routerID), b...))
	return fmt.Sprintf("%x", h)
}

func (p *featurePipeline) cacheGet(key string, _ map[string]any) *RouteInferResult {
	return p.cacheStore.Get(key)
}

func (p *featurePipeline) cacheSet(key string, result *RouteInferResult, cfg map[string]any) {
	p.cacheStore.Set(key, result, cacheTTL(cfg))
}

func markCachedResult(hit *RouteInferResult, fields map[string]string, hitType string) *RouteInferResult {
	if hit == nil {
		return nil
	}
	hit.CacheHit = true
	hit.CacheHitType = hitType
	if hit.CachedInputTokens <= 0 {
		hit.CachedInputTokens = estimateCachedInputTokens(fields)
	}
	return hit
}

func estimateCachedInputTokens(fields map[string]string) int64 {
	var chars int
	for key, value := range fields {
		if value == "" {
			continue
		}
		// Image URLs and similar references still consume prompt budget, but the
		// text length is a weak proxy. Count the key as a tiny hint so non-prompt
		// requests are not reported as zero saved tokens.
		chars += len(key) + len(value)
	}
	if chars <= 0 {
		return 0
	}
	tokens := int64((chars + 3) / 4)
	if tokens < 1 {
		return 1
	}
	return tokens
}

type semanticCacheEntry struct {
	scopeKey         string
	modelID          string
	embedding        []float32
	content          string
	selectedModelID  string
	selectedTargetID string
	modelDefKey      string
	provider         string
	expiresAt        time.Time
}

func semanticCacheScope(routerID, modelID string) string {
	return routerID + ":" + modelID
}

func (p *featurePipeline) semanticCacheGet(scopeKey, modelID string, embedding []float32, cfg map[string]any) *RouteInferResult {
	threshold := float32(0.92)
	if v, ok := cfg["similarity_threshold"]; ok {
		if n := toFloat(v); n > 0 {
			threshold = float32(n)
		}
	}
	p.semanticMu.Lock()
	defer p.semanticMu.Unlock()
	now := time.Now()
	for i := len(p.semanticEntries) - 1; i >= 0; i-- {
		e := p.semanticEntries[i]
		if now.After(e.expiresAt) {
			continue
		}
		if e.scopeKey != scopeKey || e.modelID != modelID {
			continue
		}
		if cosineSimilarity(embedding, e.embedding) >= threshold {
			return &RouteInferResult{
				Content:          e.content,
				SelectedModelID:  e.selectedModelID,
				SelectedTargetID: e.selectedTargetID,
				ModelDefKey:      e.modelDefKey,
				Provider:         e.provider,
			}
		}
	}
	return nil
}

const semanticCacheMaxEntries = 10_000

func (p *featurePipeline) semanticCacheSet(scopeKey, modelID string, embedding []float32, result *RouteInferResult, cfg map[string]any) {
	ttl := cacheTTL(cfg)
	p.semanticMu.Lock()
	defer p.semanticMu.Unlock()
	now := time.Now()
	live := p.semanticEntries[:0]
	for _, e := range p.semanticEntries {
		if !now.After(e.expiresAt) {
			live = append(live, e)
		}
	}
	if len(live) >= semanticCacheMaxEntries {
		drop := len(live) - semanticCacheMaxEntries + 1
		live = live[drop:]
	}
	p.semanticEntries = append(live, semanticCacheEntry{
		scopeKey:         scopeKey,
		modelID:          modelID,
		embedding:        embedding,
		content:          result.Content,
		selectedModelID:  result.SelectedModelID,
		selectedTargetID: result.SelectedTargetID,
		modelDefKey:      result.ModelDefKey,
		provider:         result.Provider,
		expiresAt:        now.Add(ttl),
	})
}

func cacheTTL(cfg map[string]any) time.Duration {
	if v, ok := cfg["ttl_seconds"]; ok {
		if n := toFloat(v); n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 300 * time.Second
}
