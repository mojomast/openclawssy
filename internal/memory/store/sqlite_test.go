package store

import (
	"context"
	"path/filepath"
	"testing"

	"openclawssy/internal/memory"
)

func TestSQLiteStoreCRUDAndSearch(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.db")

	store, err := OpenSQLite(dbPath, "default")
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer func() { _ = store.Close() }()

	item, err := store.Upsert(ctx, memory.MemoryItem{
		Kind:       "preference",
		Title:      "Notification preference",
		Content:    "User prefers proactive notifications.",
		Importance: 4,
		Confidence: 0.9,
	})
	if err != nil {
		t.Fatalf("upsert item: %v", err)
	}

	results, err := store.Search(ctx, memory.SearchParams{Query: "proactive", Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].ID != item.ID {
		t.Fatalf("expected item id %q, got %q", item.ID, results[0].ID)
	}

	updated, err := store.Update(ctx, memory.MemoryItem{
		ID:         item.ID,
		Kind:       "preference",
		Title:      "Notification preference",
		Content:    "User prefers weekly proactive notifications.",
		Importance: 5,
		Confidence: 0.95,
		Status:     memory.MemoryStatusActive,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Importance != 5 {
		t.Fatalf("expected updated importance=5, got %d", updated.Importance)
	}

	forgotten, err := store.Forget(ctx, item.ID)
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if !forgotten {
		t.Fatal("expected forgotten=true")
	}

	results, err = store.Search(ctx, memory.SearchParams{Query: "weekly", Limit: 5})
	if err != nil {
		t.Fatalf("search after forget: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no active results after forget, got %d", len(results))
	}

	health, err := store.Health(ctx)
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if health.ForgottenItems != 1 {
		t.Fatalf("expected forgotten count=1, got %d", health.ForgottenItems)
	}
}

func TestSQLiteStoreArchiveListAndVacuum(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.db")

	store, err := OpenSQLite(dbPath, "default")
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer func() { _ = store.Close() }()

	item, err := store.Upsert(ctx, memory.MemoryItem{Kind: "note", Title: "T", Content: "C", Importance: 3, Confidence: 0.8})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	active, err := store.List(ctx, memory.MemoryStatusActive, 10)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected one active item, got %d", len(active))
	}
	ok, err := store.Archive(ctx, item.ID)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !ok {
		t.Fatal("expected archive success")
	}
	archived, err := store.List(ctx, memory.MemoryStatusArchived, 10)
	if err != nil {
		t.Fatalf("list archived: %v", err)
	}
	if len(archived) != 1 {
		t.Fatalf("expected one archived item, got %d", len(archived))
	}
	if err := store.Vacuum(ctx); err != nil {
		t.Fatalf("vacuum: %v", err)
	}
}

func TestSQLiteStoreSearchByEmbedding(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "memory.db"), "default")
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer func() { _ = store.Close() }()

	itemA, err := store.Upsert(ctx, memory.MemoryItem{Kind: "note", Title: "A", Content: "alpha", Importance: 4, Confidence: 0.9})
	if err != nil {
		t.Fatalf("upsert A: %v", err)
	}
	itemB, err := store.Upsert(ctx, memory.MemoryItem{Kind: "note", Title: "B", Content: "beta", Importance: 4, Confidence: 0.9})
	if err != nil {
		t.Fatalf("upsert B: %v", err)
	}
	if err := store.UpsertEmbedding(ctx, itemA.ID, "test-emb", []float32{1, 0}); err != nil {
		t.Fatalf("upsert embedding A: %v", err)
	}
	if err := store.UpsertEmbedding(ctx, itemB.ID, "test-emb", []float32{0, 1}); err != nil {
		t.Fatalf("upsert embedding B: %v", err)
	}

	results, err := store.SearchByEmbedding(ctx, []float32{0.9, 0.1}, 5, 1, memory.MemoryStatusActive)
	if err != nil {
		t.Fatalf("search by embedding: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected embedding search results")
	}
	if results[0].ID != itemA.ID {
		t.Fatalf("expected item A to rank first, got %q", results[0].ID)
	}
}

func TestSQLiteStoreSearchFallsBackToSubstringMatch(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "memory.db"), "default")
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer func() { _ = store.Close() }()

	item, err := store.Upsert(ctx, memory.MemoryItem{
		Kind:       "note",
		Title:      "Ussyverse research",
		Content:    "OpenClawssy becomes a node inside the ussyverse knowledge graph.",
		Importance: 4,
		Confidence: 0.9,
	})
	if err != nil {
		t.Fatalf("upsert item: %v", err)
	}

	results, err := store.Search(ctx, memory.SearchParams{Query: "ussy", Limit: 5})
	if err != nil {
		t.Fatalf("search substring: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result for substring query, got %d", len(results))
	}
	if results[0].ID != item.ID {
		t.Fatalf("expected item id %q, got %q", item.ID, results[0].ID)
	}

	results, err = store.Search(ctx, memory.SearchParams{Query: "openclaw", Limit: 5})
	if err != nil {
		t.Fatalf("search substring openclaw: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected substring search to match OpenClawssy, got %d results", len(results))
	}
}
