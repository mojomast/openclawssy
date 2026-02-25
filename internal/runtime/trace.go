package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"openclawssy/internal/agent"
)

type runTraceEnvelope struct {
	RunID                string                   `json:"run_id"`
	SessionID            string                   `json:"session_id,omitempty"`
	Channel              string                   `json:"channel,omitempty"`
	InputMessageHash     string                   `json:"input_message_hash"`
	Thinking             string                   `json:"thinking,omitempty"`
	ThinkingPresent      bool                     `json:"thinking_present,omitempty"`
	ModelInputs          []modelInputTrace        `json:"model_inputs,omitempty"`
	ModelUsage           []modelUsageTrace        `json:"model_usage,omitempty"`
	ExtractedToolCalls   []toolExtractionTrace    `json:"extracted_tool_calls,omitempty"`
	ToolExecutionResults []toolExecutionResultLog `json:"tool_execution_results,omitempty"`
}

type modelInputTrace struct {
	Iteration       int    `json:"iteration"`
	Message         string `json:"message"`
	PromptLength    int    `json:"prompt_length"`
	HistoryInjected bool   `json:"history_injected"`
	RequestJSON     string `json:"request_json"`
}

type modelUsageTrace struct {
	Iteration          int `json:"iteration"`
	PromptTokens       int `json:"prompt_tokens,omitempty"`
	CachedPromptTokens int `json:"cached_prompt_tokens,omitempty"`
	CompletionTokens   int `json:"completion_tokens,omitempty"`
	TotalTokens        int `json:"total_tokens,omitempty"`
}

type toolExtractionTrace struct {
	RawSnippet      string `json:"raw_snippet"`
	ParsedToolName  string `json:"parsed_tool_name,omitempty"`
	ParsedArguments string `json:"parsed_arguments,omitempty"`
	Accepted        bool   `json:"accepted"`
	Reason          string `json:"reason,omitempty"`
}

type toolExecutionResultLog struct {
	Tool          string `json:"tool"`
	ToolCallID    string `json:"tool_call_id,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	Arguments     string `json:"arguments,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Output        string `json:"output,omitempty"`
	Error         string `json:"error,omitempty"`
	CallbackError string `json:"callback_error,omitempty"`
}

type runTraceCollector struct {
	mu      sync.Mutex
	env     runTraceEnvelope
	current int
}

func newRunTraceCollector(runID, sessionID, channel, message string) *runTraceCollector {
	hash := sha256.Sum256([]byte(message))
	return &runTraceCollector{
		env: runTraceEnvelope{
			RunID:            strings.TrimSpace(runID),
			SessionID:        strings.TrimSpace(sessionID),
			Channel:          strings.TrimSpace(channel),
			InputMessageHash: hex.EncodeToString(hash[:]),
		},
	}
}

func (c *runTraceCollector) RecordModelInput(message string, promptLength int, historyInjected bool, requestJSON string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current++
	c.env.ModelInputs = append(c.env.ModelInputs, modelInputTrace{
		Iteration:       c.current,
		Message:         message,
		PromptLength:    promptLength,
		HistoryInjected: historyInjected,
		RequestJSON:     requestJSON,
	})
}

func (c *runTraceCollector) RecordModelUsage(promptTokens, cachedPromptTokens, completionTokens, totalTokens int) {
	if c == nil {
		return
	}
	if promptTokens <= 0 && cachedPromptTokens <= 0 && completionTokens <= 0 && totalTokens <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	iteration := c.current
	if iteration <= 0 {
		iteration = len(c.env.ModelInputs) + 1
	}
	c.env.ModelUsage = append(c.env.ModelUsage, modelUsageTrace{
		Iteration:          iteration,
		PromptTokens:       promptTokens,
		CachedPromptTokens: cachedPromptTokens,
		CompletionTokens:   completionTokens,
		TotalTokens:        totalTokens,
	})
}

func (c *runTraceCollector) RecordToolExtraction(rawSnippet, parsedTool string, parsedArguments json.RawMessage, accepted bool, reason string) {
	if c == nil {
		return
	}
	entry := toolExtractionTrace{
		RawSnippet:     strings.TrimSpace(rawSnippet),
		ParsedToolName: strings.TrimSpace(parsedTool),
		Accepted:       accepted,
		Reason:         strings.TrimSpace(reason),
	}
	if len(parsedArguments) > 0 {
		entry.ParsedArguments = strings.TrimSpace(string(parsedArguments))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.env.ExtractedToolCalls = append(c.env.ExtractedToolCalls, entry)
}

func (c *runTraceCollector) RecordToolExecution(records []agent.ToolCallRecord) {
	if c == nil || len(records) == 0 {
		return
	}
	items := make([]toolExecutionResultLog, 0, len(records))
	for _, rec := range records {
		durationMS := rec.CompletedAt.Sub(rec.StartedAt).Milliseconds()
		if durationMS < 0 {
			durationMS = 0
		}
		item := toolExecutionResultLog{
			Tool:          strings.TrimSpace(rec.Request.Name),
			ToolCallID:    strings.TrimSpace(rec.Request.ID),
			DurationMS:    durationMS,
			Summary:       summarizeToolExecution(rec.Request.Name, rec.Result.Output, rec.Result.Error),
			Output:        strings.TrimSpace(rec.Result.Output),
			Error:         strings.TrimSpace(rec.Result.Error),
			CallbackError: strings.TrimSpace(rec.CallbackErr),
		}
		if len(rec.Request.Arguments) > 0 {
			item.Arguments = strings.TrimSpace(string(rec.Request.Arguments))
		}
		items = append(items, item)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.env.ToolExecutionResults = append(c.env.ToolExecutionResults, items...)
}

func (c *runTraceCollector) RecordThinking(thinking string, thinkingPresent bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.env.Thinking = strings.TrimSpace(thinking)
	c.env.ThinkingPresent = thinkingPresent
}

func summarizeToolExecution(toolName, output, errText string) string {
	errText = strings.TrimSpace(errText)
	if errText != "" {
		return "error: " + truncateSummary(errText, 180)
	}

	toolName = strings.TrimSpace(toolName)
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}

	parsed := map[string]any{}
	_ = json.Unmarshal([]byte(output), &parsed)

	switch toolName {
	case "fs.write":
		path := strings.TrimSpace(fmt.Sprintf("%v", parsed["path"]))
		lines := intValue(parsed["lines"])
		if path != "" && lines >= 0 {
			return fmt.Sprintf("wrote %d line(s) to %s", lines, path)
		}
	case "fs.edit":
		path := strings.TrimSpace(fmt.Sprintf("%v", parsed["path"]))
		edits := intValue(parsed["applied_edits"])
		if path != "" && edits > 0 {
			return fmt.Sprintf("applied %d edit(s) to %s", edits, path)
		}
	case "fs.list":
		path := strings.TrimSpace(fmt.Sprintf("%v", parsed["path"]))
		entries := 0
		if list, ok := parsed["entries"].([]any); ok {
			entries = len(list)
		}
		if path != "" {
			return fmt.Sprintf("listed %d entries in %s", entries, path)
		}
	case "fs.read":
		path := strings.TrimSpace(fmt.Sprintf("%v", parsed["path"]))
		content := strings.TrimSpace(fmt.Sprintf("%v", parsed["content"]))
		if path != "" {
			if content == "" {
				return fmt.Sprintf("read %s (empty)", path)
			}
			return fmt.Sprintf("read %s", path)
		}
	case "shell.exec":
		exitCode := intValue(parsed["exit_code"])
		fallback := strings.TrimSpace(fmt.Sprintf("%v", parsed["shell_fallback"]))
		if fallback != "" && fallback != "<nil>" {
			return fmt.Sprintf("shell command completed via %s fallback (exit %d)", fallback, exitCode)
		}
		return fmt.Sprintf("shell command completed (exit %d)", exitCode)
	}

	return truncateSummary(output, 180)
}

func truncateSummary(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return strings.TrimSpace(value[:max-3]) + "..."
}

func intValue(v any) int {
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" || s == "<nil>" {
		return 0
	}
	switch value := v.(type) {
	case int:
		return value
	case int8:
		return int(value)
	case int16:
		return int(value)
	case int32:
		return int(value)
	case int64:
		return int(value)
	case uint:
		return int(value)
	case uint8:
		return int(value)
	case uint16:
		return int(value)
	case uint32:
		return int(value)
	case uint64:
		return int(value)
	case float32:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if i, err := value.Int64(); err == nil {
			return int(i)
		}
		if f, err := value.Float64(); err == nil {
			return int(f)
		}
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int(f)
	}
	return 0
}

func (c *runTraceCollector) Snapshot() map[string]any {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	copyEnv := c.env
	c.mu.Unlock()

	b, err := json.Marshal(copyEnv)
	if err != nil {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

type traceContextKey struct{}

func withRunTraceCollector(ctx context.Context, collector *runTraceCollector) context.Context {
	if collector == nil {
		return ctx
	}
	return context.WithValue(ctx, traceContextKey{}, collector)
}

func runTraceCollectorFromContext(ctx context.Context) *runTraceCollector {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(traceContextKey{})
	collector, _ := v.(*runTraceCollector)
	return collector
}
