package runtime

import (
	"testing"

	"openclawssy/internal/agent"
)

// TestBuildCodexInput_SystemExcluded verifies that system messages are dropped.
func TestBuildCodexInput_SystemExcluded(t *testing.T) {
	messages := []agent.ChatMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}
	input := buildCodexInput(messages)
	if len(input) != 2 {
		t.Fatalf("expected 2 input entries, got %d", len(input))
	}
	for _, entry := range input {
		if role, _ := entry["role"].(string); role == "system" {
			t.Error("system message should have been excluded from codex input")
		}
	}
}

// TestBuildCodexInput_ToolMappedToUser verifies that "tool" role is remapped to "user".
func TestBuildCodexInput_ToolMappedToUser(t *testing.T) {
	messages := []agent.ChatMessage{
		{Role: "user", Content: "Run the tool"},
		{Role: "tool", Content: "Tool result here"},
	}
	input := buildCodexInput(messages)
	if len(input) != 2 {
		t.Fatalf("expected 2 input entries, got %d", len(input))
	}
	role, _ := input[1]["role"].(string)
	if role != "user" {
		t.Errorf("expected tool role to be remapped to 'user', got %q", role)
	}
}

// TestBuildCodexInput_EmptyContentSkipped verifies that empty messages are omitted.
func TestBuildCodexInput_EmptyContentSkipped(t *testing.T) {
	messages := []agent.ChatMessage{
		{Role: "user", Content: ""},
		{Role: "user", Content: "   "},
		{Role: "user", Content: "Hello"},
	}
	input := buildCodexInput(messages)
	if len(input) != 1 {
		t.Fatalf("expected 1 non-empty input entry, got %d", len(input))
	}
}

// TestBuildCodexTools_CorrectFormat verifies the function/type structure.
func TestBuildCodexTools_CorrectFormat(t *testing.T) {
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
	tools := buildCodexTools(schemas)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0]
	if toolType, _ := tool["type"].(string); toolType != "function" {
		t.Errorf("expected type=function, got %q", toolType)
	}
	fn, ok := tool["function"].(map[string]any)
	if !ok {
		t.Fatal("expected 'function' field to be a map")
	}
	if fn["name"] == nil {
		t.Error("expected function name to be set")
	}
	if fn["description"] == nil {
		t.Error("expected function description to be set")
	}
	if fn["parameters"] == nil {
		t.Error("expected function parameters to be set")
	}
}

// TestBuildCodexTools_NilParametersOmitted verifies nil params are handled.
func TestBuildCodexTools_NilParametersOmitted(t *testing.T) {
	schemas := []agent.ToolSchema{
		{Name: "agent.list", Description: "List agents"},
	}
	tools := buildCodexTools(schemas)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool")
	}
	fn, _ := tools[0]["function"].(map[string]any)
	if fn["parameters"] != nil {
		t.Error("expected parameters to be absent when schema.Parameters is nil")
	}
}

// TestParseCodexResponse_TextExtraction verifies text is extracted from output.
func TestParseCodexResponse_TextExtraction(t *testing.T) {
	result := codexResponseBody{
		Status: "completed",
		Output: []providerResponseOutputItem{
			{
				Type: "message",
				Content: []providerResponseContentPart{
					{Type: "output_text", Text: "Hello, world!"},
				},
			},
		},
	}
	resp, err := parseCodexResponse(result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FinalText != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", resp.FinalText)
	}
}

// TestParseCodexResponse_OutputTextFallback verifies the output_text field is used as fallback.
func TestParseCodexResponse_OutputTextFallback(t *testing.T) {
	result := codexResponseBody{
		Status:     "completed",
		OutputText: "Fallback text",
		Output:     []providerResponseOutputItem{},
	}
	resp, err := parseCodexResponse(result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FinalText != "Fallback text" {
		t.Errorf("expected 'Fallback text', got %q", resp.FinalText)
	}
}

// TestParseCodexResponse_FailedStatus verifies an error is returned for failed status.
func TestParseCodexResponse_FailedStatus(t *testing.T) {
	result := codexResponseBody{
		Status: "failed",
		Error:  "something went wrong",
	}
	_, err := parseCodexResponse(result, nil)
	if err == nil {
		t.Fatal("expected error for failed status, got nil")
	}
}

// TestParseCodexResponse_EmptyContent verifies an error is returned when there's no content.
func TestParseCodexResponse_EmptyContent(t *testing.T) {
	result := codexResponseBody{
		Status: "completed",
		Output: []providerResponseOutputItem{},
	}
	_, err := parseCodexResponse(result, nil)
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
}

// TestParseCodexResponse_FunctionCall verifies tool calls are extracted from output.
func TestParseCodexResponse_FunctionCall(t *testing.T) {
	result := codexResponseBody{
		Status: "completed",
		Output: []providerResponseOutputItem{
			{
				ID:        "call_abc",
				Type:      "function_call",
				Name:      "fs__read",
				Arguments: `{"path":"/tmp/test.txt"}`,
			},
		},
	}
	resp, err := parseCodexResponse(result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) == 0 {
		t.Fatal("expected tool calls, got none")
	}
}
