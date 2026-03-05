package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/config"
	"openclawssy/internal/memory"
	memorystore "openclawssy/internal/memory/store"
)

func TestBuildMemoryRecallBlockIncludesRelevantItems(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	dbPath := filepath.Join(root, ".openclawssy", "agents", "default", "memory", "memory.db")
	store, err := memorystore.OpenSQLite(dbPath, "default")
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer func() { _ = store.Close() }()
	_, _ = store.Upsert(context.Background(), memory.MemoryItem{Kind: "preference", Title: "Notifications", Content: "User prefers proactive notifications.", Importance: 4, Confidence: 0.9, UpdatedAt: time.Now().UTC()})
	_, _ = store.Upsert(context.Background(), memory.MemoryItem{Kind: "issue", Title: "Tool failure", Content: "Recent timeout in network call.", Importance: 3, Confidence: 0.8, UpdatedAt: time.Now().UTC().Add(-2 * time.Hour)})

	cfg := config.Default()
	cfg.Memory.Enabled = true
	cfg.Memory.MaxPromptTokens = 200
	cfg.Memory.MaxWorkingItems = 10

	block, err := e.buildMemoryRecallBlock(context.Background(), cfg, "default", "please keep proactive notifications", []agent.ChatMessage{{Role: "user", Content: "I prefer proactive notifications"}})
	if err != nil {
		t.Fatalf("build memory recall block: %v", err)
	}
	if !strings.Contains(block, "RELEVANT MEMORY") {
		t.Fatalf("expected recall header, got %q", block)
	}
	if !strings.Contains(block, "proactive notifications") {
		t.Fatalf("expected relevant memory content, got %q", block)
	}
}

func TestBuildMemoryRecallBlockRespectsSizeCap(t *testing.T) {
	items := []memory.MemoryItem{{ID: "mem_123456789", Content: strings.Repeat("x", 500), Importance: 5, UpdatedAt: time.Now().UTC()}}
	block := formatRecallBlock(items, 80)
	if len(block) > 80 {
		t.Fatalf("expected block length <= 80, got %d", len(block))
	}
}

func TestBuildMemoryRecallBlockReturnsEmptyWhenDisabled(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	cfg := config.Default()
	cfg.Memory.Enabled = false

	block, err := e.buildMemoryRecallBlock(context.Background(), cfg, "default", "test query", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block != "" {
		t.Fatalf("expected empty block when memory disabled, got %q", block)
	}
}

func TestFormatRecallBlockSortsImportanceThenRecency(t *testing.T) {
	now := time.Now().UTC()
	items := []memory.MemoryItem{
		{ID: "low_imp", Content: "low importance item", Importance: 1, UpdatedAt: now},
		{ID: "high_imp", Content: "high importance item", Importance: 5, UpdatedAt: now.Add(-time.Hour)},
		{ID: "med_recent", Content: "medium recent item", Importance: 3, UpdatedAt: now},
	}
	block := formatRecallBlock(items, 10000)
	// High importance should come first even though it's older.
	highIdx := strings.Index(block, "high importance")
	lowIdx := strings.Index(block, "low importance")
	if highIdx < 0 || lowIdx < 0 {
		t.Fatalf("expected both items in block, got %q", block)
	}
	if highIdx > lowIdx {
		t.Fatalf("expected high importance item before low importance item in recall block")
	}
}

func TestFormatRecallBlockTruncatesLargeItems(t *testing.T) {
	now := time.Now().UTC()
	// Single item with very long content — should be truncated at 420 chars per line.
	items := []memory.MemoryItem{
		{ID: "long_item", Content: strings.Repeat("a", 600), Importance: 5, UpdatedAt: now},
	}
	block := formatRecallBlock(items, 10000)
	// The block should not contain the full 600 chars worth of 'a'.
	if strings.Count(block, "a") > 420 {
		t.Fatalf("expected content truncation, got %d 'a' chars", strings.Count(block, "a"))
	}
}

func TestFormatRecallBlockEmptyItems(t *testing.T) {
	block := formatRecallBlock(nil, 10000)
	if block != "" {
		t.Fatalf("expected empty block for nil items, got %q", block)
	}
}

func TestBuildMemoryRecallBlockMaxPromptTokensLimit(t *testing.T) {
	root := t.TempDir()
	e, err := NewEngine(root)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	dbPath := filepath.Join(root, ".openclawssy", "agents", "default", "memory", "memory.db")
	store, err := memorystore.OpenSQLite(dbPath, "default")
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Insert many items to build a large recall block.
	for i := 0; i < 20; i++ {
		_, _ = store.Upsert(context.Background(), memory.MemoryItem{
			Kind:       "note",
			Title:      "Item",
			Content:    strings.Repeat("word ", 50),
			Importance: 5,
			Confidence: 0.9,
			UpdatedAt:  time.Now().UTC(),
		})
	}

	cfg := config.Default()
	cfg.Memory.Enabled = true
	cfg.Memory.MaxPromptTokens = 50 // very small — 50 * 4 = 200 chars max
	cfg.Memory.MaxWorkingItems = 20

	block, err := e.buildMemoryRecallBlock(context.Background(), cfg, "default", "test query", []agent.ChatMessage{{Role: "user", Content: "test"}})
	if err != nil {
		t.Fatalf("build memory recall block: %v", err)
	}
	maxChars := 50 * 4
	if len(block) > maxChars {
		t.Fatalf("expected block length <= %d chars, got %d", maxChars, len(block))
	}
}
