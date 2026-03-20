package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/messagecontent"
)

// Claude Code canonical tool names (case-insensitive lookup → CC casing).
// When using Anthropic OAuth we map outgoing tool names to these and
// reverse-map incoming tool_use names back to our canonical names.
var claudeCodeTools = []string{
	"Read", "Write", "Edit", "Bash", "Grep", "Glob",
	"AskUserQuestion", "EnterPlanMode", "ExitPlanMode",
	"KillShell", "NotebookEdit", "Skill", "Task",
	"TaskOutput", "TodoWrite", "WebFetch", "WebSearch",
}

var ccToolLookup = func() map[string]string {
	m := make(map[string]string, len(claudeCodeTools))
	for _, t := range claudeCodeTools {
		m[strings.ToLower(t)] = t
	}
	return m
}()

// toClaudeCodeName maps a tool name to the CC canonical casing if it
// matches (case-insensitive); otherwise returns the name unchanged.
func toClaudeCodeName(name string) string {
	if cc, ok := ccToolLookup[strings.ToLower(name)]; ok {
		return cc
	}
	return name
}

// anthropicModelSupportsAdaptiveThinking returns true for Opus 4.6 and
// Sonnet 4.6 models which use adaptive thinking instead of budget-based.
func anthropicModelSupportsAdaptiveThinking(modelName string) bool {
	lower := strings.ToLower(modelName)
	return strings.Contains(lower, "opus-4-6") ||
		strings.Contains(lower, "opus-4.6") ||
		strings.Contains(lower, "sonnet-4-6") ||
		strings.Contains(lower, "sonnet-4.6")
}

// generateAnthropicMessages translates internal ModelRequest to the Anthropic
// Messages API format and executes the request.
func (m *ProviderModel) generateAnthropicMessages(ctx context.Context, req agent.ModelRequest) (agent.ModelResponse, error) {
	messages := requestMessages(req)
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = strings.TrimSpace(lastUserMessage(messages))
	}

	systemPrompt := strings.TrimSpace(req.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = strings.TrimSpace(req.Prompt)
	}
	system := appendToolResultsPrompt(systemPrompt, req.ToolResults)
	isOAuth := strings.HasPrefix(strings.TrimSpace(m.apiKey), "sk-ant-oat")

	// Build Anthropic-format messages from history.
	// Anthropic requires strict user/assistant alternation.
	anthMessages := buildAnthropicMessages(messages)

	body := map[string]any{
		"model":      m.modelName,
		"max_tokens": m.responseMaxTokens,
		"messages":   anthMessages,
	}
	if systemPayload, ok := buildAnthropicSystemPayload(system, isOAuth); ok {
		body["system"] = systemPayload
	}
	if len(req.ToolSchemas) > 0 {
		body["tools"] = buildAnthropicTools(req.ToolSchemas, isOAuth)
	}

	// Adaptive thinking for Opus 4.6 / Sonnet 4.6 (OAuth or API-key).
	if anthropicModelSupportsAdaptiveThinking(m.modelName) {
		body["thinking"] = map[string]string{"type": "adaptive"}
		// Temperature is incompatible with thinking mode.
		delete(body, "temperature")
	}

	if req.OnTextDelta != nil {
		body["stream"] = true
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return agent.ModelResponse{}, err
	}

	if trace := runTraceCollectorFromContext(ctx); trace != nil {
		trace.RecordModelInput(msg, len(system), len(anthMessages) > 0, string(raw))
	}

	startedAt := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return agent.ModelResponse{}, err
	}
	// Anthropic uses x-api-key, not Bearer auth; headers already set via resolveProviderAccess
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range m.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return agent.ModelResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if trace := runTraceCollectorFromContext(ctx); trace != nil {
			trace.RecordModelOutput(resp.StatusCode, string(errBody), false, time.Since(startedAt).Milliseconds())
		}
		return agent.ModelResponse{}, fmt.Errorf("anthropic request failed: status=%d error=%s", resp.StatusCode, parseProviderErrorBody(errBody))
	}

	if req.OnTextDelta != nil {
		return m.consumeAnthropicSSE(ctx, resp.Body, req.OnTextDelta, req.AllowedTools, startedAt)
	}

	// Non-streaming
	var result anthropicResponseBody
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return agent.ModelResponse{}, fmt.Errorf("anthropic response parse error: %w", err)
	}
	if trace := runTraceCollectorFromContext(ctx); trace != nil {
		if rawOut, err := json.Marshal(result); err == nil {
			trace.RecordModelOutput(resp.StatusCode, string(rawOut), false, time.Since(startedAt).Milliseconds())
		}
		if result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 {
			trace.RecordModelUsage(result.Usage.InputTokens, result.Usage.CacheReadInputTokens, result.Usage.OutputTokens, result.Usage.InputTokens+result.Usage.OutputTokens)
		}
	}

	return parseAnthropicResponse(result, req.AllowedTools)
}

type anthropicResponseBody struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Content    []anthropicContentBlock `json:"content"`
	Model      string                  `json:"model"`
	StopReason string                  `json:"stop_reason"`
	Usage      anthropicUsage          `json:"usage"`
	Error      *anthropicError         `json:"error,omitempty"`
}

type anthropicContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Input    any    `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens          int `json:"input_tokens"`
	OutputTokens         int `json:"output_tokens"`
	CacheReadInputTokens int `json:"cache_read_input_tokens"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func buildAnthropicSystemPayload(system string, isOAuth bool) (any, bool) {
	system = strings.TrimSpace(system)
	if isOAuth {
		blocks := []map[string]string{{
			"type": "text",
			"text": "You are Claude Code, Anthropic's official CLI for Claude.",
		}}
		if system != "" {
			blocks = append(blocks, map[string]string{
				"type": "text",
				"text": system,
			})
		}
		return blocks, true
	}
	if system == "" {
		return nil, false
	}
	return system, true
}

func buildAnthropicMessages(messages []agent.ChatMessage) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role == "system" {
			continue // system prompt goes to top-level "system" field
		}
		// Map tool results to user messages
		if role == "tool" {
			role = "user"
		}
		if role != "user" && role != "assistant" {
			role = "user"
		}
		content := strings.TrimSpace(msg.Content)
		parts := messagecontent.Normalize(msg.ContentParts)
		if content == "" && len(parts) == 0 {
			continue
		}

		entry := map[string]any{"role": role, "content": content}
		result = append(result, entry)
	}

	// Anthropic requires alternating user/assistant messages.
	// Merge adjacent messages with the same role.
	return mergeAdjacentAnthropicMessages(result)
}

func mergeAdjacentAnthropicMessages(messages []map[string]any) []map[string]any {
	if len(messages) == 0 {
		return messages
	}
	merged := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		if len(merged) > 0 {
			prevRole, _ := merged[len(merged)-1]["role"].(string)
			currRole, _ := msg["role"].(string)
			if prevRole == currRole {
				// Merge by concatenating content
				prevContent, _ := merged[len(merged)-1]["content"].(string)
				currContent, _ := msg["content"].(string)
				merged[len(merged)-1]["content"] = strings.TrimSpace(prevContent + "\n\n" + currContent)
				continue
			}
		}
		merged = append(merged, msg)
	}

	// Anthropic requires the first message to be from the user
	if len(merged) > 0 {
		if role, _ := merged[0]["role"].(string); role != "user" {
			merged = append([]map[string]any{{"role": "user", "content": "(conversation context follows)"}}, merged...)
		}
	}

	return merged
}

func buildAnthropicTools(schemas []agent.ToolSchema, isOAuth bool) []map[string]any {
	tools := make([]map[string]any, 0, len(schemas))
	for _, s := range schemas {
		name := normalizeProviderToolSchemaName("anthropic", s.Name)
		if isOAuth {
			name = toClaudeCodeName(name)
		}
		tool := map[string]any{
			"name":        name,
			"description": s.Description,
		}
		params := s.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tool["input_schema"] = normalizeProviderToolParameters(params)
		tools = append(tools, tool)
	}
	return tools
}

func parseAnthropicResponse(result anthropicResponseBody, allowedTools []string) (agent.ModelResponse, error) {
	if result.Error != nil {
		return agent.ModelResponse{}, fmt.Errorf("anthropic error: %s: %s", result.Error.Type, result.Error.Message)
	}

	var toolCalls []providerToolCall
	var textContent strings.Builder
	var thinkingContent strings.Builder

	for _, block := range result.Content {
		switch block.Type {
		case "text":
			textContent.WriteString(block.Text)
		case "thinking":
			if block.Thinking != "" {
				thinkingContent.WriteString(block.Thinking)
			}
		case "tool_use":
			argsJSON, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, providerToolCall{
				ID:   block.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: block.Name, Arguments: string(argsJSON)},
			})
		}
	}

	if len(toolCalls) > 0 {
		parsed, parseFailure, reason := parseNativeProviderToolCalls(toolCalls, allowedTools)
		if len(parsed) > 0 {
			return agent.ModelResponse{
				ToolCalls:       parsed,
				Thinking:        strings.TrimSpace(thinkingContent.String()),
				ThinkingPresent: thinkingContent.Len() > 0,
			}, nil
		}
		if parseFailure {
			return agent.ModelResponse{
				FinalText:        formatNativeToolCallParseFailureUserMessage(reason),
				ToolParseFailure: true,
			}, nil
		}
	}

	text := strings.TrimSpace(textContent.String())
	nativeThinking := strings.TrimSpace(thinkingContent.String())

	// If native thinking blocks were returned, use those directly.
	if nativeThinking != "" {
		if text == "" {
			return agent.ModelResponse{}, errors.New("anthropic returned no content")
		}
		return agent.ModelResponse{
			FinalText:       text,
			Thinking:        nativeThinking,
			ThinkingPresent: true,
		}, nil
	}

	if text == "" {
		return agent.ModelResponse{}, errors.New("anthropic returned no content")
	}

	visibleText, extracted, extractedPresent := ExtractThinking(text)
	return agent.ModelResponse{
		FinalText:       strings.TrimSpace(visibleText),
		Thinking:        extracted,
		ThinkingPresent: extractedPresent,
	}, nil
}

// consumeAnthropicSSE parses Anthropic's SSE stream format.
// Event types: message_start, content_block_start, content_block_delta,
// content_block_stop, message_delta, message_stop
func (m *ProviderModel) consumeAnthropicSSE(ctx context.Context, body io.Reader, onDelta func(string) error, allowedTools []string, startedAt time.Time) (agent.ModelResponse, error) {
	br := bufio.NewReader(body)
	var content strings.Builder
	var rawLog bytes.Buffer
	var toolCalls []providerToolCall

	var thinking strings.Builder

	// Track current tool_use block being built
	type toolUseState struct {
		id   string
		name string
		args strings.Builder
	}
	var currentTool *toolUseState

	for {
		line, err := br.ReadString('\n')
		rawLog.WriteString(line)
		if err != nil {
			if err == io.EOF {
				break
			}
			return agent.ModelResponse{}, err
		}

		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		// We identify event type by data payload type field, not the SSE "event:" header
		if strings.HasPrefix(line, "event: ") {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event struct {
			Type         string `json:"type"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
				Text string `json:"text"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Index   int                    `json:"index"`
			Message *anthropicResponseBody `json:"message"`
			Usage   *anthropicUsage        `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				currentTool = &toolUseState{
					id:   event.ContentBlock.ID,
					name: event.ContentBlock.Name,
				}
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				if event.Delta.Text != "" {
					content.WriteString(event.Delta.Text)
					if onDelta != nil {
						if err := onDelta(event.Delta.Text); err != nil {
							return agent.ModelResponse{}, err
						}
					}
				}
			case "thinking_delta":
				if event.Delta.Thinking != "" {
					thinking.WriteString(event.Delta.Thinking)
				}
			case "input_json_delta":
				if currentTool != nil {
					currentTool.args.WriteString(event.Delta.PartialJSON)
				}
			}
		case "content_block_stop":
			if currentTool != nil {
				toolCalls = append(toolCalls, providerToolCall{
					ID:   currentTool.id,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: currentTool.name, Arguments: currentTool.args.String()},
				})
				currentTool = nil
			}
		case "message_stop":
			if trace := runTraceCollectorFromContext(ctx); trace != nil {
				trace.RecordModelOutput(200, rawLog.String(), true, time.Since(startedAt).Milliseconds())
			}
		case "message_start":
			if event.Message != nil && event.Message.Usage.InputTokens > 0 {
				if trace := runTraceCollectorFromContext(ctx); trace != nil {
					trace.RecordModelUsage(event.Message.Usage.InputTokens, event.Message.Usage.CacheReadInputTokens, 0, 0)
				}
			}
		case "message_delta":
			if event.Usage != nil {
				if trace := runTraceCollectorFromContext(ctx); trace != nil {
					trace.RecordModelUsage(0, 0, event.Usage.OutputTokens, event.Usage.OutputTokens)
				}
			}
		}
	}

	if len(toolCalls) > 0 {
		parsed, parseFailure, reason := parseNativeProviderToolCalls(toolCalls, allowedTools)
		if len(parsed) > 0 {
			return agent.ModelResponse{ToolCalls: parsed}, nil
		}
		if parseFailure {
			return agent.ModelResponse{
				FinalText:        formatNativeToolCallParseFailureUserMessage(reason),
				ToolParseFailure: true,
			}, nil
		}
	}

	text := strings.TrimSpace(content.String())
	thinkingText := strings.TrimSpace(thinking.String())

	// If native thinking blocks were streamed, use those directly.
	// Otherwise fall back to tag-based extraction from text content.
	if thinkingText != "" {
		if text == "" && len(toolCalls) == 0 {
			return agent.ModelResponse{}, errors.New("anthropic stream returned no content")
		}
		return agent.ModelResponse{
			FinalText:       text,
			Thinking:        thinkingText,
			ThinkingPresent: true,
		}, nil
	}

	if text == "" {
		return agent.ModelResponse{}, errors.New("anthropic stream returned no content")
	}

	visibleText, extracted, extractedPresent := ExtractThinking(text)
	return agent.ModelResponse{
		FinalText:       strings.TrimSpace(visibleText),
		Thinking:        extracted,
		ThinkingPresent: extractedPresent,
	}, nil
}
