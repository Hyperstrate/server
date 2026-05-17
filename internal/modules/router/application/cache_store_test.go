package application

import (
	"testing"
	"time"
)

func TestMemoryCacheStoreCompactsExpiredEntriesOnSet(t *testing.T) {
	store := NewMemoryCacheStore().(*MemoryCacheStore)

	store.Set("expired", &RouteInferResult{Content: "old"}, -time.Second)
	store.Set("fresh", &RouteInferResult{Content: "new"}, time.Hour)

	if got := store.lenForTest(); got != 1 {
		t.Fatalf("cache size = %d, want 1 live entry", got)
	}
	if hit := store.Get("fresh"); hit == nil || hit.Content != "new" {
		t.Fatalf("fresh entry missing after compaction: %+v", hit)
	}
}

func TestMemoryCacheStoreEvictsOldestEntryWhenFull(t *testing.T) {
	store := NewMemoryCacheStoreWithMaxEntries(2).(*MemoryCacheStore)

	store.Set("first", &RouteInferResult{Content: "1"}, time.Hour)
	store.Set("second", &RouteInferResult{Content: "2"}, time.Hour)
	store.Set("third", &RouteInferResult{Content: "3"}, time.Hour)

	if got := store.lenForTest(); got != 2 {
		t.Fatalf("cache size = %d, want 2", got)
	}
	if hit := store.Get("first"); hit != nil {
		t.Fatalf("oldest entry was not evicted: %+v", hit)
	}
	if hit := store.Get("second"); hit == nil || hit.Content != "2" {
		t.Fatalf("second entry missing: %+v", hit)
	}
	if hit := store.Get("third"); hit == nil || hit.Content != "3" {
		t.Fatalf("third entry missing: %+v", hit)
	}
}
