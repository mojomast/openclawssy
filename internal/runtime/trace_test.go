package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"openclawssy/internal/agent"
)

func TestSummarizeToolExecutionFsWrite(t *testing.T) {
	summary := summarizeToolExecution("fs.write", `{"path":"templates/index.html","bytes":1200,"lines":42}`, "")
	if summary != "wrote 42 line(s) to templates/index.html" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestSummarizeToolExecutionError(t *testing.T) {
	summary := summarizeToolExecution("fs.read", "", "permission denied")
	if summary != "error: permission denied" {
		t.Fatalf("unexpected error summary: %q", summary)
	}
}

func TestRecordToolExecutionAddsSummaryToTrace(t *testing.T) {
	collector := newRunTraceCollector("run_1", "session_1", "dashboard", "write file")
	collector.RecordToolExecution([]agent.ToolCallRecord{{
		Request: agent.ToolCallRequest{ID: "tool-json-1", Name: "fs.write"},
		Result:  agent.ToolCallResult{ID: "tool-json-1", Output: `{"path":"Dockerfile","bytes":320,"lines":12}`},
	}})

	snapshot := collector.Snapshot()
	items, ok := snapshot["tool_execution_results"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one tool trace item, got %#v", snapshot["tool_execution_results"])
	}
	entry, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected trace entry shape: %#v", items[0])
	}
	if entry["summary"] != "wrote 12 line(s) to Dockerfile" {
		t.Fatalf("expected summary in trace entry, got %#v", entry)
	}
}

func TestRecordToolExecutionIncludesCallbackErrorInTrace(t *testing.T) {
	collector := newRunTraceCollector("run_2", "session_2", "dashboard", "list files")
	collector.RecordToolExecution([]agent.ToolCallRecord{{
		Request:     agent.ToolCallRequest{ID: "tool-json-2", Name: "fs.list", Arguments: []byte(`{"path":"."}`)},
		Result:      agent.ToolCallResult{ID: "tool-json-2", Output: `{"entries":["README.md"]}`},
		CallbackErr: "runtime: append tool message: permission denied",
	}})

	snapshot := collector.Snapshot()
	items, ok := snapshot["tool_execution_results"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one tool trace item, got %#v", snapshot["tool_execution_results"])
	}
	entry, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected trace entry shape: %#v", items[0])
	}
	if entry["callback_error"] != "runtime: append tool message: permission denied" {
		t.Fatalf("expected callback_error in trace entry, got %#v", entry)
	}
}

func TestRecordToolExecutionIncludesDurationInTrace(t *testing.T) {
	collector := newRunTraceCollector("run_d", "session_d", "dashboard", "work")
	start := time.Now().Add(-125 * time.Millisecond)
	end := time.Now()
	collector.RecordToolExecution([]agent.ToolCallRecord{{
		Request:     agent.ToolCallRequest{ID: "tool-json-3", Name: "fs.read", Arguments: []byte(`{"path":"a.txt"}`)},
		Result:      agent.ToolCallResult{ID: "tool-json-3", Output: `{"path":"a.txt","content":"x"}`},
		StartedAt:   start,
		CompletedAt: end,
	}})

	snapshot := collector.Snapshot()
	items, ok := snapshot["tool_execution_results"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one tool trace item, got %#v", snapshot["tool_execution_results"])
	}
	entry, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected trace entry shape: %#v", items[0])
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Fatalf("expected duration_ms in trace entry, got %#v", entry)
	}
}

func TestSummarizeToolExecutionShellPlainOutput(t *testing.T) {
	summary := summarizeToolExecution("shell.exec", "ok\nwarn", "")
	if summary != "shell command completed" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestSummarizeToolExecutionShellJSONOutput(t *testing.T) {
	summary := summarizeToolExecution("shell.exec", `{"stdout":"ok","stderr":"warn","exit_code":1}`, "")
	if summary != "shell command completed" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestRecordThinkingPersistsThinkingFields(t *testing.T) {
	collector := newRunTraceCollector("run_3", "session_3", "dashboard", "hello")
	collector.RecordThinking("redacted notes", true)

	snapshot := collector.Snapshot()
	if snapshot["thinking"] != "redacted notes" {
		t.Fatalf("expected thinking in trace snapshot, got %#v", snapshot["thinking"])
	}
	if snapshot["thinking_present"] != true {
		t.Fatalf("expected thinking_present=true, got %#v", snapshot["thinking_present"])
	}
}

func TestRecordModelUsagePersistsCachedTokenStats(t *testing.T) {
	collector := newRunTraceCollector("run_usage", "session_usage", "dashboard", "hello")
	collector.RecordModelInput("hello", 1234, false, `{"model":"glm-4.7"}`)
	collector.RecordModelUsage(1200, 800, 300, 1500)

	snapshot := collector.Snapshot()
	items, ok := snapshot["model_usage"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one model usage entry, got %#v", snapshot["model_usage"])
	}
	entry, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected model usage entry shape: %#v", items[0])
	}
	if entry["cached_prompt_tokens"] != float64(800) {
		t.Fatalf("expected cached_prompt_tokens=800, got %#v", entry["cached_prompt_tokens"])
	}
	if entry["prompt_tokens"] != float64(1200) {
		t.Fatalf("expected prompt_tokens=1200, got %#v", entry["prompt_tokens"])
	}
	if entry["total_tokens"] != float64(1500) {
		t.Fatalf("expected total_tokens=1500, got %#v", entry["total_tokens"])
	}
}

func TestIntValueParsesCommonNumericRepresentations(t *testing.T) {
	if got := intValue(12); got != 12 {
		t.Fatalf("expected int 12, got %d", got)
	}
	if got := intValue(12.9); got != 12 {
		t.Fatalf("expected float truncation to 12, got %d", got)
	}
	if got := intValue("44"); got != 44 {
		t.Fatalf("expected numeric string parse to 44, got %d", got)
	}
	if got := intValue(json.Number("91")); got != 91 {
		t.Fatalf("expected json.Number parse to 91, got %d", got)
	}
	if got := intValue("not-a-number"); got != 0 {
		t.Fatalf("expected invalid numeric string to parse as 0, got %d", got)
	}
}

func TestIntValueReturnsZeroForNilAndEmptyInputs(t *testing.T) {
	if got := intValue(nil); got != 0 {
		t.Fatalf("expected nil to parse as 0, got %d", got)
	}
	if got := intValue(""); got != 0 {
		t.Fatalf("expected empty string to parse as 0, got %d", got)
	}
	if got := intValue("   "); got != 0 {
		t.Fatalf("expected whitespace string to parse as 0, got %d", got)
	}
}

func TestTruncateSummaryAndContextHelpers(t *testing.T) {
	if got := truncateSummary("abcdef", 3); got != "abc" {
		t.Fatalf("expected hard truncation for max<=3, got %q", got)
	}
	if got := truncateSummary("abcdef", 5); got != "ab..." {
		t.Fatalf("expected ellipsis truncation, got %q", got)
	}

	collector := newRunTraceCollector("run_ctx", "", "", "")
	ctx := withRunTraceCollector(context.Background(), collector)
	if got := runTraceCollectorFromContext(ctx); got != collector {
		t.Fatalf("expected collector from context, got %+v", got)
	}
	if got := runTraceCollectorFromContext(nil); got != nil {
		t.Fatalf("expected nil collector from nil context, got %+v", got)
	}
}

func TestIntValueSupportsAdditionalNumericTypes(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{in: int8(7), want: 7},
		{in: int16(8), want: 8},
		{in: int32(9), want: 9},
		{in: int64(10), want: 10},
		{in: uint(11), want: 11},
		{in: uint8(12), want: 12},
		{in: uint16(13), want: 13},
		{in: uint32(14), want: 14},
		{in: uint64(15), want: 15},
		{in: float32(16.9), want: 16},
		{in: json.Number("17.8"), want: 17},
		{in: "18.7", want: 18},
		{in: "<nil>", want: 0},
	}
	for _, tc := range cases {
		if got := intValue(tc.in); got != tc.want {
			t.Fatalf("intValue(%#v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
