package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"openclawssy/internal/config"
	"openclawssy/internal/memory"
)

func TestParseStrictCheckpointJSONAcceptsValidSchema(t *testing.T) {
	raw := `{"new_items":[{"kind":"preference","title":"Tone","content":"Be concise","importance":4,"confidence":0.9}],"updates":[{"id":"mem_1","new_content":"Updated content","confidence":0.8}]}`
	out, err := parseStrictCheckpointJSON(raw)
	if err != nil {
		t.Fatalf("parse strict checkpoint json: %v", err)
	}
	if len(out.NewItems) != 1 || len(out.Updates) != 1 {
		t.Fatalf("unexpected parsed output: %+v", out)
	}
}

func TestParseStrictCheckpointJSONRejectsUnknownFields(t *testing.T) {
	raw := `{"new_items":[{"kind":"preference","title":"Tone","content":"Be concise","importance":4,"confidence":0.9,"extra":true}],"updates":[]}`
	if _, err := parseStrictCheckpointJSON(raw); err == nil {
		t.Fatal("expected strict parser to reject unknown field")
	}
}

func TestParseStrictCheckpointJSONRejectsMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"missing kind", `{"new_items":[{"title":"T","content":"C","importance":3,"confidence":0.8}],"updates":[]}`},
		{"missing title", `{"new_items":[{"kind":"note","content":"C","importance":3,"confidence":0.8}],"updates":[]}`},
		{"missing content", `{"new_items":[{"kind":"note","title":"T","importance":3,"confidence":0.8}],"updates":[]}`},
		{"missing update id", `{"new_items":[],"updates":[{"new_content":"C","confidence":0.8}]}`},
		{"missing update content", `{"new_items":[],"updates":[{"id":"mem_1","confidence":0.8}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseStrictCheckpointJSON(tt.raw); err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}

func TestParseStrictCheckpointJSONRejectsOutOfRangeValues(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"importance 0", `{"new_items":[{"kind":"note","title":"T","content":"C","importance":0,"confidence":0.8}],"updates":[]}`},
		{"importance 6", `{"new_items":[{"kind":"note","title":"T","content":"C","importance":6,"confidence":0.8}],"updates":[]}`},
		{"confidence -0.1", `{"new_items":[{"kind":"note","title":"T","content":"C","importance":3,"confidence":-0.1}],"updates":[]}`},
		{"confidence 1.1", `{"new_items":[{"kind":"note","title":"T","content":"C","importance":3,"confidence":1.1}],"updates":[]}`},
		{"update confidence -0.5", `{"new_items":[],"updates":[{"id":"mem_1","new_content":"C","confidence":-0.5}]}`},
		{"update confidence 2.0", `{"new_items":[],"updates":[{"id":"mem_1","new_content":"C","confidence":2.0}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseStrictCheckpointJSON(tt.raw); err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}

func TestParseStrictCheckpointJSONAcceptsEmptyLists(t *testing.T) {
	raw := `{"new_items":[],"updates":[]}`
	out, err := parseStrictCheckpointJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.NewItems) != 0 || len(out.Updates) != 0 {
		t.Fatalf("expected empty lists, got %+v", out)
	}
}

func TestParseStrictCheckpointJSONHandlesCodeFence(t *testing.T) {
	raw := "```json\n{\"new_items\":[{\"kind\":\"note\",\"title\":\"T\",\"content\":\"C\",\"importance\":3,\"confidence\":0.8}],\"updates\":[]}\n```"
	out, err := parseStrictCheckpointJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.NewItems) != 1 {
		t.Fatalf("expected 1 new item, got %d", len(out.NewItems))
	}
}

func TestParseStrictCheckpointJSONRejectsToolarge(t *testing.T) {
	// Build a payload with 201 new_items
	items := "["
	for i := 0; i < 201; i++ {
		if i > 0 {
			items += ","
		}
		items += `{"kind":"note","title":"T","content":"C","importance":3,"confidence":0.8}`
	}
	items += "]"
	raw := `{"new_items":` + items + `,"updates":[]}`
	if _, err := parseStrictCheckpointJSON(raw); err == nil {
		t.Fatal("expected error for too-large output")
	}
}

func TestExtractJSONObjectFromWrappedText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"plain JSON", `{"a":1}`, `{"a":1}`},
		{"text before", `Here is the result: {"a":1}`, `{"a":1}`},
		{"code fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"no JSON", "no json here", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONObject(tt.raw)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDistillCheckpointItemsDeterministicFallback(t *testing.T) {
	events := []memory.Event{
		{Type: memory.EventTypeDecisionLog, Text: "keep retries bounded", Metadata: map[string]any{"title": "Retry policy"}},
		{Type: memory.EventTypeError, Text: "connection timeout to api.example.com"},
		{Type: memory.EventTypeUserMessage, Text: "I prefer concise responses always"},
		{Type: memory.EventTypeToolCall, Text: "shell.exec ran"},
	}
	items := distillCheckpointItems(events)
	if len(items) == 0 {
		t.Fatal("expected deterministic fallback to produce items")
	}
	foundDecision := false
	foundError := false
	foundPref := false
	for _, item := range items {
		switch item.Kind {
		case "decision":
			foundDecision = true
			if item.Title != "Retry policy" {
				t.Fatalf("expected decision title 'Retry policy', got %q", item.Title)
			}
		case "issue":
			foundError = true
		case "preference":
			foundPref = true
		}
	}
	if !foundDecision {
		t.Fatal("expected decision item from decision_log event")
	}
	if !foundError {
		t.Fatal("expected issue item from error event")
	}
	if !foundPref {
		t.Fatal("expected preference item from user_message with preference marker")
	}
}

func TestDistillCheckpointItemsFallbackSummary(t *testing.T) {
	// Only tool_call events — no decisions, errors, or preferences.
	events := []memory.Event{
		{Type: memory.EventTypeToolCall, Text: "fs.read ran"},
		{Type: memory.EventTypeToolResult, Text: "ok"},
	}
	items := distillCheckpointItems(events)
	if len(items) != 1 {
		t.Fatalf("expected 1 summary item, got %d", len(items))
	}
	if items[0].Kind != "summary" {
		t.Fatalf("expected summary kind, got %q", items[0].Kind)
	}
}

func TestDistillCheckpointItemsEmptyInput(t *testing.T) {
	items := distillCheckpointItems(nil)
	if items != nil {
		t.Fatalf("expected nil for empty input, got %+v", items)
	}
}

// TestDistillCheckpointWithModelMock creates an httptest.Server that returns
// a valid chat completion response containing checkpoint JSON. It configures
// a config.Config pointing at the mock server, then calls
// distillCheckpointWithModel and verifies correct parsing.
func TestDistillCheckpointWithModelMock(t *testing.T) {
	// Build a valid checkpoint JSON response the "model" would return.
	checkpointJSON := `{"new_items":[{"kind":"preference","title":"Response Style","content":"User prefers concise answers","importance":4,"confidence":0.92}],"updates":[{"id":"mem_existing_1","new_content":"Updated: user confirmed dark mode preference","confidence":0.88}]}`

	// Mock chat completions server returning the checkpoint JSON inside a
	// standard OpenAI-compatible chat completion response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request was properly formed.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected /v1/chat/completions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-distill-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content type: %s", r.Header.Get("Content-Type"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": checkpointJSON,
					},
				},
			},
		})
	}))
	defer server.Close()

	// Configure to point at the mock server using the "openai" provider.
	cfg := config.Default()
	cfg.Model.Provider = "openai"
	cfg.Model.Name = "gpt-4"
	cfg.Providers.OpenAI.BaseURL = server.URL + "/v1"
	cfg.Providers.OpenAI.APIKey = "test-distill-key"
	cfg.Providers.OpenAI.APIKeyEnv = ""

	// Build some test events to distill.
	events := []memory.Event{
		{
			ID:        "evt_1",
			Type:      memory.EventTypeUserMessage,
			Text:      "I prefer concise responses always",
			Timestamp: time.Now().UTC(),
		},
		{
			ID:        "evt_2",
			Type:      memory.EventTypeAssistantOutput,
			Text:      "Understood, I will keep responses brief.",
			Timestamp: time.Now().UTC(),
		},
	}

	ctx := context.Background()
	result, err := distillCheckpointWithModel(ctx, cfg, events)
	if err != nil {
		t.Fatalf("distillCheckpointWithModel failed: %v", err)
	}

	// Verify new_items parsed correctly.
	if len(result.NewItems) != 1 {
		t.Fatalf("expected 1 new item, got %d", len(result.NewItems))
	}
	item := result.NewItems[0]
	if item.Kind != "preference" {
		t.Fatalf("expected kind 'preference', got %q", item.Kind)
	}
	if item.Title != "Response Style" {
		t.Fatalf("expected title 'Response Style', got %q", item.Title)
	}
	if item.Content != "User prefers concise answers" {
		t.Fatalf("expected content 'User prefers concise answers', got %q", item.Content)
	}
	if item.Importance != 4 {
		t.Fatalf("expected importance 4, got %d", item.Importance)
	}
	if item.Confidence != 0.92 {
		t.Fatalf("expected confidence 0.92, got %f", item.Confidence)
	}

	// Verify updates parsed correctly.
	if len(result.Updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(result.Updates))
	}
	upd := result.Updates[0]
	if upd.ID != "mem_existing_1" {
		t.Fatalf("expected update ID 'mem_existing_1', got %q", upd.ID)
	}
	if upd.NewContent != "Updated: user confirmed dark mode preference" {
		t.Fatalf("expected update content, got %q", upd.NewContent)
	}
	if upd.Confidence != 0.88 {
		t.Fatalf("expected update confidence 0.88, got %f", upd.Confidence)
	}
}
