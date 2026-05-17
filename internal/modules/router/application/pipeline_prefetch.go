package application

import (
	"context"
	"time"
)

const (
	defaultResponsePrefetchMaxFollowUps   = 4
	defaultResponsePrefetchMaxConcurrency = 2
	defaultResponsePrefetchTimeout        = 5 * time.Second
	maxResponsePrefetchMaxFollowUps       = 16
	maxResponsePrefetchMaxConcurrency     = 8
	maxResponsePrefetchTimeout            = 30 * time.Second
)

type responsePrefetchSettings struct {
	followUps   []string
	concurrency int
	timeout     time.Duration
	ttl         time.Duration
}

func newResponsePrefetchSettings(cfg map[string]any) responsePrefetchSettings {
	followUps := extractStringSlice(cfg["follow_up_prompts"])
	maxFollowUps := boundedConfigInt(cfg["max_follow_ups"], defaultResponsePrefetchMaxFollowUps, maxResponsePrefetchMaxFollowUps)
	if len(followUps) > maxFollowUps {
		followUps = followUps[:maxFollowUps]
	}
	concurrency := boundedConfigInt(cfg["max_concurrency"], defaultResponsePrefetchMaxConcurrency, maxResponsePrefetchMaxConcurrency)
	if len(followUps) > 0 && concurrency > len(followUps) {
		concurrency = len(followUps)
	}
	timeout := defaultResponsePrefetchTimeout
	if v := toFloat(cfg["timeout_ms"]); v > 0 {
		timeout = time.Duration(v) * time.Millisecond
	} else if v := toFloat(cfg["timeout_seconds"]); v > 0 {
		timeout = time.Duration(v) * time.Second
	}
	if timeout > maxResponsePrefetchTimeout {
		timeout = maxResponsePrefetchTimeout
	}
	return responsePrefetchSettings{
		followUps:   followUps,
		concurrency: concurrency,
		timeout:     timeout,
		ttl:         cacheTTL(cfg),
	}
}

func boundedConfigInt(value any, fallback, max int) int {
	n := int(toFloat(value))
	if n <= 0 {
		n = fallback
	}
	if n > max {
		return max
	}
	return n
}

func (p *featurePipeline) dispatchResponsePrefetch(
	ctx context.Context,
	routerID string,
	selectedModelID string,
	baseContent string,
	systemPrompt string,
	options map[string]any,
	cfg map[string]any,
	inferencer ModelInferencer,
) int {
	settings := newResponsePrefetchSettings(cfg)
	if len(settings.followUps) == 0 || settings.concurrency <= 0 || selectedModelID == "" || inferencer == nil {
		return 0
	}
	prefetchOptions := copyOptions(options)
	go func() {
		jobs := make(chan string)
		done := make(chan struct{}, settings.concurrency)
		for i := 0; i < settings.concurrency; i++ {
			go func() {
				defer func() { done <- struct{}{} }()
				for followUp := range jobs {
					speculativeFields := map[string]string{
						"prompt":       baseContent + "\n\nUser: " + followUp,
						"systemPrompt": systemPrompt,
					}
					prefetchCtx, cancel := context.WithTimeout(ctx, settings.timeout)
					r, prefetchErr := inferencer.InferModel(prefetchCtx, selectedModelID, speculativeFields, copyOptions(prefetchOptions))
					cancel()
					if prefetchErr == nil && r != nil {
						specResult := &RouteInferResult{Content: r.Content, SelectedModelID: selectedModelID}
						specKey := p.exactCacheKey(routerID, speculativeFields)
						p.cacheStore.Set(specKey, specResult, settings.ttl)
					}
				}
			}()
		}
		for _, followUp := range settings.followUps {
			select {
			case jobs <- followUp:
			case <-ctx.Done():
				close(jobs)
				for i := 0; i < settings.concurrency; i++ {
					<-done
				}
				return
			}
		}
		close(jobs)
		for i := 0; i < settings.concurrency; i++ {
			<-done
		}
	}()
	return len(settings.followUps)
}
