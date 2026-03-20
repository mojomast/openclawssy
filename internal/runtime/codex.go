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
)

// generateCodexResponses translates internal ModelRequest into the Codex
// Responses API format and executes the request.
func (m *ProviderModel) generateCodexResponses(ctx context.Context, req agent.ModelRequest) (agent.ModelResponse, error) {
	messages := requestMessages(req)
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = strings.TrimSpace(lastUserMessage(messages))
	}

	systemPrompt := strings.TrimSpace(req.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = strings.TrimSpace(req.Prompt)
	}
	instructions := appendToolResultsPrompt(systemPrompt, req.ToolResults)

	// Build Responses API input array from history
	input := buildCodexInput(messages)

	body := map[string]any{
		"model":        m.modelName,
		"instructions": instructions,
		"input":        input,
		"stream":       true, // Codex Responses API requires streaming
		"store":        false,
	}
	// Note: Codex Responses API does not support max_output_tokens for ChatGPT OAuth.
	if len(req.ToolSchemas) > 0 {
		body["tools"] = buildCodexTools(req.ToolSchemas)
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return agent.ModelResponse{}, err
	}
	if trace := runTraceCollectorFromContext(ctx); trace != nil {
		trace.RecordModelInput(msg, len(instructions), len(input) > 0, string(raw))
	}

	startedAt := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/codex/responses", bytes.NewReader(raw))
	if err != nil {
		return agent.ModelResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
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
		return agent.ModelResponse{}, fmt.Errorf("codex request failed: status=%d error=%s", resp.StatusCode, parseProviderErrorBody(errBody))
	}

	if req.OnTextDelta != nil {
		return m.consumeCodexSSE(ctx, resp.Body, req.OnTextDelta, startedAt)
	}

	// Non-streaming: parse full response
	var result codexResponseBody
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return agent.ModelResponse{}, fmt.Errorf("codex response parse error: %w", err)
	}
	if trace := runTraceCollectorFromContext(ctx); trace != nil {
		if rawOut, err := json.Marshal(result); err == nil {
			trace.RecordModelOutput(resp.StatusCode, string(rawOut), false, time.Since(startedAt).Milliseconds())
		}
	}

	return parseCodexResponse(result, req.AllowedTools)
}

type codexResponseBody struct {
	ID         string                       `json:"id"`
	Status     string                       `json:"status"`
	OutputText string                       `json:"output_text"`
	Output     []providerResponseOutputItem `json:"output"`
	Usage      providerResponseUsage        `json:"usage"`
	Error      any                          `json:"error"`
}

func buildCodexInput(messages []agent.ChatMessage) []map[string]any {
	input := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role == "system" {
			continue // system goes to instructions
		}
		if role == "tool" {
			role = "user" // codex doesn't support tool role in input
		}
		if role != "user" && role != "assistant" {
			role = "user"
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		input = append(input, map[string]any{"role": role, "content": content})
	}
	return input
}

func buildCodexTools(schemas []agent.ToolSchema) []map[string]any {
	tools := make([]map[string]any, 0, len(schemas))
	for _, s := range schemas {
		tool := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        normalizeProviderToolSchemaName("openai_codex", s.Name),
				"description": s.Description,
			},
		}
		if s.Parameters != nil {
			tool["function"].(map[string]any)["parameters"] = normalizeProviderToolParameters(s.Parameters)
		}
		tools = append(tools, tool)
	}
	return tools
}

func parseCodexResponse(result codexResponseBody, allowedTools []string) (agent.ModelResponse, error) {
	if result.Status == "failed" {
		return agent.ModelResponse{}, fmt.Errorf("codex response failed: %v", result.Error)
	}

	// Check for tool calls in output
	var toolCalls []providerToolCall
	var textContent strings.Builder
	for _, item := range result.Output {
		switch item.Type {
		case "function_call":
			toolCalls = append(toolCalls, providerToolCall{
				ID:   item.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: item.Name, Arguments: item.Arguments},
			})
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" || part.Type == "text" {
					textContent.WriteString(part.Text)
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

	text := strings.TrimSpace(textContent.String())
	if text == "" {
		text = strings.TrimSpace(result.OutputText)
	}
	if text == "" {
		return agent.ModelResponse{}, errors.New("codex returned no content")
	}

	visibleText, thinkingText, thinkingPresent := ExtractThinking(text)
	return agent.ModelResponse{
		FinalText:       strings.TrimSpace(visibleText),
		Thinking:        thinkingText,
		ThinkingPresent: thinkingPresent,
	}, nil
}

func (m *ProviderModel) consumeCodexSSE(ctx context.Context, body io.Reader, onDelta func(string) error, startedAt time.Time) (agent.ModelResponse, error) {
	br := bufio.NewReader(body)
	var content strings.Builder
	var toolCalls []providerToolCall
	var rawLog bytes.Buffer

	for {
		data, done, err := readNextSSEData(br)
		if err != nil {
			break
		}
		if done {
			break
		}
		rawLog.WriteString(data)
		rawLog.WriteByte('\n')

		trimmed := strings.TrimSpace(data)
		if trimmed == "" || trimmed == "[DONE]" {
			break
		}

		var env providerStreamingEnvelope
		if err := json.Unmarshal([]byte(trimmed), &env); err != nil {
			continue
		}

		switch env.Type {
		case "response.output_text.delta":
			delta := env.Delta
			if delta != "" {
				content.WriteString(delta)
				if onDelta != nil {
					if err := onDelta(delta); err != nil {
						return agent.ModelResponse{}, err
					}
				}
			}
		case "response.function_call_arguments.done":
			if env.Item.Type == "function_call" {
				toolCalls = append(toolCalls, providerToolCall{
					ID:   env.Item.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: env.Item.Name, Arguments: env.Item.Arguments},
				})
			}
		case "response.completed":
			if trace := runTraceCollectorFromContext(ctx); trace != nil {
				trace.RecordModelOutput(200, rawLog.String(), true, time.Since(startedAt).Milliseconds())
			}
		case "response.failed":
			return agent.ModelResponse{}, fmt.Errorf("codex stream failed: %v", env.Message)
		case "error":
			return agent.ModelResponse{}, fmt.Errorf("codex stream error: %v", env.Message)
		}
	}

	if len(toolCalls) > 0 {
		parsed, parseFailure, reason := parseNativeProviderToolCalls(toolCalls, nil)
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
	if text == "" {
		return agent.ModelResponse{}, errors.New("codex stream returned no content")
	}

	visibleText, thinkingText, thinkingPresent := ExtractThinking(text)
	return agent.ModelResponse{
		FinalText:       strings.TrimSpace(visibleText),
		Thinking:        thinkingText,
		ThinkingPresent: thinkingPresent,
	}, nil
}
