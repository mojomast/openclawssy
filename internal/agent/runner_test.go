package agent

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

type mockModel struct {
	responses []ModelResponse
	idx       int
	reqs      []ModelRequest
}

func (m *mockModel) Generate(_ context.Context, req ModelRequest) (ModelResponse, error) {
	m.reqs = append(m.reqs, req)
	if m.idx >= len(m.responses) {
		return ModelResponse{}, errors.New("no more responses")
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

type mockTools struct {
	results map[string]ToolCallResult
	calls   []ToolCallRequest
}

type noChoicesThenSuccessModel struct {
	attempts int
}

func (m *noChoicesThenSuccessModel) Generate(_ context.Context, _ ModelRequest) (ModelResponse, error) {
	m.attempts++
	if m.attempts <= 2 {
		return ModelResponse{}, errors.New("provider returned no choices")
	}
	return ModelResponse{FinalText: "done"}, nil
}

type alwaysNoChoicesModel struct {
	attempts int
}

func (m *alwaysNoChoicesModel) Generate(_ context.Context, _ ModelRequest) (ModelResponse, error) {
	m.attempts++
	return ModelResponse{}, errors.New("provider returned no choices")
}

type toolThenNoChoicesModel struct {
	attempts int
}

func (m *toolThenNoChoicesModel) Generate(_ context.Context, _ ModelRequest) (ModelResponse, error) {
	m.attempts++
	if m.attempts == 1 {
		return ModelResponse{ToolCalls: []ToolCallRequest{{ID: "1", Name: "memory.search", Arguments: []byte(`{"query":"research"}`)}}}, nil
	}
	return ModelResponse{}, errors.New("provider returned no choices")
}

type transientErrorThenSuccessModel struct {
	attempts int
}

func (m *transientErrorThenSuccessModel) Generate(_ context.Context, _ ModelRequest) (ModelResponse, error) {
	m.attempts++
	if m.attempts <= 2 {
		return ModelResponse{}, errors.New("provider stream interrupted after partial output: context deadline exceeded (Client.Timeout while reading body)")
	}
	return ModelResponse{FinalText: "done"}, nil
}

type alwaysTransientErrorModel struct {
	attempts int
}

func (m *alwaysTransientErrorModel) Generate(_ context.Context, _ ModelRequest) (ModelResponse, error) {
	m.attempts++
	return ModelResponse{}, errors.New("provider stream interrupted after partial output: context deadline exceeded")
}

func (m *mockTools) Execute(_ context.Context, call ToolCallRequest) (ToolCallResult, error) {
	m.calls = append(m.calls, call)
	result, ok := m.results[call.ID]
	if !ok {
		result = ToolCallResult{ID: call.ID, Output: "ok:" + call.Name}
	}
	return result, nil
}

type slowToolExecutor struct{}

func (s slowToolExecutor) Execute(ctx context.Context, call ToolCallRequest) (ToolCallResult, error) {
	_ = call
	<-ctx.Done()
	return ToolCallResult{}, ctx.Err()
}

func TestRunnerBasicLoopWithTools(t *testing.T) {
	model := &mockModel{
		responses: []ModelResponse{
			{
				ToolCalls: []ToolCallRequest{{ID: "call-1", Name: "time.now"}},
			},
			{
				FinalText: "done",
			},
		},
	}

	tools := &mockTools{
		results: map[string]ToolCallResult{
			"call-1": {ID: "call-1", Output: "2026-02-15T00:00:00Z"},
		},
	}

	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 2}
	out, err := runner.Run(context.Background(), RunInput{
		Message:      "What time is it?",
		ArtifactDocs: []ArtifactDoc{{Name: "SOUL.md", Content: "help user"}},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call record, got %d", len(out.ToolCalls))
	}
	if len(model.reqs) != 2 {
		t.Fatalf("expected 2 model calls, got %d", len(model.reqs))
	}
	if len(model.reqs[1].ToolResults) != 1 {
		t.Fatalf("expected second model call to include tool result")
	}
	if model.reqs[1].ToolResults[0].Output != "2026-02-15T00:00:00Z" {
		t.Fatalf("unexpected tool result passed to model: %q", model.reqs[1].ToolResults[0].Output)
	}
}

func TestRunnerRepromptsWhenModelDefersWithoutToolCall(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{FinalText: "Let me check that right now."},
		{ToolCalls: []ToolCallRequest{{ID: "call-1", Name: "secrets.get", Arguments: []byte(`{"name":"PERPLEXITY_API_KEY"}`)}}},
		{FinalText: "I checked it and the key exists."},
	}}
	tools := &mockTools{results: map[string]ToolCallResult{
		"call-1": {ID: "call-1", Output: `{"ok":true}`},
	}}

	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}
	out, err := runner.Run(context.Background(), RunInput{
		Message:      "can you verify the perplexity key",
		AllowedTools: []string{"secrets.get"},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "I checked it and the key exists." {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("expected one tool call after reprompt, got %d", len(out.ToolCalls))
	}
	if len(model.reqs) != 3 {
		t.Fatalf("expected 3 model requests including reprompt, got %d", len(model.reqs))
	}
	if !strings.Contains(model.reqs[1].SystemPrompt, "ACTION_EXECUTION_MODE") {
		t.Fatalf("expected follow-through directive in reprompt, got %q", model.reqs[1].SystemPrompt)
	}
}

func TestRunnerRepromptsAfterToolParseFailure(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{FinalText: "<tool_call>fs.list,{\"path\":\".\"}", ToolParseFailure: true},
		{ToolCalls: []ToolCallRequest{{ID: "call-1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{FinalText: "done"},
	}}
	tools := &mockTools{results: map[string]ToolCallResult{
		"call-1": {ID: "call-1", Output: `{"items":[]}`},
	}}

	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}
	out, err := runner.Run(context.Background(), RunInput{
		Message:      "list files",
		AllowedTools: []string{"fs.list"},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("expected one tool execution after parse recovery, got %d", len(out.ToolCalls))
	}
	if len(model.reqs) != 3 {
		t.Fatalf("expected reprompt model call sequence, got %d", len(model.reqs))
	}
	if !strings.Contains(model.reqs[1].SystemPrompt, "TOOL_PARSE_RECOVERY_MODE") {
		t.Fatalf("expected parse recovery directive in reprompt, got %q", model.reqs[1].SystemPrompt)
	}
}

func TestRunnerAddsPartialSuccessDirectiveAfterMixedBatchOutcome(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{
			{ID: "call-1", Name: "fs.write", Arguments: []byte(`{"path":"entry.md","content":"ok"}`)},
			{ID: "call-2", Name: "memory.write", Arguments: []byte(`{"content":"note"}`)},
			{ID: "call-3", Name: "fs.append", Arguments: []byte(`{"path":"exploration_journal.md","content":"line"}`)},
		}},
		{FinalText: "done"},
	}}
	tools := &mockTools{results: map[string]ToolCallResult{
		"call-1": {ID: "call-1", Output: "wrote entry"},
		"call-2": {ID: "call-2", Error: "missing required field: kind"},
		"call-3": {ID: "call-3", Output: "journal appended"},
	}}

	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}
	out, err := runner.Run(context.Background(), RunInput{Message: "record findings"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(model.reqs) < 2 {
		t.Fatalf("expected at least two model requests, got %d", len(model.reqs))
	}
	if !strings.Contains(model.reqs[1].SystemPrompt, "PARTIAL_SUCCESS_MODE") {
		t.Fatalf("expected partial-success directive in second turn prompt, got %q", model.reqs[1].SystemPrompt)
	}
	if !strings.Contains(model.reqs[1].SystemPrompt, "fs.write") || !strings.Contains(model.reqs[1].SystemPrompt, "memory.write") {
		t.Fatalf("expected tool-specific mixed outcome guidance, got %q", model.reqs[1].SystemPrompt)
	}
}

func TestRunnerReplacesRepeatedDeferralWithConcreteFallback(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{FinalText: "Let me check that right now."},
		{FinalText: "Okay, let me do that now."},
		{FinalText: "Let me actually execute it."},
		{FinalText: "Hold on while I verify."},
		{FinalText: "Give me a moment."},
		{FinalText: "I will do that now."},
	}}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 8}

	out, err := runner.Run(context.Background(), RunInput{
		Message:      "please verify the key",
		AllowedTools: []string{"secrets.get"},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(out.FinalText, "could not complete an actionable execution step") {
		t.Fatalf("expected non-actionable fallback final text, got %q", out.FinalText)
	}
	if len(out.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls in fallback case, got %d", len(out.ToolCalls))
	}
}

func TestRunnerPassesRunMetadataAndContextToModelRequests(t *testing.T) {
	history := []ChatMessage{
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "follow-up"},
	}
	allowedTools := []string{"fs.list", "fs.read"}

	model := &mockModel{responses: []ModelResponse{{FinalText: "done"}}}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 2}

	input := RunInput{
		AgentID:       "agent-chat",
		RunID:         "run-42",
		Message:       "latest user message",
		Messages:      history,
		AllowedTools:  allowedTools,
		ToolTimeoutMS: 7777,
	}
	out, err := runner.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(model.reqs) != 1 {
		t.Fatalf("expected one model request, got %d", len(model.reqs))
	}
	req := model.reqs[0]
	if req.AgentID != input.AgentID || req.RunID != input.RunID {
		t.Fatalf("expected run metadata to pass through, got agent=%q run=%q", req.AgentID, req.RunID)
	}
	if req.ToolTimeoutMS != input.ToolTimeoutMS {
		t.Fatalf("expected tool timeout to pass through, got %d", req.ToolTimeoutMS)
	}
	if len(req.Messages) != len(history) {
		t.Fatalf("expected %d history messages, got %d", len(history), len(req.Messages))
	}
	if req.Messages[0].Content != history[0].Content || req.Messages[2].Content != history[2].Content {
		t.Fatalf("expected history messages to be forwarded unchanged, got %+v", req.Messages)
	}
	if len(req.AllowedTools) != len(allowedTools) || req.AllowedTools[0] != allowedTools[0] || req.AllowedTools[1] != allowedTools[1] {
		t.Fatalf("expected allowed tools to pass through, got %#v", req.AllowedTools)
	}
}

func TestRunnerAppliesSystemPromptExtenderEachTurn(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "call-1", Name: "time.now"}}},
		{FinalText: "done"},
	}}
	tools := &mockTools{results: map[string]ToolCallResult{"call-1": {ID: "call-1", Output: "2026-02-19T00:00:00Z"}}}

	callCount := 0
	ext := func(_ context.Context, basePrompt string, _ []ChatMessage, _ string, _ []ToolCallResult) string {
		callCount++
		return appendPromptDirective(basePrompt, "--- RELEVANT MEMORY ---\n[MEM-1] Test memory\n------------------------")
	}

	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 3}
	out, err := runner.Run(context.Background(), RunInput{Message: "hello", SystemPromptExt: ext})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if callCount < 2 {
		t.Fatalf("expected extender to run for each model turn, got %d", callCount)
	}
	for i, req := range model.reqs {
		if !strings.Contains(req.SystemPrompt, "RELEVANT MEMORY") {
			t.Fatalf("model request %d missing recall block: %q", i, req.SystemPrompt)
		}
	}
}

func TestRunnerToolIterationCap(t *testing.T) {
	model := &mockModel{
		responses: []ModelResponse{
			{ToolCalls: []ToolCallRequest{{ID: "1", Name: "a"}}},
			{ToolCalls: []ToolCallRequest{{ID: "2", Name: "b"}}},
		},
	}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 1}

	out, err := runner.Run(context.Background(), RunInput{Message: "loop"})
	if err != nil {
		t.Fatalf("expected graceful fallback, got %v", err)
	}
	if out.FinalText == "" {
		t.Fatal("expected fallback final text when cap reached after tool results")
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("expected first tool call record retained, got %d", len(out.ToolCalls))
	}
}

func TestRunnerCachesRepeatedToolCallResult(t *testing.T) {
	model := &mockModel{
		responses: []ModelResponse{
			{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
			{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
			{ToolCalls: []ToolCallRequest{{ID: "3", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
			{ToolCalls: []ToolCallRequest{{ID: "4", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
			{FinalText: "done"},
		},
	}
	tools := &mockTools{results: map[string]ToolCallResult{"1": {ID: "1", Output: `{"entries":["a.txt"]}`}}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}

	out, err := runner.Run(context.Background(), RunInput{Message: "loop"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 4 {
		t.Fatalf("expected four tool call records, got %d", len(out.ToolCalls))
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected tool executor to run once and reuse cached result, got %d calls", len(tools.calls))
	}
	for i, rec := range out.ToolCalls {
		if rec.Result.Error != "" {
			t.Fatalf("expected no repeated-call guard errors, got record %d: %+v", i, rec)
		}
		if rec.Result.Output != `{"entries":["a.txt"]}` {
			t.Fatalf("expected cached output for record %d, got %q", i, rec.Result.Output)
		}
	}
}

func TestRunnerCachesHTTPRequestsWithImplicitGETAndRootSlashVariants(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "http.request", Arguments: []byte(`{"method":"GET","url":"https://ussy.host"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "http.request", Arguments: []byte(`{"url":"https://ussy.host/"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Output: `{"status":200,"url":"https://ussy.host"}`},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}

	out, err := runner.Run(context.Background(), RunInput{Message: "fetch homepage"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected two tool call records, got %d", len(out.ToolCalls))
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected semantically-equivalent http.request call to use cache, got %d executions", len(tools.calls))
	}
	if out.ToolCalls[0].Result.Output != out.ToolCalls[1].Result.Output {
		t.Fatalf("expected cached http.request output to be reused, got %+v", out.ToolCalls)
	}
}

func TestRunnerAllowsRepeatedCallAndLetsModelRecover(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{"1": {ID: "1", Output: "ok"}}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}
	out, err := runner.Run(context.Background(), RunInput{Message: "loop"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected two tool call records, got %d", len(out.ToolCalls))
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected repeated call to be served from cache, got %d tool executions", len(tools.calls))
	}
	if out.ToolCalls[0].Result.Output != out.ToolCalls[1].Result.Output {
		t.Fatalf("expected repeated call output to be reused, got %+v", out.ToolCalls)
	}
}

func TestRunnerAllowsRepeatedCallAfterPreviousToolError(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{FinalText: "done"},
	}}
	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Error: "temporary failure"},
		"2": {ID: "2", Output: "ok"},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}

	out, err := runner.Run(context.Background(), RunInput{Message: "retry"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 2 {
		t.Fatalf("expected both repeated calls to execute after first error, got %d", len(tools.calls))
	}
}

func TestRunnerCachesRepeatedFailureAfterSecondIdenticalError(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","nmap -sS 127.0.0.1"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","nmap -sS 127.0.0.1"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","nmap -sS 127.0.0.1"]}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Error: "internal.error (shell.exec): exit status 1"},
		"2": {ID: "2", Error: "internal.error (shell.exec): exit status 1"},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 8}

	out, err := runner.Run(context.Background(), RunInput{Message: "scan localhost"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 3 {
		t.Fatalf("expected three tool call records, got %d", len(out.ToolCalls))
	}
	if len(tools.calls) != 2 {
		t.Fatalf("expected third identical failing call to be served from cache, got %d executions", len(tools.calls))
	}
	if out.ToolCalls[2].Result.Error == "" {
		t.Fatalf("expected cached failure on third call, got %+v", out.ToolCalls[2].Result)
	}
}

func TestRunnerBlocksRepeatedFileWriteLoopsOnSamePath(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.write", Arguments: []byte(`{"path":"notes.md","content":"v1"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.write", Arguments: []byte(`{"path":"notes.md","content":"v2"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "fs.write", Arguments: []byte(`{"path":"notes.md","content":"v3"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "4", Name: "fs.write", Arguments: []byte(`{"path":"notes.md","content":"v4"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "5", Name: "fs.write", Arguments: []byte(`{"path":"notes.md","content":"v5"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "6", Name: "fs.write", Arguments: []byte(`{"path":"notes.md","content":"v6"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "7", Name: "fs.write", Arguments: []byte(`{"path":"notes.md","content":"v7"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "journal"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 5 {
		t.Fatalf("expected only first 5 writes to execute, got %d", len(tools.calls))
	}
	if len(out.ToolCalls) != 7 {
		t.Fatalf("expected seven tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[5].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on 6th call, got %q", out.ToolCalls[5].Result.Error)
	}
	if !strings.Contains(out.ToolCalls[6].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on 7th call, got %q", out.ToolCalls[6].Result.Error)
	}
}

func TestRunnerBlocksSecondJournalWriteInSameRun(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.write", Arguments: []byte(`{"path":"exploration_journal.md","content":"v1"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.write", Arguments: []byte(`{"path":"exploration_journal.md","content":"v2"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "journal"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected only first journal write to execute, got %d", len(tools.calls))
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected two tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[1].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on second journal write, got %q", out.ToolCalls[1].Result.Error)
	}
}

func TestRunnerBlocksThirdJournalReadInSameRun(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.read", Arguments: []byte(`{"path":"exploration_journal.md"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.read", Arguments: []byte(`{"path":"exploration_journal.md"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "fs.read", Arguments: []byte(`{"path":"exploration_journal.md"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Error: "tool.input_invalid (fs.read): path denied: exploration_journal.md (read path does not exist or is invalid)"},
		"2": {ID: "2", Error: "tool.input_invalid (fs.read): path denied: exploration_journal.md (read path does not exist or is invalid)"},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "journal"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 2 {
		t.Fatalf("expected only first two journal reads to execute, got %d", len(tools.calls))
	}
	if len(out.ToolCalls) != 3 {
		t.Fatalf("expected three tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[2].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on third journal read, got %q", out.ToolCalls[2].Result.Error)
	}
}

func TestRunnerBlocksThirdBuildLogReadInSameRun(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.read", Arguments: []byte(`{"path":"ussyflow_build_log.md"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.read", Arguments: []byte(`{"path":"ussyflow_build_log.md"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "fs.read", Arguments: []byte(`{"path":"ussyflow_build_log.md"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "read log"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected cache to execute first build-log read only, got %d", len(tools.calls))
	}
	if len(out.ToolCalls) != 3 {
		t.Fatalf("expected three tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[2].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on third build-log read, got %q", out.ToolCalls[2].Result.Error)
	}
}

func TestRunnerBlocksFourthSpecReadInSameRun(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.read", Arguments: []byte(`{"path":"ussyflow_spec.md"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.read", Arguments: []byte(`{"path":"ussyflow_spec.md"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "fs.read", Arguments: []byte(`{"path":"ussyflow_spec.md"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "4", Name: "fs.read", Arguments: []byte(`{"path":"ussyflow_spec.md"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "read spec"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected first read only due to result cache + guard, got %d executions", len(tools.calls))
	}
	if len(out.ToolCalls) != 4 {
		t.Fatalf("expected four tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[3].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on fourth spec read, got %q", out.ToolCalls[3].Result.Error)
	}
}

func TestRunnerBlocksFourthSpecReadWithFileAliasInSameRun(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.read", Arguments: []byte(`{"file":"ussyflow_spec.md"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "fs.read", Arguments: []byte(`{"file":"ussyflow_spec.md"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "fs.read", Arguments: []byte(`{"file":"ussyflow_spec.md"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "4", Name: "fs.read", Arguments: []byte(`{"file":"ussyflow_spec.md"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "read spec"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected first read only due to result cache + guard, got %d executions", len(tools.calls))
	}
	if len(out.ToolCalls) != 4 {
		t.Fatalf("expected four tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[3].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on fourth spec read, got %q", out.ToolCalls[3].Result.Error)
	}
}

func TestRunnerBlocksRepeatedAgentMessageSendForSameTask(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "agent.message.send", Arguments: []byte(`{"to_agent_id":"ussyflow_builder","task_id":"A2","message":"do task"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "agent.message.send", Arguments: []byte(`{"to_agent_id":"ussyflow_builder","task_id":"A2","message":"do task"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "agent.message.send", Arguments: []byte(`{"to_agent_id":"ussyflow_builder","task_id":"A2","message":"do task"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "dispatch once"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected first dispatch only, got %d executions", len(tools.calls))
	}
	if len(out.ToolCalls) != 3 {
		t.Fatalf("expected three tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[1].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on second dispatch, got %q", out.ToolCalls[1].Result.Error)
	}
	if !strings.Contains(out.ToolCalls[2].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on third dispatch, got %q", out.ToolCalls[2].Result.Error)
	}
}

func TestRunnerBlocksRepeatedAgentMessageSendWithLegacyAliases(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "agent.message.send", Arguments: []byte(`{"agent_id":"ussyflow_builder","task_id":"A2","content":"do task"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "agent.message.send", Arguments: []byte(`{"agent_id":"ussyflow_builder","task_id":"A2","content":"do task"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "dispatch once"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected first dispatch only, got %d executions", len(tools.calls))
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected two tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[1].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on second dispatch, got %q", out.ToolCalls[1].Result.Error)
	}
}

func TestRunnerBlocksRepeatedAgentMessageSendForNormalizedTaskID(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "agent.message.send", Arguments: []byte(`{"to_agent_id":"ussyflow_builder","task_id":"A2","message":"do task"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "agent.message.send", Arguments: []byte(`{"to_agent_id":"ussyflow_builder","task_id":"A2-v2","message":"do task"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "dispatch once"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected first dispatch only, got %d executions", len(tools.calls))
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected two tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[1].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on normalized task id retry, got %q", out.ToolCalls[1].Result.Error)
	}
}

func TestRunnerBlocksThirdAgentInboxPollForSameAgent(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "agent.message.inbox", Arguments: []byte(`{"agent_id":"default","limit":50}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "agent.message.inbox", Arguments: []byte(`{"agent_id":"default","limit":50}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "agent.message.inbox", Arguments: []byte(`{"agent_id":"default","limit":50}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "check inbox"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected first poll only due to result cache + guard, got %d executions", len(tools.calls))
	}
	if len(out.ToolCalls) != 3 {
		t.Fatalf("expected three tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[2].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on third poll, got %q", out.ToolCalls[2].Result.Error)
	}
}

func TestRunnerBlocksRepeatedAgentRunForSameTask(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "agent.run", Arguments: []byte(`{"agent_id":"ussyflow_architect","task_id":"A1","message":"do a1"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "agent.run", Arguments: []byte(`{"agent_id":"ussyflow_architect","task_id":"A1","message":"do a1"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "run subagents"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected first agent.run only, got %d executions", len(tools.calls))
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected two tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[1].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on second agent.run, got %q", out.ToolCalls[1].Result.Error)
	}
}

func TestRunnerBlocksRepeatedAgentRunForNormalizedTaskID(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "agent.run", Arguments: []byte(`{"agent_id":"ussyflow_builder","task_id":"A2","message":"do a2"}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "agent.run", Arguments: []byte(`{"agent_id":"ussyflow_builder","task_id":"A2-v2","message":"do a2"}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "run subagents"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected first agent.run only, got %d executions", len(tools.calls))
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected two tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[1].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on normalized task id retry, got %q", out.ToolCalls[1].Result.Error)
	}
}

func TestRunnerFinalizesWithModelWhenToolCapReached(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","nmap -sT 127.0.0.1"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","nmap -sT localhost"]}`)}}},
		{FinalText: "Nmap completed. localhost is up and tcp/8080 is open."},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Output: `{"exit_code":0,"stdout":"PORT\n8080/tcp open http-proxy\n","stderr":""}`},
	}}

	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 1}
	out, err := runner.Run(context.Background(), RunInput{
		AgentID:       "agent-red",
		RunID:         "run-cap-1",
		Message:       "run nmap on localhost",
		Messages:      []ChatMessage{{Role: "user", Content: "scan localhost"}},
		AllowedTools:  []string{"shell.exec", "fs.read"},
		ToolTimeoutMS: 2500,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "Nmap completed. localhost is up and tcp/8080 is open." {
		t.Fatalf("unexpected finalized text: %q", out.FinalText)
	}
	if len(model.reqs) != 3 {
		t.Fatalf("expected third model request for finalization, got %d requests", len(model.reqs))
	}
	if len(model.reqs[2].AllowedTools) != 0 {
		t.Fatalf("expected finalize request to disable tools, got %#v", model.reqs[2].AllowedTools)
	}
	if model.reqs[2].AgentID != "agent-red" || model.reqs[2].RunID != "run-cap-1" {
		t.Fatalf("expected finalize request to preserve run metadata, got agent=%q run=%q", model.reqs[2].AgentID, model.reqs[2].RunID)
	}
	if model.reqs[2].ToolTimeoutMS != 2500 {
		t.Fatalf("expected finalize request timeout passthrough, got %d", model.reqs[2].ToolTimeoutMS)
	}
	if len(model.reqs[2].Messages) != 1 || model.reqs[2].Messages[0].Content != "scan localhost" {
		t.Fatalf("expected finalize request to include message history, got %+v", model.reqs[2].Messages)
	}
}

func TestRunnerAppliesPerToolTimeout(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{{ToolCalls: []ToolCallRequest{{ID: "1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}}, {FinalText: "done"}}}
	runner := Runner{Model: model, ToolExecutor: slowToolExecutor{}, MaxToolIterations: 4}

	start := time.Now()
	out, err := runner.Run(context.Background(), RunInput{Message: "timeout", ToolTimeoutMS: 20})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("expected one tool call record, got %d", len(out.ToolCalls))
	}
	if out.ToolCalls[0].Result.Error == "" {
		t.Fatal("expected timeout error in tool result")
	}
	if !strings.Contains(strings.ToLower(out.ToolCalls[0].Result.Error), "timeout") {
		t.Fatalf("expected structured timeout error, got %q", out.ToolCalls[0].Result.Error)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("expected run to finish quickly due to timeout")
	}
}

func TestRunnerNormalizesDuplicateToolCallIDs(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{
			{ID: "tool-json-1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)},
			{ID: "tool-json-1", Name: "fs.read", Arguments: []byte(`{"path":"README.md"}`)},
		}},
		{FinalText: "done"},
	}}

	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 4}
	out, err := runner.Run(context.Background(), RunInput{Message: "inspect files"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(out.ToolCalls))
	}

	firstID := out.ToolCalls[0].Request.ID
	secondID := out.ToolCalls[1].Request.ID
	if firstID != "tool-json-1" {
		t.Fatalf("expected first ID to be preserved, got %q", firstID)
	}
	if secondID == firstID {
		t.Fatalf("expected second call ID to be rewritten, got duplicate %q", secondID)
	}
	if !strings.HasPrefix(secondID, "tool-json-1-") {
		t.Fatalf("expected rewritten ID to keep base prefix, got %q", secondID)
	}

	if out.ToolCalls[0].Result.ID != firstID || out.ToolCalls[1].Result.ID != secondID {
		t.Fatalf("expected results to use normalized IDs, got %+v", out.ToolCalls)
	}

	if len(model.reqs) != 2 || len(model.reqs[1].ToolResults) != 2 {
		t.Fatalf("expected two tool results in follow-up model request, got %#v", model.reqs)
	}
	if model.reqs[1].ToolResults[0].ID != firstID || model.reqs[1].ToolResults[1].ID != secondID {
		t.Fatalf("unexpected tool result IDs passed to model: %+v", model.reqs[1].ToolResults)
	}
}

func TestRunnerInvokesOnToolCallForEachRecord(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "tool-1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "tool-2", Name: "fs.read", Arguments: []byte(`{"path":"README.md"}`)}}},
		{FinalText: "done"},
	}}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 8}

	notifications := make([]ToolCallRecord, 0, 2)
	out, err := runner.Run(context.Background(), RunInput{
		Message: "inspect project",
		OnToolCall: func(rec ToolCallRecord) error {
			notifications = append(notifications, rec)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %d", len(out.ToolCalls))
	}
	if len(notifications) != 2 {
		t.Fatalf("expected two tool notifications, got %d", len(notifications))
	}
	if notifications[0].Request.Name != "fs.list" || notifications[1].Request.Name != "fs.read" {
		t.Fatalf("unexpected notification order: %+v", notifications)
	}
}

func TestRunnerContinuesWhenOnToolCallReturnsError(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "tool-1", Name: "fs.list", Arguments: []byte(`{"path":"."}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "tool-2", Name: "fs.read", Arguments: []byte(`{"path":"README.md"}`)}}},
		{FinalText: "done"},
	}}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 8}

	callbackCalls := 0
	out, err := runner.Run(context.Background(), RunInput{
		Message: "inspect project",
		OnToolCall: func(rec ToolCallRecord) error {
			callbackCalls++
			return errors.New("chatstore append failed for " + rec.Request.ID)
		},
	})
	if err != nil {
		t.Fatalf("expected callback failures to be non-fatal, got %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %d", len(out.ToolCalls))
	}
	if callbackCalls != 2 {
		t.Fatalf("expected callback invoked once per record, got %d", callbackCalls)
	}
	for _, rec := range out.ToolCalls {
		if !strings.Contains(rec.CallbackErr, "chatstore append failed") {
			t.Fatalf("expected callback error captured in record, got %+v", rec)
		}
	}
}

func TestRunnerBreaksNoProgressToolLoopBeforeCap(t *testing.T) {
	// After the shell.exec repetition cap (3), identical shell commands are blocked.
	// After 2 all-blocked iterations the runner fast-finalizes.
	// Sequence: iter1=fresh exec, iter2=cached (no progress), iter3=cached (no progress),
	// iter4=repetition-blocked (allBlocked=1), iter5=repetition-blocked (allBlocked=2) → finalize.
	// But with repeatedNoProgressLoopCapTrigger=3, generic no-progress fires at iter4.
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","pip install flask requests"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","pip install flask requests"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","pip install flask requests"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "4", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","pip install flask requests"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "5", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","pip install flask requests"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "6", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","pip install flask requests"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "7", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","pip install flask requests"]}`)}}},
		{FinalText: "The venv is ready and flask/requests are already installed."},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Output: "Requirement already satisfied: flask\nRequirement already satisfied: requests"},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 24}

	out, err := runner.Run(context.Background(), RunInput{Message: "please install useful tools into your .venv"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	// The runner should finalize before the model's 8th response due to no-progress / all-blocked detection
	if out.FinalText == "" {
		t.Fatalf("expected non-empty final text")
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected only one real tool execution, got %d", len(tools.calls))
	}
	// With shell.exec cap=3 and no-progress cap=3, the run stops well before 8 model calls
	if len(model.reqs) > 8 {
		t.Fatalf("expected no more than 8 model requests, got %d", len(model.reqs))
	}
}

func TestRunnerDefaultToolSettingsAreRaisedForLongTasks(t *testing.T) {
	if DefaultToolIterationCap < 16 {
		t.Fatalf("expected higher default tool iteration cap, got %d", DefaultToolIterationCap)
	}
	if DefaultToolTimeout < 120*time.Second {
		t.Fatalf("expected higher default tool timeout, got %s", DefaultToolTimeout)
	}
}

func TestRunnerEntersRecoveryModeAfterTwoFailures(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","tailscale status --json"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","tailscale status --json --peers"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","ps aux | grep tailscaled"]}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Error: "internal.error (shell.exec): socket missing"},
		"2": {ID: "2", Error: "internal.error (shell.exec): socket missing"},
		"3": {ID: "3", Output: "tailscaled running"},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 120}

	out, err := runner.Run(context.Background(), RunInput{Message: "fix tailscaled"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(model.reqs) < 3 {
		t.Fatalf("expected at least three model requests, got %d", len(model.reqs))
	}
	if !strings.Contains(model.reqs[2].SystemPrompt, "ERROR_RECOVERY_MODE") {
		t.Fatalf("expected recovery mode directive after two failures, got %q", model.reqs[2].SystemPrompt)
	}
}

func TestRunnerAsksUserGuidanceAfterThreeMoreFailures(t *testing.T) {
	responses := make([]ModelResponse, 0, 6)
	toolResults := make(map[string]ToolCallResult, 6)
	for i := 1; i <= 6; i++ {
		id := "call-" + strconv.Itoa(i)
		cmd := `{"command":"bash","args":["-lc","tailscale status # attempt ` + strconv.Itoa(i) + `"]}`
		responses = append(responses, ModelResponse{ToolCalls: []ToolCallRequest{{ID: id, Name: "shell.exec", Arguments: []byte(cmd)}}})
		toolResults[id] = ToolCallResult{ID: id, Output: "stderr: connect unix /var/run/tailscale/tailscaled.sock", Error: "internal.error (shell.exec): socket missing"}
	}

	model := &mockModel{responses: responses}
	tools := &mockTools{results: toolResults}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 120}

	out, err := runner.Run(context.Background(), RunInput{Message: "please fix tailscaled"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(out.FinalText, "need your help to continue") {
		t.Fatalf("expected user-guidance finalization, got %q", out.FinalText)
	}
	if !strings.Contains(out.FinalText, "What I tried") {
		t.Fatalf("expected attempted steps in finalization, got %q", out.FinalText)
	}
	if len(out.ToolCalls) != 5 {
		t.Fatalf("expected 5 tool calls before user-guidance escalation, got %d", len(out.ToolCalls))
	}
	if len(model.reqs) != 5 {
		t.Fatalf("expected runner to stop without extra model call after escalation, got %d", len(model.reqs))
	}
}

func TestRunnerRecoversWithToolSummaryWhenModelCallFailsMidRun(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","tailscale status"]}`)}}},
	}}
	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Error: "exit status 1", Output: "failed to connect to local tailscaled: socket missing"},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 120}

	out, err := runner.Run(context.Background(), RunInput{Message: "help me connect tailscale"})
	if err != nil {
		t.Fatalf("expected graceful recovery from model failure, got %v", err)
	}
	if !strings.Contains(out.FinalText, "model/API error") {
		t.Fatalf("expected model error context in final text, got %q", out.FinalText)
	}
	if !strings.Contains(out.FinalText, "no more responses") {
		t.Fatalf("expected wrapped model error details, got %q", out.FinalText)
	}
	if !strings.Contains(out.FinalText, "failed to connect to local tailscaled") {
		t.Fatalf("expected tool output in fallback summary, got %q", out.FinalText)
	}
}

func TestRunnerRetriesProviderNoChoicesAndRecovers(t *testing.T) {
	model := &noChoicesThenSuccessModel{}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 120}

	out, err := runner.Run(context.Background(), RunInput{Message: "hello"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("expected successful retry final text, got %q", out.FinalText)
	}
	if model.attempts != 3 {
		t.Fatalf("expected 3 model attempts (2 retries then success), got %d", model.attempts)
	}
}

func TestRunnerStopsAfterNoChoicesRetryCap(t *testing.T) {
	model := &alwaysNoChoicesModel{}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 120}

	_, err := runner.Run(context.Background(), RunInput{Message: "hello"})
	if err == nil {
		t.Fatal("expected run to fail after retry cap")
	}
	if model.attempts != 3 {
		t.Fatalf("expected 3 model attempts at retry cap, got %d", model.attempts)
	}
}

func TestRunnerNoChoicesAfterToolsUsesFriendlyRecovery(t *testing.T) {
	model := &toolThenNoChoicesModel{}
	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Output: `{"count":0,"items":[],"limit":8,"mode":"fts","query":"research explored discoveries journal","status":"active"}`},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 120}

	out, err := runner.Run(context.Background(), RunInput{Message: "what did you find"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if strings.Contains(out.FinalText, "model/API error") {
		t.Fatalf("expected friendly no-choices recovery, got %q", out.FinalText)
	}
	if !strings.Contains(out.FinalText, "found no matching entries") {
		t.Fatalf("expected empty-search summary, got %q", out.FinalText)
	}
	if model.attempts != 4 {
		t.Fatalf("expected 1 initial + 3 no-choices attempts, got %d", model.attempts)
	}
}

func TestRunnerRetriesTransientProviderTimeoutAndRecovers(t *testing.T) {
	model := &transientErrorThenSuccessModel{}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 120}

	out, err := runner.Run(context.Background(), RunInput{Message: "hello"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("expected successful retry final text, got %q", out.FinalText)
	}
	if model.attempts != 3 {
		t.Fatalf("expected 3 model attempts (2 retries then success), got %d", model.attempts)
	}
}

func TestRunnerStopsAfterTransientProviderRetryCap(t *testing.T) {
	model := &alwaysTransientErrorModel{}
	runner := Runner{Model: model, ToolExecutor: &mockTools{}, MaxToolIterations: 120}

	_, err := runner.Run(context.Background(), RunInput{Message: "hello"})
	if err == nil {
		t.Fatal("expected run to fail after retry cap")
	}
	if model.attempts != 3 {
		t.Fatalf("expected 3 model attempts at retry cap, got %d", model.attempts)
	}
}

func TestRunnerTreatsStructuredToolOutputErrorsAsFailures(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","curl -fsSL https://tailscale.com/install.sh | sh"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","curl -fsSL https://tailscale.com/install.sh | sh"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","apk add --no-cache openrc"]}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Output: `{"exit_code":127,"stderr":"sh: rc-update: not found","stdout":"Installing Tailscale","error":"exit status 127"}`},
		"2": {ID: "2", Output: `{"exit_code":127,"stderr":"sh: rc-update: not found","stdout":"Installing Tailscale","error":"exit status 127"}`},
		"3": {ID: "3", Output: `{"exit_code":0,"stderr":"","stdout":"OK"}`},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 120}

	out, err := runner.Run(context.Background(), RunInput{Message: "install tailscale"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(model.reqs) < 3 {
		t.Fatalf("expected at least 3 model requests, got %d", len(model.reqs))
	}
	if !strings.Contains(model.reqs[2].SystemPrompt, "ERROR_RECOVERY_MODE") {
		t.Fatalf("expected recovery mode after structured output errors, got %q", model.reqs[2].SystemPrompt)
	}
}

func TestRunnerEscalatesGuidanceForStructuredToolOutputErrors(t *testing.T) {
	// Use distinct shell commands so the shell.exec repetition cap doesn't interfere
	// with the failure recovery escalation path.
	responses := make([]ModelResponse, 0, 6)
	results := make(map[string]ToolCallResult, 6)
	commands := []string{
		`{"command":"bash","args":["-lc","curl -fsSL https://tailscale.com/install.sh | sh"]}`,
		`{"command":"bash","args":["-lc","wget -q https://tailscale.com/install.sh -O- | sh"]}`,
		`{"command":"bash","args":["-lc","apt-get install -y tailscale"]}`,
		`{"command":"bash","args":["-lc","yum install -y tailscale"]}`,
		`{"command":"bash","args":["-lc","apk add tailscale"]}`,
		`{"command":"bash","args":["-lc","snap install tailscale"]}`,
	}
	for i := 0; i < 6; i++ {
		id := "call-" + strconv.Itoa(i+1)
		responses = append(responses, ModelResponse{ToolCalls: []ToolCallRequest{{ID: id, Name: "shell.exec", Arguments: []byte(commands[i])}}})
		results[id] = ToolCallResult{ID: id, Output: `{"exit_code":127,"stderr":"sh: rc-update: not found","stdout":"Installing Tailscale","error":"exit status 127"}`}
	}

	model := &mockModel{responses: responses}
	tools := &mockTools{results: results}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 120}

	out, err := runner.Run(context.Background(), RunInput{Message: "install tailscale via curl"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(out.FinalText, "need your help to continue") {
		t.Fatalf("expected guidance escalation, got %q", out.FinalText)
	}
	if len(out.ToolCalls) != 5 {
		t.Fatalf("expected 5 tool calls before escalation, got %d", len(out.ToolCalls))
	}
}

func TestRunnerEscalatesGuidanceAfterIntermittentFailuresInRecoveryMode(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","cmd1"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","cmd2"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","cmd3"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "4", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","cmd4"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "5", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","cmd5"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "6", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","cmd6"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "7", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","cmd7"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "8", Name: "shell.exec", Arguments: []byte(`{"command":"bash","args":["-lc","cmd8"]}`)}}},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Error: "exit status 1", Output: "first failure"},
		"2": {ID: "2", Error: "exit status 1", Output: "second failure"},
		"3": {ID: "3", Output: "success"},
		"4": {ID: "4", Error: "exit status 1", Output: "third failure after recovery"},
		"5": {ID: "5", Output: "success"},
		"6": {ID: "6", Error: "exit status 1", Output: "fourth failure after recovery"},
		"7": {ID: "7", Output: "success"},
		"8": {ID: "8", Error: "exit status 1", Output: "fifth failure after recovery"},
	}}

	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 120}
	out, err := runner.Run(context.Background(), RunInput{Message: "install tooling"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(out.FinalText, "need your help to continue") {
		t.Fatalf("expected guidance escalation after intermittent failures, got %q", out.FinalText)
	}
	if len(out.ToolCalls) != 8 {
		t.Fatalf("expected escalation after 8 tool calls, got %d", len(out.ToolCalls))
	}
}

func TestRunnerAsksPermissionToAddAllowedDomainAfterRepeatedNetworkDeny(t *testing.T) {
	responses := make([]ModelResponse, 0, 6)
	toolResults := make(map[string]ToolCallResult, 6)
	for i := 1; i <= 6; i++ {
		id := "call-" + strconv.Itoa(i)
		args := `{"method":"POST","url":"https://api.perplexity.ai/chat/completions"}`
		responses = append(responses, ModelResponse{ToolCalls: []ToolCallRequest{{ID: id, Name: "http.request", Arguments: []byte(args)}}})
		toolResults[id] = ToolCallResult{ID: id, Error: `internal.error (http.request): host "api.perplexity.ai" is not in network.allowed_domains`}
	}

	model := &mockModel{responses: responses}
	tools := &mockTools{results: toolResults}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 120}

	out, err := runner.Run(context.Background(), RunInput{Message: "use the perplexity skill to search the ussyverse"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(out.FinalText, "need your permission first") {
		t.Fatalf("expected permission prompt, got %q", out.FinalText)
	}
	if !strings.Contains(out.FinalText, "api.perplexity.ai") {
		t.Fatalf("expected blocked host guidance, got %q", out.FinalText)
	}
	if !strings.Contains(out.FinalText, "config.set") {
		t.Fatalf("expected config.set guidance, got %q", out.FinalText)
	}
}

func TestRunnerBlocksRepeatedShellExecLoopOnSameCommand(t *testing.T) {
	// shell.exec with the same command+args should be blocked after shellExecRepetitionCap (3) calls.
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "shell.exec", Arguments: []byte(`{"command":"npm","args":["run","build"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "shell.exec", Arguments: []byte(`{"command":"npm","args":["run","build"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "shell.exec", Arguments: []byte(`{"command":"npm","args":["run","build"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "4", Name: "shell.exec", Arguments: []byte(`{"command":"npm","args":["run","build"]}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Output: "build failed: error TS2345"},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "build project"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	// First call executes, second is cached, third is cached, fourth is repetition-blocked.
	if len(tools.calls) != 1 {
		t.Fatalf("expected only one real shell.exec execution, got %d", len(tools.calls))
	}
	if len(out.ToolCalls) != 4 {
		t.Fatalf("expected four tool call records, got %d", len(out.ToolCalls))
	}
	if !strings.Contains(out.ToolCalls[3].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on 4th shell.exec, got %q", out.ToolCalls[3].Result.Error)
	}
}

func TestRunnerBlocksShellExecButAllowsDifferentCommands(t *testing.T) {
	// Different shell commands should NOT be collapsed — each has its own counter.
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "shell.exec", Arguments: []byte(`{"command":"npm","args":["run","build"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "shell.exec", Arguments: []byte(`{"command":"npm","args":["run","test"]}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "shell.exec", Arguments: []byte(`{"command":"npm","args":["run","lint"]}`)}}},
		{FinalText: "all passed"},
	}}

	tools := &mockTools{}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "run build, test, lint"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "all passed" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	// All three are different commands, so all should execute.
	if len(tools.calls) != 3 {
		t.Fatalf("expected three real executions for different commands, got %d", len(tools.calls))
	}
}

func TestRunnerBlocksRepeatedMemoryWriteLoopWithMinorArgVariants(t *testing.T) {
	model := &mockModel{responses: []ModelResponse{
		{ToolCalls: []ToolCallRequest{{ID: "1", Name: "memory.write", Arguments: []byte(`{"kind":"journal","title":"Exploration Journal Entry 7","content":"Agency and consciousness notes","confidence":0.85}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "2", Name: "memory.write", Arguments: []byte(`{"title":"Exploration Journal Entry 7","content":"Agency and consciousness notes","kind":"journal","confidence":0.90}`)}}},
		{ToolCalls: []ToolCallRequest{{ID: "3", Name: "memory.write", Arguments: []byte(`{"kind":"journal","title":"Exploration Journal Entry 7","content":"Agency and consciousness notes","keywords":["agency","consciousness"]}`)}}},
		{FinalText: "done"},
	}}

	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Output: `{"written":true,"id":"mem_1"}`},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

	out, err := runner.Run(context.Background(), RunInput{Message: "journal this insight"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if out.FinalText != "done" {
		t.Fatalf("unexpected final text: %q", out.FinalText)
	}
	if len(out.ToolCalls) != 3 {
		t.Fatalf("expected three tool call records, got %d", len(out.ToolCalls))
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected only one real memory.write execution, got %d", len(tools.calls))
	}
	if !strings.Contains(out.ToolCalls[2].Result.Error, "repetition detected") {
		t.Fatalf("expected repetition guard on third memory.write, got %q", out.ToolCalls[2].Result.Error)
	}
}

func TestRunnerNormalizesTaskIDSuffixesForRepetition(t *testing.T) {
	// Task IDs with -retry, -v2, -continue, -fix, -redo, -attempt3 suffixes
	// should all normalize to the same base, so the second call is blocked.
	suffixes := []string{"-retry", "-v2", "-continue", "-fix", "-redo", "-attempt3"}
	for _, suffix := range suffixes {
		t.Run(suffix, func(t *testing.T) {
			variantID := "build-frontend" + suffix
			model := &mockModel{responses: []ModelResponse{
				{ToolCalls: []ToolCallRequest{{ID: "1", Name: "agent.run", Arguments: []byte(`{"agent_id":"builder","task_id":"build-frontend","message":"build it"}`)}}},
				{ToolCalls: []ToolCallRequest{{ID: "2", Name: "agent.run", Arguments: []byte(`{"agent_id":"builder","task_id":"` + variantID + `","message":"build it"}`)}}},
				{FinalText: "done"},
			}}

			tools := &mockTools{}
			runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 20}

			out, err := runner.Run(context.Background(), RunInput{Message: "run builder"})
			if err != nil {
				t.Fatalf("run failed: %v", err)
			}
			if len(tools.calls) != 1 {
				t.Fatalf("suffix %q: expected first dispatch only, got %d executions", suffix, len(tools.calls))
			}
			if !strings.Contains(out.ToolCalls[1].Result.Error, "repetition detected") {
				t.Fatalf("suffix %q: expected repetition guard, got %q", suffix, out.ToolCalls[1].Result.Error)
			}
		})
	}
}

func TestRunnerFastFinalizesWhenAllToolCallsBlockedForTwoIterations(t *testing.T) {
	// When every tool call in an iteration is repetition-blocked for 2+ consecutive
	// iterations, the runner should fast-finalize without waiting for the full no-progress cap.
	// Setup: 1 real exec, then iterations of the same blocked call. After shell.exec cap (3),
	// calls 4-7 are blocked. Iterations 4&5 are all-blocked → fast finalize.
	responses := make([]ModelResponse, 0, 10)
	for i := 1; i <= 10; i++ {
		id := strconv.Itoa(i)
		responses = append(responses, ModelResponse{
			ToolCalls: []ToolCallRequest{{ID: id, Name: "shell.exec", Arguments: []byte(`{"command":"make","args":["deploy"]}`)}},
		})
	}
	responses = append(responses, ModelResponse{FinalText: "should not reach this"})

	model := &mockModel{responses: responses}
	tools := &mockTools{results: map[string]ToolCallResult{
		"1": {ID: "1", Output: "deploy: permission denied"},
	}}
	runner := Runner{Model: model, ToolExecutor: tools, MaxToolIterations: 50}

	out, err := runner.Run(context.Background(), RunInput{Message: "deploy the app"})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	// Should finalize, not reach the model's final text
	if out.FinalText == "should not reach this" {
		t.Fatalf("runner did not fast-finalize; reached model's scripted final text")
	}
	if out.FinalText == "" {
		t.Fatalf("expected non-empty finalized text")
	}
	// Should not have consumed all 10 model calls — fast finalization cuts it short
	if len(model.reqs) > 7 {
		t.Fatalf("expected early termination, but used %d model requests", len(model.reqs))
	}
}
