package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/config"
)

type requestCapture struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	Stream    bool   `json:"stream,omitempty"`
	Messages  []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type staticToolExecutor struct {
	result agent.ToolCallResult
}

func (s *staticToolExecutor) Execute(_ context.Context, call agent.ToolCallRequest) (agent.ToolCallResult, error) {
	res := s.result
	if res.ID == "" {
		res.ID = call.ID
	}
	return res, nil
}

func TestProviderModelRoutesToolResultsBackThroughModel(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []requestCapture
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var payload requestCapture
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		mu.Lock()
		requests = append(requests, payload)
		callNum := len(requests)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch callNum {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{
					map[string]any{"message": map[string]string{
						"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```",
					}},
				},
			})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{
					map[string]any{"message": map[string]string{"content": "There is one file in the folder."}},
				},
			})
		default:
			t.Fatalf("unexpected extra provider call: %d", callNum)
		}
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	runner := agent.Runner{
		Model:             model,
		ToolExecutor:      &staticToolExecutor{result: agent.ToolCallResult{Output: `{"entries":["README.md"]}`}},
		MaxToolIterations: 4,
	}

	out, err := runner.Run(context.Background(), agent.RunInput{
		Message:      "what is in this folder?",
		ArtifactDocs: []agent.ArtifactDoc{{Name: "SOUL.md", Content: "help the user"}},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if out.FinalText != "There is one file in the folder." {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(out.ToolCalls))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("expected 2 provider requests, got %d", len(requests))
	}
	if len(requests[1].Messages) < 1 {
		t.Fatalf("expected second request messages")
	}
	systemPrompt := requests[1].Messages[0].Content
	if !strings.Contains(systemPrompt, "## Tool Results") {
		t.Fatalf("expected tool results section in follow-up prompt")
	}
	if !strings.Contains(systemPrompt, `{"entries":["README.md"]}`) {
		t.Fatalf("expected tool output included in follow-up prompt")
	}
}

func TestAppendToolResultsPromptLimitsToolResultCountAndSize(t *testing.T) {
	results := make([]agent.ToolCallResult, 0, maxPromptToolResults+3)
	for i := 1; i <= maxPromptToolResults+3; i++ {
		results = append(results, agent.ToolCallResult{
			ID:     "tool-" + strconv.Itoa(i),
			Output: strings.Repeat("x", maxPromptToolOutput+200),
		})
	}

	prompt := appendToolResultsPrompt("system", results)
	if !strings.Contains(prompt, "older_results_omitted: 3") {
		t.Fatalf("expected omitted-result marker, got %q", prompt)
	}
	if strings.Contains(prompt, "- id: tool-1\n") || strings.Contains(prompt, "- id: tool-2\n") || strings.Contains(prompt, "- id: tool-3\n") {
		t.Fatalf("expected oldest tool IDs to be omitted from prompt")
	}
	if !strings.Contains(prompt, "- id: tool-4\n") || !strings.Contains(prompt, "- id: tool-15\n") {
		t.Fatalf("expected newest tool IDs to remain in prompt")
	}
	if strings.Count(prompt, strings.Repeat("x", maxPromptToolOutput+50)) > 0 {
		t.Fatalf("expected oversized tool outputs to be truncated")
	}
}

func TestAppendToolResultsPromptIncludesErrorOutputAndRecoveryGuidance(t *testing.T) {
	prompt := appendToolResultsPrompt("system", []agent.ToolCallResult{
		{
			ID:     "tool-1",
			Output: "partial stdout from failed command",
			Error:  "internal.error (shell.exec): permission denied",
		},
	})

	if !strings.Contains(prompt, "## Tool Failure Recovery") {
		t.Fatalf("expected failure recovery guidance in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "error: internal.error") {
		t.Fatalf("expected tool error in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "partial stdout from failed command") {
		t.Fatalf("expected failed call output in prompt for diagnosis, got %q", prompt)
	}
}

func TestProviderModelSendsStructuredHistoryMessages(t *testing.T) {
	var captured requestCapture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{"content": "ok"}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	_, err := model.Generate(context.Background(), agent.ModelRequest{
		SystemPrompt: "system",
		Messages: []agent.ChatMessage{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "done"},
			{Role: "user", Content: "second"},
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(captured.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(captured.Messages))
	}
	if captured.Messages[0].Role != "system" {
		t.Fatalf("expected system role first, got %q", captured.Messages[0].Role)
	}
	if captured.Messages[1].Role != "user" || captured.Messages[1].Content != "first" {
		t.Fatalf("unexpected first history message: %+v", captured.Messages[1])
	}
	if captured.Messages[2].Role != "assistant" || captured.Messages[2].Content != "done" {
		t.Fatalf("unexpected second history message: %+v", captured.Messages[2])
	}
	if captured.Messages[3].Role != "user" || captured.Messages[3].Content != "second" {
		t.Fatalf("unexpected final user message: %+v", captured.Messages[3])
	}
}

func TestProviderModelTraceCapturesModelInputAndToolExtraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	collector := newRunTraceCollector("run_1", "chat_1", "dashboard", "list files")
	ctx := withRunTraceCollector(context.Background(), collector)

	resp, err := model.Generate(ctx, agent.ModelRequest{
		SystemPrompt: "system",
		Messages:     []agent.ChatMessage{{Role: "user", Content: "list files"}},
		AllowedTools: []string{"fs.list"},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}

	trace := collector.Snapshot()
	if trace == nil {
		t.Fatal("expected trace snapshot")
	}
	inputs, ok := trace["model_inputs"].([]any)
	if !ok || len(inputs) != 1 {
		t.Fatalf("expected one model input trace entry, got %#v", trace["model_inputs"])
	}
	entry, ok := inputs[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected model input trace shape: %#v", inputs[0])
	}
	if entry["message"] != "list files" {
		t.Fatalf("unexpected traced message: %#v", entry["message"])
	}
	if entry["history_injected"] != false {
		t.Fatalf("expected history_injected=false, got %#v", entry["history_injected"])
	}

	extractions, ok := trace["extracted_tool_calls"].([]any)
	if !ok || len(extractions) == 0 {
		t.Fatalf("expected tool extraction trace entries, got %#v", trace["extracted_tool_calls"])
	}
	extract, ok := extractions[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected extraction trace shape: %#v", extractions[0])
	}
	if extract["accepted"] != true {
		t.Fatalf("expected accepted extraction, got %#v", extract["accepted"])
	}
	if extract["parsed_tool_name"] != "fs.list" {
		t.Fatalf("unexpected parsed tool name: %#v", extract["parsed_tool_name"])
	}
}

func TestProviderModelStripsThinkTagsFromFinalText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "<think>internal</think>Hello there</think><think>",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hi"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if strings.Contains(resp.FinalText, "<think>") || strings.Contains(resp.FinalText, "</think>") {
		t.Fatalf("think tags leaked in final text: %q", resp.FinalText)
	}
	if resp.FinalText != "Hello there" {
		t.Fatalf("unexpected cleaned final text: %q", resp.FinalText)
	}
}

func TestProviderModelCapturesThinkingInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "<think>secret plan</think>Hello there",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hi"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if !resp.ThinkingPresent {
		t.Fatal("expected thinking_present=true")
	}
	if resp.Thinking != "secret plan" {
		t.Fatalf("unexpected thinking text: %q", resp.Thinking)
	}
	if resp.FinalText != "Hello there" {
		t.Fatalf("unexpected visible text: %q", resp.FinalText)
	}
}

func TestExtractThinkingNestedTagsDoesNotCrash(t *testing.T) {
	visible, thinking, present := ExtractThinking("before <think>outer <think>inner</think> tail</think> after")
	if !present {
		t.Fatal("expected thinkingPresent=true")
	}
	if visible != "before  after" {
		t.Fatalf("unexpected visible text: %q", visible)
	}
	if thinking != "outer <think>inner</think> tail" {
		t.Fatalf("unexpected thinking text: %q", thinking)
	}
}

func TestExtractThinkingMissingClosingTagGraceful(t *testing.T) {
	input := "Hello <analysis>internal plan"
	visible, thinking, present := ExtractThinking(input)
	if !present {
		t.Fatal("expected thinkingPresent=true")
	}
	if visible != "Hello internal plan" {
		t.Fatalf("expected content to remain intact, got %q", visible)
	}
	if thinking != "" {
		t.Fatalf("expected no extracted thinking for ambiguous block, got %q", thinking)
	}
}

func TestExtractThinkingMixedVisibleAndThinkingContent(t *testing.T) {
	input := "start <analysis>plan A</analysis> mid <!-- THINK -->plan B<!-- /THINK --> end"
	visible, thinking, present := ExtractThinking(input)
	if !present {
		t.Fatal("expected thinkingPresent=true")
	}
	if visible != "start  mid  end" {
		t.Fatalf("unexpected visible text: %q", visible)
	}
	if thinking != "plan A\n\nplan B" {
		t.Fatalf("unexpected thinking text: %q", thinking)
	}
}

func TestExtractThinkingPreservesExistingThinkTagStrippingSemantics(t *testing.T) {
	visible, thinking, present := ExtractThinking("<think>internal</think>Hello there</think><think>")
	if !present {
		t.Fatal("expected thinkingPresent=true")
	}
	if visible != "Hello there" {
		t.Fatalf("unexpected visible text: %q", visible)
	}
	if thinking != "internal" {
		t.Fatalf("unexpected thinking text: %q", thinking)
	}
}

func TestProviderModelParsesToolCallsAfterThinkTagStripping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n</think>{\"tool_name\":\"time.now\",\"arguments\":{}}<think>\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "time?"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "time.now" {
		t.Fatalf("unexpected tool call name: %q", resp.ToolCalls[0].Name)
	}
}

func TestProviderModelIgnoresBracketStyleToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "I'll check now.\n[fs.list] path: .",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "list files"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelIgnoresUnfencedJSONToolObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "I will create it now.\n{\"tool\":\"fs.write\",\"path\":\"test.md\",\"content\":\"test\"}",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "create test.md"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelIgnoresXMLStyleToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "<tool_call>fs.read\npath=\"test.md\"</arg_value>",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "read file"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelIgnoresXMLFunctionBlockToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "<tool_call>\n<function=fs.list>\n<path>.</path>\n</function>",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "list files"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelRequiresCanonicalToolJSONFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "{\"tool_code\":\"fs.list\",\"parameters\":{\"path\":\".\"}}",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "list files"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelIgnoresShellSnippets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "I'll check now.\nls\ncat test.md",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "check files"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelRetriesOnTimeout(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			time.Sleep(80 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "retry succeeded",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	model.httpClient = &http.Client{Timeout: 30 * time.Millisecond}

	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hi"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.FinalText != "retry succeeded" {
		t.Fatalf("unexpected final text: %q", resp.FinalText)
	}
	if calls < 2 {
		t.Fatalf("expected retry attempt, got %d call(s)", calls)
	}
}

func TestProviderModelRetriesOnUnexpectedEOF(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"partial`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "retry after eof",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hi"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.FinalText != "retry after eof" {
		t.Fatalf("unexpected final text: %q", resp.FinalText)
	}
	if calls < 2 {
		t.Fatalf("expected retry attempt, got %d call(s)", calls)
	}
}

func TestProviderModelFailsGracefullyOnNetworkConnectionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}}})
	}))
	baseURL := server.URL
	server.Close()

	model := testProviderModel(t, baseURL)
	_, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hi"})
	if err == nil {
		t.Fatal("expected network error when provider endpoint is unavailable")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "connect") && !strings.Contains(lower, "refused") && !strings.Contains(lower, "unreachable") {
		t.Fatalf("expected connection-style error, got %v", err)
	}
}

func TestProviderModelRetriesUseFreshAttemptTimeouts(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			time.Sleep(80 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "recovered after multiple retry attempts",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	model.httpClient = &http.Client{Timeout: 30 * time.Millisecond}

	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hi"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.FinalText != "recovered after multiple retry attempts" {
		t.Fatalf("unexpected final text: %q", resp.FinalText)
	}
	if calls < 3 {
		t.Fatalf("expected third attempt to succeed, got %d calls", calls)
	}
}

func TestProviderModelIgnoresUnknownToolNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "I'll wait a second. time.sleep(1)",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "wait"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no parsed tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelRejectsToolCallsNotInAllowlist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "list files", AllowedTools: []string{"fs.read"}})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelReturnsFriendlyMessageWhenToolIsNotAllowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "Let me call the web API now.\n```json\n{\"tool_name\":\"http.request\",\"arguments\":{\"method\":\"GET\",\"url\":\"https://example.com\"}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "fetch example", AllowedTools: []string{"fs.read"}})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
	if !resp.ToolParseFailure {
		t.Fatal("expected tool parse failure flag")
	}
	if !strings.Contains(resp.FinalText, "http.request") || !strings.Contains(resp.FinalText, "network.enabled=true") {
		t.Fatalf("expected actionable not-allowed guidance, got %q", resp.FinalText)
	}
}

func TestProviderModelReturnsFriendlyMessageWhenToolPayloadIsInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{path:.}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "list files", AllowedTools: []string{"fs.list"}})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
	if !resp.ToolParseFailure {
		t.Fatal("expected tool parse failure flag")
	}
	if !strings.Contains(resp.FinalText, "payload could not be executed") {
		t.Fatalf("expected friendly parse failure text, got %q", resp.FinalText)
	}
}

func TestParseToolDirectiveRepairsTruncatedJSON(t *testing.T) {
	resp, err := parseToolDirective(`/tool secrets.get {"name":"PERPLEXITY_API_KEY"`, []string{"secrets.get"})
	if err != nil {
		t.Fatalf("expected relaxed parse to recover from truncated json, got %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	args := map[string]any{}
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("decode normalized args: %v", err)
	}
	if args["key"] != "provider/perplexity/api_key" {
		t.Fatalf("expected canonical key normalization, got %#v", args["key"])
	}
}

func TestParseToolCallsFromResponseNormalizesAndDedupesEquivalentSecretsCalls(t *testing.T) {
	content := "```json\n[{\"tool_name\":\"secrets.get\",\"arguments\":{\"name\":\"PERPLEXITY_API_KEY\"}},{\"tool_name\":\"secrets.get\",\"arguments\":{\"key\":\"provider/perplexity/api_key\"}}]\n```"
	calls, parseFailure, reason := parseToolCallsFromResponse(content, []string{"secrets.get"}, nil)
	if parseFailure {
		t.Fatalf("expected no parse failure, got reason %q", reason)
	}
	if len(calls) != 1 {
		t.Fatalf("expected equivalent secret lookups to dedupe to one call, got %d", len(calls))
	}
	args := map[string]any{}
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatalf("decode normalized args: %v", err)
	}
	if args["key"] != "provider/perplexity/api_key" {
		t.Fatalf("expected canonical key argument, got %#v", args["key"])
	}
}

func TestProviderModelRejectsShellExecWhenNotAllowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"shell.exec\",\"arguments\":{\"command\":\"pwd\"}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "run pwd", AllowedTools: []string{"fs.list"}})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelRejectsToolResultAsCallableTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"tool.result\",\"arguments\":{}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "test"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelDoesNotSynthesizeWriteCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "Here you go:\n```python\nprint(\"hello\")\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "create hello.py with a simple print"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelIgnoresNonJSONFencedBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```bash\nls -la\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "run ls"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool call, got %d", len(resp.ToolCalls))
	}
}

func TestProviderModelCanonicalizesBashExecAlias(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"bash.exec\",\"arguments\":{\"command\":\"pwd\"}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "run bash"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "shell.exec" {
		t.Fatalf("expected canonical shell.exec, got %q", resp.ToolCalls[0].Name)
	}
}

func TestProviderModelRequestsMaxTokensCap(t *testing.T) {
	var captured requestCapture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}}})
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Model.Provider = "generic"
	cfg.Model.Name = "test-model"
	cfg.Model.MaxTokens = 50000
	cfg.Providers.Generic.BaseURL = server.URL
	cfg.Providers.Generic.APIKey = "test-key"
	cfg.Providers.Generic.APIKeyEnv = ""

	model, err := NewProviderModel(cfg, nil)
	if err != nil {
		t.Fatalf("new provider model: %v", err)
	}

	_, err = model.Generate(context.Background(), agent.ModelRequest{Prompt: "system", Message: "hello"})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if captured.MaxTokens != 20000 {
		t.Fatalf("expected max_tokens=20000, got %d", captured.MaxTokens)
	}
}

func TestProviderModelCompactsAtEightyPercentContext(t *testing.T) {
	var captured requestCapture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}}})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)

	messages := make([]agent.ChatMessage, 0, 260)
	for i := 0; i < 260; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		messages = append(messages, agent.ChatMessage{
			Role:    role,
			Content: strings.Repeat("context-window-line-", 180) + " marker-" + strconv.Itoa(i),
		})
	}
	messages = append(messages, agent.ChatMessage{Role: "user", Content: "latest-question-marker"})

	_, err := model.Generate(context.Background(), agent.ModelRequest{
		SystemPrompt: "system",
		Messages:     messages,
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(captured.Messages) < 3 {
		t.Fatalf("expected compacted request with system + history, got %d message(s)", len(captured.Messages))
	}
	if captured.Messages[1].Role != "system" || !strings.Contains(captured.Messages[1].Content, "Conversation compaction summary") {
		t.Fatalf("expected compaction summary system message, got %+v", captured.Messages[1])
	}
	if captured.Messages[len(captured.Messages)-1].Role != "user" || captured.Messages[len(captured.Messages)-1].Content != "latest-question-marker" {
		t.Fatalf("expected latest user turn preserved, got %+v", captured.Messages[len(captured.Messages)-1])
	}

	reqSystem := captured.Messages[0].Content
	reqHistory := make([]agent.ChatMessage, 0, len(captured.Messages)-1)
	for _, item := range captured.Messages[1:] {
		reqHistory = append(reqHistory, agent.ChatMessage{Role: item.Role, Content: item.Content})
	}
	used := estimateConversationTokens(reqSystem, reqHistory)
	budget := int(float64(model.contextWindow) * contextCompactionRatio)
	if used > budget {
		t.Fatalf("expected compacted context <= %d tokens, got %d", budget, used)
	}
}

func TestProviderModelParsesMultipleToolCallsFromSingleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```\n```json\n{\"tool_name\":\"fs.read\",\"arguments\":{\"path\":\"README.md\"}}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{
		Prompt:       "system",
		Message:      "inspect files",
		AllowedTools: []string{"fs.list", "fs.read"},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.list" || resp.ToolCalls[1].Name != "fs.read" {
		t.Fatalf("unexpected parsed tools: %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].ID == resp.ToolCalls[1].ID {
		t.Fatalf("expected unique tool IDs, got duplicate %q", resp.ToolCalls[0].ID)
	}
}

func TestProviderModelParsesLooseJSONObjectToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "Let me check that now.\n" +
						`{"tool_name":"shell.exec","arguments":{"command":"bash","args":["-lc","python3 --version"]}}`,
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{
		Prompt:       "system",
		Message:      "what version of python is installed?",
		AllowedTools: []string{"shell.exec"},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one parsed tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "shell.exec" {
		t.Fatalf("expected shell.exec tool, got %q", resp.ToolCalls[0].Name)
	}
	if !strings.HasPrefix(resp.ToolCalls[0].ID, "tool-json-") {
		t.Fatalf("expected tool-json id, got %q", resp.ToolCalls[0].ID)
	}
}

func TestProviderModelParsesToolCallArrayFromResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": `[{"tool_name":"fs.list","arguments":{"path":"."}},{"tool_name":"fs.read","arguments":{"path":"README.md"}}]`,
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{
		Prompt:       "system",
		Message:      "inspect files",
		AllowedTools: []string{"fs.list", "fs.read"},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.list" || resp.ToolCalls[1].Name != "fs.read" {
		t.Fatalf("unexpected parsed tools: %+v", resp.ToolCalls)
	}
}

func TestProviderModelTraceIncludesRejectedParseDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]string{
					"content": "```json\n{invalid}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	collector := newRunTraceCollector("run_2", "chat_2", "dashboard", "list files")
	ctx := withRunTraceCollector(context.Background(), collector)

	_, err := model.Generate(ctx, agent.ModelRequest{
		SystemPrompt: "system",
		Messages:     []agent.ChatMessage{{Role: "user", Content: "list files"}},
		AllowedTools: []string{"fs.list"},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	trace := collector.Snapshot()
	extractions, ok := trace["extracted_tool_calls"].([]any)
	if !ok || len(extractions) == 0 {
		t.Fatalf("expected extraction trace entries, got %#v", trace["extracted_tool_calls"])
	}
	entry, ok := extractions[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected extraction entry shape: %#v", extractions[0])
	}
	if entry["accepted"] != false {
		t.Fatalf("expected rejected extraction, got %#v", entry["accepted"])
	}
	reason := strings.ToLower(strings.TrimSpace(entry["reason"].(string)))
	if !strings.Contains(reason, "invalid json") {
		t.Fatalf("expected invalid json reason, got %q", reason)
	}
}

func TestProviderModelUsesCurrentMessageForToolDirectiveDetection(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "ok"}}},
		})
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	resp, err := model.Generate(context.Background(), agent.ModelRequest{
		Message: "what should we do now?",
		Messages: []agent.ChatMessage{
			{Role: "user", Content: `/tool fs.list {"path":"."}`},
			{Role: "assistant", Content: "Done."},
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected provider to be called once, got %d", requestCount)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no direct tool calls from historical /tool turn, got %+v", resp.ToolCalls)
	}
	if resp.FinalText != "ok" {
		t.Fatalf("unexpected final text: %q", resp.FinalText)
	}
}

func TestProviderModelToolDirectiveSupportsBashAlias(t *testing.T) {
	model := testProviderModel(t, "http://unused.local")
	resp, err := model.Generate(context.Background(), agent.ModelRequest{Message: `/tool bash.exec {"command":"pwd"}`})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "shell.exec" {
		t.Fatalf("expected canonical shell.exec, got %q", resp.ToolCalls[0].Name)
	}
}

func TestParseLooseJSONToolCallsDedupesCandidates(t *testing.T) {
	trace := newRunTraceCollector("run-loose", "", "", "")
	content := `first {"tool_name":"fs.list","arguments":{"path":"."}} then {"tool_name":"fs.list","arguments":{"path":"."}} and {"tool_name":"fs.read","arguments":{"path":"README.md"}}`
	calls := parseLooseJSONToolCalls(content, []string{"fs.list", "fs.read"}, trace)

	if len(calls) != 2 {
		t.Fatalf("expected 2 deduped calls, got %d (%+v)", len(calls), calls)
	}
	if calls[0].Name != "fs.list" || calls[1].Name != "fs.read" {
		t.Fatalf("unexpected call order/names: %+v", calls)
	}
	if !strings.HasPrefix(calls[0].ID, "tool-json-loose-") {
		t.Fatalf("expected loose-json id prefix, got %q", calls[0].ID)
	}

	snapshot := trace.Snapshot()
	extractions, ok := snapshot["extracted_tool_calls"].([]any)
	if !ok || len(extractions) == 0 {
		t.Fatalf("expected extraction trace entries, got %#v", snapshot["extracted_tool_calls"])
	}
}

func TestSynthesizeWriteCallFromResponse(t *testing.T) {
	content := "Here is the file:\n```txt\nhello\nworld\n```"
	call, ok := synthesizeWriteCallFromResponse(content, "create notes.txt with this content", 3)
	if !ok {
		t.Fatal("expected synthesized write call")
	}
	if call.Name != "fs.write" || call.ID != "tool-synth-3" {
		t.Fatalf("unexpected synthesized call: %+v", call)
	}
	args := decodeToolArgs(t, call.Arguments)
	if args["path"] != "notes.txt" {
		t.Fatalf("expected notes.txt path, got %#v", args["path"])
	}
	if args["content"] != "hello\nworld" {
		t.Fatalf("unexpected synthesized content: %#v", args["content"])
	}

	if _, ok := synthesizeWriteCallFromResponse(content, "summarize this", 1); ok {
		t.Fatal("expected synthesis disabled for non-create request")
	}
}

func TestParseBashBlocksAndShellSnippets(t *testing.T) {
	bashCalls := parseBashCodeBlocks("```bash\n$ ls -la\ncat README.md\n```", 2)
	if len(bashCalls) != 1 {
		t.Fatalf("expected one bash call, got %d", len(bashCalls))
	}
	if bashCalls[0].Name != "shell.exec" || bashCalls[0].ID != "tool-bash-2" {
		t.Fatalf("unexpected bash call: %+v", bashCalls[0])
	}
	args := decodeToolArgs(t, bashCalls[0].Arguments)
	if args["command"] != "bash" {
		t.Fatalf("expected bash command, got %#v", args["command"])
	}

	plain := removeFencedCodeBlocks("before\n```bash\nls\n```\nafter")
	if strings.Contains(plain, "ls") {
		t.Fatalf("expected fenced block removal, got %q", plain)
	}

	snippets := parseShellSnippets("ls -la ./docs\ncat README.md\n$ ls", 5)
	if len(snippets) != 3 {
		t.Fatalf("expected 3 shell snippet calls, got %d", len(snippets))
	}
	if snippets[0].Name != "fs.list" || snippets[1].Name != "fs.list" || snippets[2].Name != "fs.read" {
		t.Fatalf("unexpected snippet calls: %+v", snippets)
	}
}

func TestParseJSONToolCallAndCandidateExtraction(t *testing.T) {
	raw := `{"function":"bash.exec","command":"pwd"}`
	call, ok := parseJSONToolCall(raw, 7)
	if !ok {
		t.Fatal("expected valid json tool call")
	}
	if call.Name != "shell.exec" || call.ID != "tool-json-7" {
		t.Fatalf("unexpected parsed json tool call: %+v", call)
	}
	args := decodeToolArgs(t, call.Arguments)
	if args["command"] != "pwd" {
		t.Fatalf("expected command=pwd, got %#v", args["command"])
	}

	if _, ok := parseJSONToolCall(`{"arguments":{"path":"."}}`, 1); ok {
		t.Fatal("expected invalid json tool call without tool name")
	}

	cands := extractJSONObjectCandidates(`noise {"tool_name":"fs.list","arguments":{"path":"{x}"}} bad {oops} tail {"tool_name":"fs.read","arguments":{"path":"README.md"}}`)
	if len(cands) != 2 {
		t.Fatalf("expected 2 valid object candidates, got %d (%#v)", len(cands), cands)
	}
}

func TestParseFunctionCallAndArgsString(t *testing.T) {
	call := parseFunctionCall(`fs.list(path="docs")`)
	if call == nil || call.Name != "fs.list" {
		t.Fatalf("expected fs.list function call, got %+v", call)
	}
	args := decodeToolArgs(t, call.Arguments)
	if args["path"] != "docs" {
		t.Fatalf("expected path=docs, got %#v", args["path"])
	}

	call = parseFunctionCall(`fs.read("README.md")`)
	if call == nil || call.Name != "fs.read" {
		t.Fatalf("expected fs.read function call, got %+v", call)
	}
	args = decodeToolArgs(t, call.Arguments)
	if args["path"] != "README.md" {
		t.Fatalf("expected positional path, got %#v", args["path"])
	}

	if call := parseFunctionCall("not a function call"); call != nil {
		t.Fatalf("expected nil call for invalid syntax, got %+v", call)
	}

	parsed := parseArgsString(`{"path":".","recursive":true}`)
	if parsed["path"] != "." || parsed["recursive"] != true {
		t.Fatalf("unexpected json args parse: %#v", parsed)
	}
	parsed = parseArgsString(`path=README.md mode=ro`)
	if parsed["path"] != "README.md" || parsed["mode"] != "ro" {
		t.Fatalf("unexpected key=value args parse: %#v", parsed)
	}
}

func TestToolNameHelpersAndAllowlist(t *testing.T) {
	if isToolAllowed("tool.result", nil) {
		t.Fatal("tool.result must never be allowed")
	}
	if !isToolAllowed("fs.list", nil) {
		t.Fatal("expected unrestricted allowlist to allow fs.list")
	}
	if !isToolAllowed("shell.exec", []string{"bash.exec"}) {
		t.Fatal("expected alias to map and allow shell.exec")
	}
	if isToolAllowed("fs.write", []string{"fs.read"}) {
		t.Fatal("expected fs.write to be denied")
	}
	if !isToolAllowed("fs.delete", []string{"fs.delete"}) {
		t.Fatal("expected fs.delete to be allowed when explicitly granted")
	}
	if !isToolAllowed("fs.append", []string{"fs.append"}) {
		t.Fatal("expected fs.append to be allowed when explicitly granted")
	}
	if !isToolAllowed("fs.move", []string{"fs.rename"}) {
		t.Fatal("expected fs.rename alias to allow canonical fs.move")
	}
	if !isToolAllowed("config.set", []string{"config.set"}) {
		t.Fatal("expected config.set to be allowed when explicitly granted")
	}
	if !isToolAllowed("secrets.get", []string{"secrets.get"}) {
		t.Fatal("expected secrets.get to be allowed when explicitly granted")
	}
	if !isToolAllowed("scheduler.list", []string{"scheduler.list"}) {
		t.Fatal("expected scheduler.list to be allowed when explicitly granted")
	}
	if !isToolAllowed("session.close", []string{"session.close"}) {
		t.Fatal("expected session.close to be allowed when explicitly granted")
	}
	if !isToolAllowed("skill.list", []string{"skill.list"}) {
		t.Fatal("expected skill.list to be allowed when explicitly granted")
	}
	if !isToolAllowed("skill.read", []string{"skill.get"}) {
		t.Fatal("expected skill.get alias to allow canonical skill.read")
	}
	if !isToolAllowed("agent.list", []string{"agent.list"}) {
		t.Fatal("expected agent.list to be allowed when explicitly granted")
	}
	if !isToolAllowed("agent.create", []string{"agent.create"}) {
		t.Fatal("expected agent.create to be allowed when explicitly granted")
	}
	if !isToolAllowed("agent.switch", []string{"agent.switch"}) {
		t.Fatal("expected agent.switch to be allowed when explicitly granted")
	}
	if !isToolAllowed("policy.grant", []string{"policy.grant"}) {
		t.Fatal("expected policy.grant to be allowed when explicitly granted")
	}
	if !isToolAllowed("run.cancel", []string{"run.cancel"}) {
		t.Fatal("expected run.cancel to be allowed when explicitly granted")
	}
	if !isToolAllowed("metrics.get", []string{"metrics.get"}) {
		t.Fatal("expected metrics.get to be allowed when explicitly granted")
	}
	if !isToolAllowed("http.request", []string{"net.fetch"}) {
		t.Fatal("expected net.fetch alias to allow canonical http.request")
	}

	if canonical, ok := canonicalToolName("terminal.run"); !ok || canonical != "shell.exec" {
		t.Fatalf("unexpected canonical alias mapping: ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("skill.get"); !ok || canonical != "skill.read" {
		t.Fatalf("expected skill.get alias to canonicalize to skill.read, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("fs.delete"); !ok || canonical != "fs.delete" {
		t.Fatalf("expected fs.delete canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("fs.append"); !ok || canonical != "fs.append" {
		t.Fatalf("expected fs.append canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("fs.rename"); !ok || canonical != "fs.move" {
		t.Fatalf("expected fs.rename alias to canonicalize to fs.move, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("config.get"); !ok || canonical != "config.get" {
		t.Fatalf("expected config.get canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("secrets.list"); !ok || canonical != "secrets.list" {
		t.Fatalf("expected secrets.list canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("scheduler.pause"); !ok || canonical != "scheduler.pause" {
		t.Fatalf("expected scheduler.pause canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("session.list"); !ok || canonical != "session.list" {
		t.Fatalf("expected session.list canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("agent.list"); !ok || canonical != "agent.list" {
		t.Fatalf("expected agent.list canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("agent.create"); !ok || canonical != "agent.create" {
		t.Fatalf("expected agent.create canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("agent.switch"); !ok || canonical != "agent.switch" {
		t.Fatalf("expected agent.switch canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("policy.revoke"); !ok || canonical != "policy.revoke" {
		t.Fatalf("expected policy.revoke canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("run.cancel"); !ok || canonical != "run.cancel" {
		t.Fatalf("expected run.cancel canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("metrics.get"); !ok || canonical != "metrics.get" {
		t.Fatalf("expected metrics.get canonical mapping, got ok=%v canonical=%q", ok, canonical)
	}
	if canonical, ok := canonicalToolName("net.fetch"); !ok || canonical != "http.request" {
		t.Fatalf("expected net.fetch alias to canonicalize to http.request, got ok=%v canonical=%q", ok, canonical)
	}
	if _, ok := canonicalToolName("unknown.tool"); ok {
		t.Fatal("expected unknown tool alias to fail")
	}
}

type testNetError struct{}

func (testNetError) Error() string   { return "temporary network failure" }
func (testNetError) Timeout() bool   { return true }
func (testNetError) Temporary() bool { return true }

func TestProviderRetryAndTimeoutHelpers(t *testing.T) {
	if shouldRetryProviderError(nil) {
		t.Fatal("nil errors must not be retryable")
	}
	if shouldRetryProviderError(context.Canceled) {
		t.Fatal("context canceled must not be retryable")
	}
	if !shouldRetryProviderError(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded should be retryable")
	}
	if !shouldRetryProviderError(testNetError{}) {
		t.Fatal("timeout net error should be retryable")
	}
	if !shouldRetryProviderError(errors.New("retryable provider status: 429")) {
		t.Fatal("retryable status text should be retryable")
	}

	ctx := context.Background()
	timeoutCtx, cancel := ensureProviderRequestTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if _, ok := timeoutCtx.Deadline(); !ok {
		t.Fatal("expected timeout context to have deadline")
	}

	base, baseCancel := context.WithTimeout(ctx, time.Second)
	defer baseCancel()
	keptCtx, keptCancel := ensureProviderRequestTimeout(base, 10*time.Millisecond)
	defer keptCancel()
	originalDeadline, ok := base.Deadline()
	if !ok {
		t.Fatal("expected base context deadline")
	}
	keptDeadline, ok := keptCtx.Deadline()
	if !ok || !keptDeadline.Equal(originalDeadline) {
		t.Fatalf("expected existing deadline to be preserved, got %v (ok=%v), want %v", keptDeadline, ok, originalDeadline)
	}
}

func TestProviderEndpointAndMessageHelpers(t *testing.T) {
	cfg := config.Default()
	providers := []string{"openai", "openrouter", "requesty", "zai", "generic"}
	for _, name := range providers {
		if _, err := providerEndpoint(cfg, name); err != nil {
			t.Fatalf("expected provider %q to resolve, got %v", name, err)
		}
	}
	if _, err := providerEndpoint(cfg, "unknown"); err == nil {
		t.Fatal("expected unsupported provider error")
	}

	messages := []agent.ChatMessage{{Role: "assistant", Content: "a"}, {Role: "system", Content: "s"}}
	if got := lastUserMessage(messages); got != "s" {
		t.Fatalf("expected fallback to last message content, got %q", got)
	}
	if got := lastUserMessage(nil); got != "" {
		t.Fatalf("expected empty last user message for empty history, got %q", got)
	}

	trimmed := truncateCompactedMessages("sys", []agent.ChatMessage{
		{Role: "system", Content: strings.Repeat("s", 1200)},
		{Role: "user", Content: strings.Repeat("u", 1200)},
		{Role: "assistant", Content: strings.Repeat("a", 1200)},
		{Role: "user", Content: "tail"},
	}, 20)
	if len(trimmed) >= 4 {
		t.Fatalf("expected compaction to drop older messages, got %d entries", len(trimmed))
	}
	if trimmed[len(trimmed)-1].Content != "tail" {
		t.Fatalf("expected latest turn preserved, got %+v", trimmed)
	}
}

func TestProviderModelStreamingParsesDeltasAndReturnsFinalText(t *testing.T) {
	var captured requestCapture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"world!\"}}]}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	var deltas []string
	resp, err := model.Generate(context.Background(), agent.ModelRequest{
		Prompt:  "system",
		Message: "hello",
		OnTextDelta: func(delta string) error {
			deltas = append(deltas, delta)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if !captured.Stream {
		t.Fatal("expected streaming request payload to set stream=true")
	}
	if resp.FinalText != "Hello world!" {
		t.Fatalf("unexpected final text: %q", resp.FinalText)
	}
	if strings.Join(deltas, "") != "Hello world!" {
		t.Fatalf("unexpected streamed deltas: %#v", deltas)
	}
}

func TestProviderModelStreamingRetriesBeforeFirstDelta(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "temporary upstream"})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"retry ok\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	var deltas []string
	resp, err := model.Generate(context.Background(), agent.ModelRequest{
		Prompt:  "system",
		Message: "hello",
		OnTextDelta: func(delta string) error {
			deltas = append(deltas, delta)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected retry before first delta, got %d call(s)", calls)
	}
	if resp.FinalText != "retry ok" {
		t.Fatalf("unexpected final text after retry: %q", resp.FinalText)
	}
	if strings.Join(deltas, "") != "retry ok" {
		t.Fatalf("unexpected streamed deltas: %#v", deltas)
	}
}

func TestProviderModelStreamingDoesNotRetryAfterFirstDelta(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	model := testProviderModel(t, server.URL)
	deltaCalls := 0
	_, err := model.Generate(context.Background(), agent.ModelRequest{
		Prompt:  "system",
		Message: "hello",
		OnTextDelta: func(_ string) error {
			deltaCalls++
			return context.DeadlineExceeded
		},
	})
	if err == nil {
		t.Fatal("expected streaming callback error")
	}
	if calls != 1 {
		t.Fatalf("expected no retry after first emitted delta, got %d call(s)", calls)
	}
	if deltaCalls != 1 {
		t.Fatalf("expected one emitted delta before failure, got %d", deltaCalls)
	}
}

func decodeToolArgs(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	return out
}

func testProviderModel(t *testing.T, baseURL string) *ProviderModel {
	t.Helper()

	cfg := config.Default()
	cfg.Model.Provider = "generic"
	cfg.Model.Name = "test-model"
	cfg.Providers.Generic.BaseURL = baseURL
	cfg.Providers.Generic.APIKey = "test-key"
	cfg.Providers.Generic.APIKeyEnv = ""

	model, err := NewProviderModel(cfg, nil)
	if err != nil {
		t.Fatalf("new provider model: %v", err)
	}
	return model
}
