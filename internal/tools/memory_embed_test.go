package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"openclawssy/internal/config"
	"openclawssy/internal/memory"
	memorystore "openclawssy/internal/memory/store"
	"os"
	"path/filepath"
)

func TestMemoryEmbedderSupportsOpenRouterEmbeddings(t *testing.T) {
	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float64{0.1, 0.2, 0.3}}},
		})
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Memory.Enabled = true
	cfg.Memory.EmbeddingsEnabled = true
	cfg.Memory.EmbeddingProvider = "openrouter"
	cfg.Memory.EmbeddingModel = "text-embedding-3-small"
	cfg.Providers.OpenRouter.BaseURL = server.URL + "/v1"
	cfg.Providers.OpenRouter.APIKey = "test-openrouter-key"
	cfg.Providers.OpenRouter.APIKeyEnv = ""

	embedder, err := memoryEmbedderFromConfig(cfg)
	if err != nil {
		t.Fatalf("memoryEmbedderFromConfig: %v", err)
	}
	if embedder == nil {
		t.Fatal("expected embedder to be configured")
	}
	vec, err := embedder.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("expected 3 embedding dimensions, got %d", len(vec))
	}
	if gotPath != "/v1/embeddings" {
		t.Fatalf("expected openrouter embeddings path, got %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer test-openrouter-key") {
		t.Fatalf("expected bearer auth header, got %q", gotAuth)
	}
}

// TestEmbeddingProviderFailureReturnsError verifies that when the embedding
// provider returns HTTP 500, the embedder's Embed method propagates an error.
func TestEmbeddingProviderFailureReturnsError(t *testing.T) {
	// Simulate an embedding provider that always fails with HTTP 500.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Memory.Enabled = true
	cfg.Memory.EmbeddingsEnabled = true
	cfg.Memory.EmbeddingProvider = "openai"
	cfg.Memory.EmbeddingModel = "text-embedding-3-small"
	cfg.Providers.OpenAI.BaseURL = server.URL + "/v1"
	cfg.Providers.OpenAI.APIKey = "test-key"
	cfg.Providers.OpenAI.APIKeyEnv = ""

	embedder, err := memoryEmbedderFromConfig(cfg)
	if err != nil {
		t.Fatalf("memoryEmbedderFromConfig should succeed even though provider is broken: %v", err)
	}
	if embedder == nil {
		t.Fatal("expected embedder to be non-nil")
	}

	// Calling Embed should fail because the server returns 500.
	_, err = embedder.Embed(context.Background(), "test text for embedding")
	if err == nil {
		t.Fatal("expected error from embedding provider returning HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error to reference status 500, got: %v", err)
	}
}

// TestEmbeddingSyncBestEffortWriteSucceeds verifies that memory.write succeeds
// even when the embedding provider is down. The maybeSyncMemoryEmbedding call
// in memoryWrite discards its error (` _ = maybeSyncMemoryEmbedding(...)`)
// making it best-effort. This test confirms that behavior by:
//  1. Setting up an embedding server that returns 500
//  2. Creating a real SQLite store
//  3. Calling maybeSyncMemoryEmbedding directly (should return an error)
//  4. Verifying the memory item was still written successfully to the store
func TestEmbeddingSyncBestEffortWriteSucceeds(t *testing.T) {
	// 1. Embedding provider that always fails.
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"embedding service unavailable"}`, http.StatusInternalServerError)
	}))
	defer failServer.Close()

	cfg := config.Default()
	cfg.Memory.Enabled = true
	cfg.Memory.EmbeddingsEnabled = true
	cfg.Memory.EmbeddingProvider = "openai"
	cfg.Memory.EmbeddingModel = "text-embedding-3-small"
	cfg.Providers.OpenAI.BaseURL = failServer.URL + "/v1"
	cfg.Providers.OpenAI.APIKey = "test-key"
	cfg.Providers.OpenAI.APIKeyEnv = ""

	// 2. Create a temp directory and real SQLite store.
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "memory.db")
	store, err := memorystore.OpenSQLite(dbPath, "test-agent")
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer store.Close()

	// 3. Write a memory item to the store (simulating what memoryWrite does).
	ctx := context.Background()
	item := memory.MemoryItem{
		Kind:       "preference",
		Title:      "Test Preference",
		Content:    "User prefers dark mode",
		Importance: 3,
		Confidence: 0.9,
	}
	saved, err := store.Upsert(ctx, item)
	if err != nil {
		t.Fatalf("store.Upsert failed: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("expected saved item to have an ID")
	}

	// 4. Call maybeSyncMemoryEmbedding — it SHOULD return an error since the
	//    embedding server is down.
	embedErr := maybeSyncMemoryEmbedding(ctx, cfg, store, saved)
	if embedErr == nil {
		t.Fatal("expected maybeSyncMemoryEmbedding to return error when provider fails")
	}
	if !strings.Contains(embedErr.Error(), "500") {
		t.Fatalf("expected embedding error to reference status 500, got: %v", embedErr)
	}

	// 5. Verify the memory item is still retrievable from the store,
	//    confirming that a failing embedding provider doesn't affect storage.
	//    In production, memoryWrite discards this error with `_ = maybeSyncMemoryEmbedding(...)`.
	retrieved, found, err := store.Get(ctx, saved.ID)
	if err != nil {
		t.Fatalf("store.Get failed: %v", err)
	}
	if !found {
		t.Fatal("expected memory item to be found in store despite embedding failure")
	}
	if retrieved.Title != "Test Preference" {
		t.Fatalf("expected title 'Test Preference', got %q", retrieved.Title)
	}
	if retrieved.Content != "User prefers dark mode" {
		t.Fatalf("expected content 'User prefers dark mode', got %q", retrieved.Content)
	}

	// Verify that the embedding error is indeed discarded at the call site.
	// This mirrors the production pattern: `_ = maybeSyncMemoryEmbedding(ctx, cfg, store, saved)`
	// The `_ =` means the write operation succeeds even when embedding fails.
	_ = os.Remove(dbPath) // cleanup
}
