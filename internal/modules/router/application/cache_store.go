package application

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"hyperstrate/server/internal/config"
)

// CacheStore is the strategy interface for the exact-match response cache.
// Implementations: MemoryCacheStore (default, single-node), RedisCacheStore (distributed).
type CacheStore interface {
	Get(key string) *RouteInferResult
	Set(key string, result *RouteInferResult, ttl time.Duration)
	Delete(key string)
}

// ── Memory backend (default) ──────────────────────────────────────────────────

type memoryCacheEntry struct {
	content          string
	selectedModelID  string
	selectedTargetID string
	modelDefKey      string
	provider         string
	expiresAt        time.Time
	sequence         uint64
}

const defaultMemoryCacheMaxEntries = 10_000

// MemoryCacheStore is an in-process cache with TTL compaction and a hard cap.
// Suitable for single-node and local development; not shared across instances.
type MemoryCacheStore struct {
	mu         sync.Mutex
	entries    map[string]*memoryCacheEntry
	maxEntries int
	nextSeq    uint64
}

func NewMemoryCacheStore() CacheStore {
	return NewMemoryCacheStoreWithMaxEntries(defaultMemoryCacheMaxEntries)
}

func NewMemoryCacheStoreWithMaxEntries(maxEntries int) CacheStore {
	if maxEntries <= 0 {
		maxEntries = defaultMemoryCacheMaxEntries
	}
	return &MemoryCacheStore{
		entries:    make(map[string]*memoryCacheEntry),
		maxEntries: maxEntries,
	}
}

func (s *MemoryCacheStore) Get(key string) *RouteInferResult {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return nil
	}
	if !e.expiresAt.After(now) {
		delete(s.entries, key)
		return nil
	}
	return &RouteInferResult{
		Content:          e.content,
		SelectedModelID:  e.selectedModelID,
		SelectedTargetID: e.selectedTargetID,
		ModelDefKey:      e.modelDefKey,
		Provider:         e.provider,
	}
}

func (s *MemoryCacheStore) Set(key string, r *RouteInferResult, ttl time.Duration) {
	if r == nil {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepExpiredLocked(now)
	if _, exists := s.entries[key]; !exists && len(s.entries) >= s.maxEntries {
		s.evictOldestLocked()
	}
	s.nextSeq++
	s.entries[key] = &memoryCacheEntry{
		content:          r.Content,
		selectedModelID:  r.SelectedModelID,
		selectedTargetID: r.SelectedTargetID,
		modelDefKey:      r.ModelDefKey,
		provider:         r.Provider,
		expiresAt:        now.Add(ttl),
		sequence:         s.nextSeq,
	}
}

func (s *MemoryCacheStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
}

func (s *MemoryCacheStore) sweepExpiredLocked(now time.Time) {
	for key, entry := range s.entries {
		if !entry.expiresAt.After(now) {
			delete(s.entries, key)
		}
	}
}

func (s *MemoryCacheStore) evictOldestLocked() {
	var oldestKey string
	var oldestSeq uint64
	for key, entry := range s.entries {
		if oldestKey == "" || entry.sequence < oldestSeq {
			oldestKey = key
			oldestSeq = entry.sequence
		}
	}
	if oldestKey != "" {
		delete(s.entries, oldestKey)
	}
}

func (s *MemoryCacheStore) lenForTest() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// ── Redis backend (distributed) ───────────────────────────────────────────────

// RedisCacheStore uses Redis for distributed caching across multiple instances.
// Only requires the standard library — connects via raw RESP protocol over TCP.
// Set CACHE_BACKEND=redis and CACHE_REDIS_ADDR=host:port to enable.
type RedisCacheStore struct {
	addr   string
	prefix string
}

func NewRedisCacheStore(addr, prefix string) CacheStore {
	return &RedisCacheStore{addr: addr, prefix: prefix}
}

type redisCachePayload struct {
	Content          string `json:"c"`
	SelectedModelID  string `json:"m"`
	SelectedTargetID string `json:"t,omitempty"`
	ModelDefKey      string `json:"d,omitempty"`
	Provider         string `json:"p,omitempty"`
}

func (s *RedisCacheStore) fullKey(k string) string { return s.prefix + ":" + k }

func (s *RedisCacheStore) Get(key string) *RouteInferResult {
	val, err := redisGet(s.addr, s.fullKey(key))
	if err != nil {
		slog.Error("cache GET error", "err", err)
		return nil
	}
	if val == "" {
		return nil
	}
	var p redisCachePayload
	if err := json.Unmarshal([]byte(val), &p); err != nil {
		return nil
	}
	return &RouteInferResult{
		Content:          p.Content,
		SelectedModelID:  p.SelectedModelID,
		SelectedTargetID: p.SelectedTargetID,
		ModelDefKey:      p.ModelDefKey,
		Provider:         p.Provider,
	}
}

func (s *RedisCacheStore) Set(key string, r *RouteInferResult, ttl time.Duration) {
	b, err := json.Marshal(redisCachePayload{
		Content:          r.Content,
		SelectedModelID:  r.SelectedModelID,
		SelectedTargetID: r.SelectedTargetID,
		ModelDefKey:      r.ModelDefKey,
		Provider:         r.Provider,
	})
	if err != nil {
		return
	}
	if err := redisSetEX(s.addr, s.fullKey(key), string(b), int(ttl.Seconds())); err != nil {
		slog.Error("cache SET error", "err", err)
	}
}

func (s *RedisCacheStore) Delete(key string) {
	_ = redisDEL(s.addr, s.fullKey(key))
}

// ── Minimal RESP client (stdlib only) ─────────────────────────────────────────

func redisCmd(addr string, args ...string) (string, error) {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return "", fmt.Errorf("redis dial: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck

	var cmd strings.Builder
	fmt.Fprintf(&cmd, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&cmd, "$%d\r\n%s\r\n", len(a), a)
	}
	if _, err := fmt.Fprint(conn, cmd.String()); err != nil {
		return "", err
	}
	buf := make([]byte, 8192)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	return parseRESP(string(buf[:n]))
}

func parseRESP(resp string) (string, error) {
	if len(resp) == 0 {
		return "", nil
	}
	switch resp[0] {
	case '+': // simple string
		return strings.TrimRight(resp[1:], "\r\n"), nil
	case '$': // bulk string
		if strings.HasPrefix(resp, "$-1") { // nil bulk string — key not found
			return "", nil
		}
		idx := strings.Index(resp, "\r\n")
		if idx < 0 {
			return "", nil
		}
		val := resp[idx+2:]
		return strings.TrimRight(val, "\r\n"), nil
	case '-': // Redis error response
		msg := strings.TrimRight(resp[1:], "\r\n")
		return "", fmt.Errorf("redis: %s", msg)
	case ':': // integer — return as string
		return strings.TrimRight(resp[1:], "\r\n"), nil
	}
	return "", nil
}

func redisGet(addr, key string) (string, error) {
	return redisCmd(addr, "GET", key)
}

func redisSetEX(addr, key, val string, ttlSecs int) error {
	if ttlSecs <= 0 {
		ttlSecs = 60
	}
	_, err := redisCmd(addr, "SET", key, val, "EX", fmt.Sprintf("%d", ttlSecs))
	return err
}

func redisDEL(addr, key string) error {
	_, err := redisCmd(addr, "DEL", key)
	return err
}

// ── Factory ───────────────────────────────────────────────────────────────────

// NewCacheStore reads cache settings from app config and constructs the
// appropriate store. Set CACHE_BACKEND=redis and CACHE_REDIS_ADDR=host:6379.
func NewCacheStore(cfg config.Config) CacheStore {
	if cfg.CacheBackend == "redis" {
		addr := cfg.CacheRedisAddr
		if addr == "" {
			addr = config.DefaultCacheRedisAddr
		}
		prefix := cfg.CacheRedisPrefix
		if prefix == "" {
			prefix = config.DefaultCacheRedisPrefix
		}
		slog.Info("cache backend", "backend", "redis", "addr", addr, "prefix", prefix)
		return NewRedisCacheStore(addr, prefix)
	}
	return NewMemoryCacheStore()
}
