package application

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"strings"
	"sync"
)

// EmbeddingProvider computes a dense vector for a piece of text using a
// specific model. The modelID must match a model registered in the AI catalog.
//
// When the noopEmbedder is active (no real provider wired), all calls return
// an error and semantic features degrade gracefully.
type EmbeddingProvider interface {
	Embed(ctx context.Context, modelID string, text string) ([]float32, error)
}

// embeddingCache stores per-utterance embeddings keyed by a hash of
// (modelID, targetID, sorted utterances). Embeddings are computed lazily
// on the first request that needs them, then reused until the process restarts
// or utterances change (cache key changes automatically).
//
// Storing individual embeddings (not a centroid) lets the classifier use
// max-similarity scoring — the same approach as the semantic-router library —
// which is more accurate when a route's utterances cover diverse intents.
type embeddingCache struct {
	mu      sync.Mutex
	entries map[string]embeddingCacheEntry // key → one vector per utterance
}

type embeddingCacheEntry struct {
	targetID string
	vectors  [][]float32
}

func newEmbeddingCache() *embeddingCache {
	return &embeddingCache{entries: make(map[string]embeddingCacheEntry)}
}

// GetOrComputeAll returns cached per-utterance embeddings, computing and
// caching them on first call. Returns nil when all Embed calls fail.
func (c *embeddingCache) GetOrComputeAll(
	ctx context.Context,
	embedder EmbeddingProvider,
	modelID string,
	targetID string,
	utterances []string,
) [][]float32 {
	key := utteranceCacheKey(modelID, targetID, utterances)

	c.mu.Lock()
	if v, ok := c.entries[key]; ok {
		c.mu.Unlock()
		return v.vectors
	}
	c.mu.Unlock()

	vecs := make([][]float32, 0, len(utterances))
	for _, u := range utterances {
		emb, err := embedder.Embed(ctx, modelID, u)
		if err == nil && len(emb) > 0 {
			vecs = append(vecs, emb)
		}
	}
	if len(vecs) == 0 {
		return nil
	}

	c.mu.Lock()
	c.entries[key] = embeddingCacheEntry{targetID: targetID, vectors: vecs}
	c.mu.Unlock()
	return vecs
}

// Invalidate removes cached embeddings for a specific target so they are
// recomputed after utterances are updated.
func (c *embeddingCache) Invalidate(targetID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, entry := range c.entries {
		if entry.targetID == targetID {
			delete(c.entries, k)
		}
	}
}

func utteranceCacheKey(modelID, targetID string, utterances []string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s", modelID, targetID, strings.Join(utterances, "\x01"))))
	return fmt.Sprintf("%x", h)
}

// ── Math helpers ──────────────────────────────────────────────────────────────

// cosineSimilarity returns the cosine similarity in [-1, 1].
// Returns 0 for zero-length or mismatched vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}
