package application

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// memoryEntry is a single stored query→response pair with its embedding.
type memoryEntry struct {
	embedding []float32
	query     string
	response  string
	createdAt time.Time
}

// memoryStore is an in-memory, per-router store of past interactions.
// It supports brute-force cosine similarity search — same approach as
// semanticCacheEntries. No external vector database is required.
// For N ≤ 50,000 entries at 1536 dimensions, linear scan completes in ~5ms.
type memoryStore struct {
	mu      sync.RWMutex
	entries []memoryEntry
}

const (
	defaultSemanticMemoryMaxEntries = 1_000
	maxSemanticMemoryMaxEntries     = 10_000
	semanticMemoryStoreTimeout      = 5 * time.Second
)

func (s *memoryStore) findTopK(
	embedding []float32,
	k int,
	threshold float32,
	ttl time.Duration,
) []memoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	cutoff := now.Add(-ttl)

	type scored struct {
		entry memoryEntry
		score float32
	}
	var candidates []scored
	for _, e := range s.entries {
		if e.createdAt.Before(cutoff) {
			continue
		}
		sim := cosineSimilarity(embedding, e.embedding)
		if sim >= threshold {
			candidates = append(candidates, scored{e, sim})
		}
	}
	// Sort descending by score (simple insertion sort for small N)
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].score > candidates[j-1].score; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}
	if k > len(candidates) {
		k = len(candidates)
	}
	out := make([]memoryEntry, k)
	for i := range out {
		out[i] = candidates[i].entry
	}
	return out
}

func (s *memoryStore) add(entry memoryEntry, ttl time.Duration, maxEntries int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-ttl)
	live := s.entries[:0]
	for _, existing := range s.entries {
		if !existing.createdAt.Before(cutoff) {
			live = append(live, existing)
		}
	}
	s.entries = live
	s.entries = append(s.entries, entry)
	if maxEntries <= 0 {
		maxEntries = defaultSemanticMemoryMaxEntries
	}
	if len(s.entries) > maxEntries {
		s.entries = append([]memoryEntry(nil), s.entries[len(s.entries)-maxEntries:]...)
	}
}

func (s *memoryStore) evictExpired(ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-ttl)
	live := s.entries[:0]
	for _, e := range s.entries {
		if !e.createdAt.Before(cutoff) {
			live = append(live, e)
		}
	}
	s.entries = live
}

// getOrCreateMemory returns the memory store for routerID, creating it if absent.
func (p *featurePipeline) getOrCreateMemory(routerID string) *memoryStore {
	v, _ := p.memStores.LoadOrStore(routerID, &memoryStore{})
	return v.(*memoryStore)
}

// injectSemanticMemory embeds the prompt, finds top-K similar past Q&A pairs,
// and prepends them as few-shot examples to the system prompt.
// Returns updated fields (systemPrompt may be modified).
func (p *featurePipeline) injectSemanticMemory(
	ctx context.Context,
	routerID string,
	cfg map[string]any,
	fields map[string]string,
) {
	if p.embedder == nil {
		return
	}
	modelID, _ := cfg["model_id"].(string)
	maxExamples := 3
	if v := toFloat(cfg["max_examples"]); v > 0 {
		maxExamples = int(v)
	}
	threshold := float32(0.85)
	if v := toFloat(cfg["similarity_threshold"]); v > 0 {
		threshold = float32(v)
	}
	ttl := semanticMemoryTTL(cfg)

	prompt := fields["prompt"]
	if prompt == "" {
		return
	}
	emb, err := p.embedder.Embed(ctx, modelID, prompt)
	if err != nil || len(emb) == 0 {
		return
	}

	store := p.getOrCreateMemory(routerID)
	store.evictExpired(ttl)
	examples := store.findTopK(emb, maxExamples, threshold, ttl)
	if len(examples) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString("Here are similar past interactions for reference:\n\n")
	for i, ex := range examples {
		fmt.Fprintf(&sb, "Example %d:\nQ: %s\nA: %s\n\n", i+1, ex.query, ex.response)
	}
	sb.WriteString("---\n")

	existing := fields["systemPrompt"]
	if existing != "" {
		fields["systemPrompt"] = existing + "\n\n" + sb.String()
	} else {
		fields["systemPrompt"] = sb.String()
	}
}

// storeSemanticMemory embeds the prompt and stores the (prompt, response) pair.
func (p *featurePipeline) storeSemanticMemory(
	ctx context.Context,
	routerID string,
	cfg map[string]any,
	query, response string,
) {
	if p.embedder == nil || query == "" || response == "" {
		return
	}
	modelID, _ := cfg["model_id"].(string)
	emb, err := p.embedder.Embed(ctx, modelID, query)
	if err != nil || len(emb) == 0 {
		return
	}
	store := p.getOrCreateMemory(routerID)
	store.add(memoryEntry{
		embedding: emb,
		query:     query,
		response:  response,
		createdAt: time.Now(),
	}, semanticMemoryTTL(cfg), semanticMemoryMaxEntries(cfg))
}

func semanticMemoryTTL(cfg map[string]any) time.Duration {
	ttlDays := 30
	if v := toFloat(cfg["ttl_days"]); v > 0 {
		ttlDays = int(v)
	}
	return time.Duration(ttlDays) * 24 * time.Hour
}

func semanticMemoryMaxEntries(cfg map[string]any) int {
	maxEntries := defaultSemanticMemoryMaxEntries
	if v := toFloat(cfg["max_entries"]); v > 0 {
		maxEntries = int(v)
	}
	if maxEntries > maxSemanticMemoryMaxEntries {
		return maxSemanticMemoryMaxEntries
	}
	return maxEntries
}
