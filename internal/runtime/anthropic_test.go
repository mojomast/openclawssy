package runtime

import (
	"testing"

	"openclawssy/internal/agent"
)

// TestBuildAnthropicMessages_SystemExcluded verifies system messages are dropped.
func TestBuildAnthropicMessages_SystemExcluded(t *testing.T) {
	messages := []agent.ChatMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello"},
	}
	result := buildAnthropicMessages(messages)
	for _, m := range result {
		if role, _ := m["role"].(string); role == "system" {
			t.Error("system message should not appear in Anthropic messages")
		}
	}
}

// TestBuildAnthropicMessages_ToolMappedToUser verifies "tool" role → "user".
func TestBuildAnthropicMessages_ToolMappedToUser(t *testing.T) {
	messages := []agent.ChatMessage{
		{Role: "user", Content: "Use the tool"},
		{Role: "assistant", Content: "Calling tool..."},
		{Role: "tool", Content: "Tool result"},
	}
	result := buildAnthropicMessages(messages)
	// tool should be merged into the previous user or added as user
	for _, m := range result {
		if role, _ := m["role"].(string); role == "tool" {
			t.Error("tool role should be remapped; found 'tool' in output")
		}
	}
}

// TestBuildAnthropicMessages_FirstMessageIsUser verifies alternation fix.
func TestBuildAnthropicMessages_FirstMessageIsUser(t *testing.T) {
	messages := []agent.ChatMessage{
		{Role: "assistant", Content: "I am the assistant"},
		{Role: "user", Content: "Hello"},
	}
	result := buildAnthropicMessages(messages)
	if len(result) == 0 {
		t.Fatal("expected messages, got none")
	}
	firstRole, _ := result[0]["role"].(string)
	if firstRole != "user" {
		t.Errorf("expected first message role to be 'user', got %q", firstRole)
	}
}

// TestMergeAdjacentAnthropicMessages_SameRoleMerged verifies adjacent same-role merging.
func TestMergeAdjacentAnthropicMessages_SameRoleMerged(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "First"},
		{"role": "user", "content": "Second"},
		{"role": "assistant", "content": "Reply"},
	}
	merged := mergeAdjacentAnthropicMessages(messages)
	// Should have 2: merged user + assistant
	if len(merged) != 2 {
		t.Fatalf("expected 2 messages after merging, got %d", len(merged))
	}
	content, _ := merged[0]["content"].(string)
	if content == "" {
		t.Error("merged content should not be empty")
	}
}

// TestMergeAdjacentAnthropicMessages_DifferentRolePreserved verifies different roles are kept.
func TestMergeAdjacentAnthropicMessages_DifferentRolePreserved(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "assistant", "content": "Hi"},
		{"role": "user", "content": "How are you?"},
	}
	merged := mergeAdjacentAnthropicMessages(messages)
	if len(merged) != 3 {
		t.Fatalf("expected 3 messages (no merging needed), got %d", len(merged))
	}
}

// TestBuildAnthropicTools_InputSchema verifies Anthropic uses input_schema not parameters.
func TestBuildAnthropicTools_InputSchema(t *testing.T) {
	schemas := []agent.ToolSchema{
		{
			Name:        "fs.read",
			Description: "Read a file",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
			},
		},
	}
	tools := buildAnthropicTools(schemas, false)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0]
	if tool["parameters"] != nil {
		t.Error("Anthropic tools should use 'input_schema', not 'parameters'")
	}
	if tool["input_schema"] == nil {
		t.Error("expected 'input_schema' field to be set")
	}
}

// TestBuildAnthropicTools_NilParamsGetsDefault verifies nil parameters get a default schema.
func TestBuildAnthropicTools_NilParamsGetsDefault(t *testing.T) {
	schemas := []agent.ToolSchema{
		{Name: "agent.list", Description: "List agents"},
	}
	tools := buildAnthropicTools(schemas, false)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool")
	}
	if tools[0]["input_schema"] == nil {
		t.Error("nil parameters should produce a default input_schema")
	}
}

// TestBuildAnthropicTools_OAuthMapsToClaudeCodeNames verifies CC tool naming for OAuth.
func TestBuildAnthropicTools_OAuthMapsToClaudeCodeNames(t *testing.T) {
	schemas := []agent.ToolSchema{
		{Name: "read", Description: "Read file"},
		{Name: "write", Description: "Write file"},
		{Name: "bash", Description: "Run command"},
		{Name: "agent.list", Description: "List agents"},
	}
	tools := buildAnthropicTools(schemas, true)
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	expected := []string{"Read", "Write", "Bash", "agent.list"}
	for i, want := range expected {
		got, _ := tools[i]["name"].(string)
		if got != want {
			t.Errorf("tool[%d] name: want %q, got %q", i, want, got)
		}
	}
}

// TestBuildAnthropicTools_NonOAuthKeepsOriginalNames verifies no CC mapping without OAuth.
func TestBuildAnthropicTools_NonOAuthKeepsOriginalNames(t *testing.T) {
	schemas := []agent.ToolSchema{
		{Name: "read", Description: "Read file"},
		{Name: "bash", Description: "Run command"},
	}
	tools := buildAnthropicTools(schemas, false)
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		if name == "Read" || name == "Bash" {
			t.Errorf("non-OAuth tools should not get CC names, got %q", name)
		}
	}
}

func TestToClaudeCodeName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"read", "Read"},
		{"Read", "Read"},
		{"READ", "Read"},
		{"write", "Write"},
		{"bash", "Bash"},
		{"grep", "Grep"},
		{"websearch", "WebSearch"},
		{"webfetch", "WebFetch"},
		{"agent.list", "agent.list"}, // not a CC tool
		{"fs.read", "fs.read"},       // not a CC tool
		{"todowrite", "TodoWrite"},   // CC canonical
		{"askuserquestion", "AskUserQuestion"},
	}
	for _, tc := range cases {
		got := toClaudeCodeName(tc.in)
		if got != tc.want {
			t.Errorf("toClaudeCodeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAnthropicModelSupportsAdaptiveThinking(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"claude-opus-4-6", true},
		{"claude-opus-4.6", true},
		{"claude-sonnet-4-6", true},
		{"claude-sonnet-4.6", true},
		{"claude-sonnet-4-20250514", false},
		{"claude-opus-4-20250514", false},
		{"claude-3-haiku-20240307", false},
	}
	for _, tc := range cases {
		got := anthropicModelSupportsAdaptiveThinking(tc.model)
		if got != tc.want {
			t.Errorf("anthropicModelSupportsAdaptiveThinking(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestParseAnthropicResponse_ThinkingBlocks(t *testing.T) {
	result := anthropicResponseBody{
		Content: []anthropicContentBlock{
			{Type: "thinking", Thinking: "Let me reason about this..."},
			{Type: "text", Text: "The answer is 42."},
		},
	}
	resp, err := parseAnthropicResponse(result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FinalText != "The answer is 42." {
		t.Errorf("expected final text, got %q", resp.FinalText)
	}
	if !resp.ThinkingPresent {
		t.Error("expected thinking to be present")
	}
	if resp.Thinking != "Let me reason about this..." {
		t.Errorf("expected thinking content, got %q", resp.Thinking)
	}
}

func TestBuildAnthropicSystemPayload_OAuthIncludesClaudeCodeIdentity(t *testing.T) {
	payload, ok := buildAnthropicSystemPayload("Follow the repo instructions.", true)
	if !ok {
		t.Fatal("expected system payload")
	}
	blocks, ok := payload.([]map[string]string)
	if !ok {
		t.Fatalf("expected oauth system payload blocks, got %#v", payload)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 system blocks, got %d", len(blocks))
	}
	if blocks[0]["text"] != "You are Claude Code, Anthropic's official CLI for Claude." {
		t.Fatalf("unexpected identity block: %#v", blocks[0])
	}
	if blocks[1]["text"] != "Follow the repo instructions." {
		t.Fatalf("unexpected appended system block: %#v", blocks[1])
	}
}

func TestBuildAnthropicSystemPayload_NonOAuthRemainsString(t *testing.T) {
	payload, ok := buildAnthropicSystemPayload("Follow the repo instructions.", false)
	if !ok {
		t.Fatal("expected system payload")
	}
	text, ok := payload.(string)
	if !ok {
		t.Fatalf("expected plain string payload, got %#v", payload)
	}
	if text != "Follow the repo instructions." {
		t.Fatalf("unexpected system text %q", text)
	}
}

// TestParseAnthropicResponse_TextExtraction verifies text content blocks are assembled.
func TestParseAnthropicResponse_TextExtraction(t *testing.T) {
	result := anthropicResponseBody{
		Content: []anthropicContentBlock{
			{Type: "text", Text: "Hello from Anthropic!"},
		},
	}
	resp, err := parseAnthropicResponse(result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FinalText != "Hello from Anthropic!" {
		t.Errorf("expected 'Hello from Anthropic!', got %q", resp.FinalText)
	}
}

// TestParseAnthropicResponse_ToolUse verifies tool_use blocks produce ToolCalls.
func TestParseAnthropicResponse_ToolUse(t *testing.T) {
	result := anthropicResponseBody{
		Content: []anthropicContentBlock{
			{
				Type:  "tool_use",
				ID:    "toolu_01",
				Name:  "fs__read",
				Input: map[string]any{"path": "/tmp/test.txt"},
			},
		},
	}
	resp, err := parseAnthropicResponse(result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) == 0 {
		t.Fatal("expected tool calls, got none")
	}
}

// TestParseAnthropicResponse_ErrorField verifies error responses are surfaced.
func TestParseAnthropicResponse_ErrorField(t *testing.T) {
	result := anthropicResponseBody{
		Error: &anthropicError{
			Type:    "authentication_error",
			Message: "Invalid API key",
		},
	}
	_, err := parseAnthropicResponse(result, nil)
	if err == nil {
		t.Fatal("expected error from anthropic error field, got nil")
	}
}

// TestParseAnthropicResponse_EmptyContent verifies an error for empty responses.
func TestParseAnthropicResponse_EmptyContent(t *testing.T) {
	result := anthropicResponseBody{
		Content: []anthropicContentBlock{},
	}
	_, err := parseAnthropicResponse(result, nil)
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
}
