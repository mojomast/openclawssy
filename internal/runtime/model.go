package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/config"
	"openclawssy/internal/toolparse"
)

type ProviderModel struct {
	providerName      string
	modelName         string
	baseURL           string
	apiKey            string
	headers           map[string]string
	httpClient        *http.Client
	responseMaxTokens int
	contextWindow     int
}

const (
	defaultProviderTimeout = 90 * time.Second
	providerMaxAttempts    = 3
	providerRetryBackoff   = 700 * time.Millisecond
	toolNamePattern        = `[A-Za-z][A-Za-z0-9_]*\.[A-Za-z][A-Za-z0-9_]*`
	maxToolCallsPerReply   = 12
	maxPromptToolResults   = 12
	maxPromptToolOutput    = 6000
	maxPromptToolError     = 1200
	maxResponseTokens      = 20000
	defaultContextWindow   = 120000
	contextCompactionRatio = 0.80
	compactionKeepRecent   = 60
)

var toolNameAliases = map[string]string{
	"fs.read":              "fs.read",
	"fs.list":              "fs.list",
	"fs.write":             "fs.write",
	"fs.append":            "fs.append",
	"fs.delete":            "fs.delete",
	"fs.move":              "fs.move",
	"fs.rename":            "fs.move",
	"fs.edit":              "fs.edit",
	"code.search":          "code.search",
	"config.get":           "config.get",
	"config.set":           "config.set",
	"secrets.get":          "secrets.get",
	"secrets.set":          "secrets.set",
	"secrets.list":         "secrets.list",
	"skill.list":           "skill.list",
	"skill.read":           "skill.read",
	"skill.get":            "skill.read",
	"scheduler.list":       "scheduler.list",
	"scheduler.add":        "scheduler.add",
	"scheduler.remove":     "scheduler.remove",
	"scheduler.pause":      "scheduler.pause",
	"scheduler.resume":     "scheduler.resume",
	"session.list":         "session.list",
	"session.close":        "session.close",
	"agent.list":           "agent.list",
	"agent.create":         "agent.create",
	"agent.switch":         "agent.switch",
	"agent.profile.get":    "agent.profile.get",
	"agent.profile.set":    "agent.profile.set",
	"agent.message.send":   "agent.message.send",
	"agent.message.inbox":  "agent.message.inbox",
	"agent.run":            "agent.run",
	"agent.prompt.read":    "agent.prompt.read",
	"agent.prompt.update":  "agent.prompt.update",
	"agent.prompt.suggest": "agent.prompt.suggest",
	"policy.list":          "policy.list",
	"policy.grant":         "policy.grant",
	"policy.revoke":        "policy.revoke",
	"run.list":             "run.list",
	"run.get":              "run.get",
	"run.cancel":           "run.cancel",
	"metrics.get":          "metrics.get",
	"memory.search":        "memory.search",
	"memory.write":         "memory.write",
	"memory.update":        "memory.update",
	"memory.forget":        "memory.forget",
	"memory.health":        "memory.health",
	"memory.checkpoint":    "memory.checkpoint",
	"memory.maintenance":   "memory.maintenance",
	"decision.log":         "decision.log",
	"http.request":         "http.request",
	"net.fetch":            "http.request",
	"time.now":             "time.now",
	"shell.exec":           "shell.exec",
	"bash.exec":            "shell.exec",
	"terminal.exec":        "shell.exec",
	"terminal.run":         "shell.exec",
}

var toolNotAllowedReasonRE = regexp.MustCompile(`tool "([^"]+)" not allowed`)

type SecretLookup func(name string) (string, bool, error)

func NewProviderModel(cfg config.Config, lookup SecretLookup) (*ProviderModel, error) {
	return NewProviderModelForConfig(cfg, cfg.Model, lookup)
}

func NewProviderModelForConfig(cfg config.Config, modelCfg config.ModelConfig, lookup SecretLookup) (*ProviderModel, error) {
	pName := strings.ToLower(strings.TrimSpace(modelCfg.Provider))
	endpoint, err := providerEndpoint(cfg, pName)
	if err != nil {
		return nil, err
	}
	apiKey := strings.TrimSpace(endpoint.APIKey)
	if apiKey == "" && lookup != nil {
		if v, ok, err := lookup("provider/" + pName + "/api_key"); err == nil && ok {
			apiKey = strings.TrimSpace(v)
		}
	}
	if apiKey == "" && endpoint.APIKeyEnv != "" {
		apiKey = strings.TrimSpace(os.Getenv(endpoint.APIKeyEnv))
	}
	if apiKey == "" {
		return nil, fmt.Errorf("model provider %q is missing API key (set %s or providers.%s.api_key)", pName, endpoint.APIKeyEnv, pName)
	}

	base := strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("model provider %q requires a base_url", pName)
	}
	if strings.HasSuffix(base, "/chat/completions") {
		base = strings.TrimSuffix(base, "/chat/completions")
	}

	headers := map[string]string{}
	for k, v := range endpoint.Headers {
		headers[k] = v
	}

	responseMaxTokens := modelCfg.MaxTokens
	if responseMaxTokens <= 0 || responseMaxTokens > maxResponseTokens {
		responseMaxTokens = maxResponseTokens
	}

	return &ProviderModel{
		providerName:      pName,
		modelName:         modelCfg.Name,
		baseURL:           base,
		apiKey:            apiKey,
		headers:           headers,
		httpClient:        &http.Client{Timeout: defaultProviderTimeout},
		responseMaxTokens: responseMaxTokens,
		contextWindow:     defaultContextWindow,
	}, nil
}

func (m *ProviderModel) ProviderName() string { return m.providerName }
func (m *ProviderModel) ModelName() string    { return m.modelName }

func (m *ProviderModel) Generate(ctx context.Context, req agent.ModelRequest) (agent.ModelResponse, error) {
	messages := requestMessages(req)
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = strings.TrimSpace(lastUserMessage(messages))
	}
	if strings.HasPrefix(msg, "/tool ") {
		if len(req.ToolResults) > 0 {
			return agent.ModelResponse{FinalText: toolResultsText(req.ToolResults)}, nil
		}
		return parseToolDirective(msg, req.AllowedTools)
	}

	systemPrompt := strings.TrimSpace(req.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = strings.TrimSpace(req.Prompt)
	}
	promptText := appendToolResultsPrompt(systemPrompt, req.ToolResults)

	normalizedMessages := make([]agent.ChatMessage, 0, len(messages))
	for _, item := range messages {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if role == "" {
			role = "user"
		}
		if role != "system" && role != "user" && role != "assistant" && role != "tool" {
			continue
		}
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		normalizedMessages = append(normalizedMessages, agent.ChatMessage{Role: role, Content: content})
	}

	normalizedMessages = compactMessagesForContext(promptText, normalizedMessages, m.contextWindow)

	chatMessages := make([]map[string]string, 0, len(normalizedMessages)+1)
	chatMessages = append(chatMessages, map[string]string{"role": "system", "content": promptText})
	for _, item := range normalizedMessages {
		chatMessages = append(chatMessages, map[string]string{"role": item.Role, "content": item.Content})
	}

	body := map[string]any{
		"model":      m.modelName,
		"messages":   chatMessages,
		"max_tokens": m.responseMaxTokens,
	}
	if req.OnTextDelta != nil {
		body["stream"] = true
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return agent.ModelResponse{}, err
	}
	if trace := runTraceCollectorFromContext(ctx); trace != nil {
		trace.RecordModelInput(msg, len(promptText), len(normalizedMessages) > 1, string(raw))
	}

	content := ""
	if req.OnTextDelta == nil {
		var payload struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Error any `json:"error"`
		}
		statusCode, err := m.doChatCompletionWithRetry(ctx, raw, &payload)
		if err != nil {
			return agent.ModelResponse{}, err
		}
		if statusCode >= 300 {
			return agent.ModelResponse{}, fmt.Errorf("provider %s request failed: status=%d error=%v", m.providerName, statusCode, payload.Error)
		}
		if len(payload.Choices) == 0 {
			return agent.ModelResponse{}, errors.New("provider returned no choices")
		}
		content = strings.TrimSpace(payload.Choices[0].Message.Content)
	} else {
		streamResult, err := m.doStreamingChatCompletionWithRetry(ctx, raw, req.OnTextDelta)
		if err != nil {
			return agent.ModelResponse{}, err
		}
		if streamResult.StatusCode >= 300 {
			return agent.ModelResponse{}, fmt.Errorf("provider %s request failed: status=%d error=%v", m.providerName, streamResult.StatusCode, streamResult.Error)
		}
		content = strings.TrimSpace(streamResult.Content)
		if content == "" {
			return agent.ModelResponse{}, errors.New("provider returned no choices")
		}
	}

	trace := runTraceCollectorFromContext(ctx)
	visibleText, thinkingText, thinkingPresent := ExtractThinking(content)

	// Check if the model's response contains tool calls.
	toolCalls, parseFailure, parseFailureReason := parseToolCallsFromResponse(content, req.AllowedTools, trace)
	if len(toolCalls) == 0 && thinkingPresent {
		var visibleParseFailure bool
		var visibleParseFailureReason string
		toolCalls, visibleParseFailure, visibleParseFailureReason = parseToolCallsFromResponse(visibleText, req.AllowedTools, trace)
		parseFailure = parseFailure || visibleParseFailure
		parseFailureReason = mergeParseFailureReasons(parseFailureReason, visibleParseFailureReason)
	}
	if len(toolCalls) > 0 {
		return agent.ModelResponse{
			ToolCalls:       toolCalls,
			Thinking:        thinkingText,
			ThinkingPresent: thinkingPresent,
		}, nil
	}
	if parseFailure {
		if friendly, ok := formatToolParseFailureUserMessage(visibleText, parseFailureReason); ok {
			visibleText = friendly
		}
	}

	return agent.ModelResponse{
		FinalText:        visibleText,
		Thinking:         thinkingText,
		ThinkingPresent:  thinkingPresent,
		ToolParseFailure: parseFailure,
	}, nil
}

func (m *ProviderModel) doChatCompletionWithRetry(ctx context.Context, raw []byte, payload any) (int, error) {
	if m.httpClient == nil {
		m.httpClient = &http.Client{Timeout: defaultProviderTimeout}
	}

	var lastErr error
	for attempt := 1; attempt <= providerMaxAttempts; attempt++ {
		attemptCtx, cancel := ensureProviderRequestTimeout(ctx, defaultProviderTimeout)
		statusCode, err := m.doChatCompletionOnce(attemptCtx, raw, payload)
		cancel()
		if err == nil {
			return statusCode, nil
		}
		lastErr = err
		if !shouldRetryProviderError(err) || attempt == providerMaxAttempts {
			return 0, err
		}

		backoff := providerRetryBackoff
		if attempt > 1 {
			backoff = providerRetryBackoff * time.Duration(1<<(attempt-1))
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(backoff):
		}
	}

	if lastErr != nil {
		return 0, lastErr
	}
	return 0, errors.New("provider request failed")
}

type streamingChatCompletionResult struct {
	StatusCode   int
	Content      string
	Error        any
	DeltaEmitted bool
}

func (m *ProviderModel) doStreamingChatCompletionWithRetry(ctx context.Context, raw []byte, onDelta func(string) error) (streamingChatCompletionResult, error) {
	if m.httpClient == nil {
		m.httpClient = &http.Client{Timeout: defaultProviderTimeout}
	}

	var lastResult streamingChatCompletionResult
	var lastErr error
	for attempt := 1; attempt <= providerMaxAttempts; attempt++ {
		attemptCtx, cancel := ensureProviderRequestTimeout(ctx, defaultProviderTimeout)
		result, err := m.doStreamingChatCompletionOnce(attemptCtx, raw, onDelta)
		cancel()
		if err == nil {
			return result, nil
		}
		lastResult = result
		lastErr = err
		if result.DeltaEmitted {
			return lastResult, fmt.Errorf("provider stream interrupted after partial output: %w", err)
		}
		if !shouldRetryProviderError(err) || attempt == providerMaxAttempts {
			return lastResult, err
		}

		backoff := providerRetryBackoff
		if attempt > 1 {
			backoff = providerRetryBackoff * time.Duration(1<<(attempt-1))
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
		select {
		case <-ctx.Done():
			return streamingChatCompletionResult{}, ctx.Err()
		case <-time.After(backoff):
		}
	}
	if lastErr != nil {
		return lastResult, lastErr
	}
	return lastResult, errors.New("provider streaming request failed")
}

func (m *ProviderModel) doStreamingChatCompletionOnce(ctx context.Context, raw []byte, onDelta func(string) error) (streamingChatCompletionResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return streamingChatCompletionResult{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range m.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return streamingChatCompletionResult{}, err
	}
	defer resp.Body.Close()

	result := streamingChatCompletionResult{StatusCode: resp.StatusCode}
	if resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return result, readErr
		}
		result.Error = parseProviderErrorBody(body)
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return result, fmt.Errorf("retryable provider status: %d", resp.StatusCode)
		}
		return result, nil
	}

	content, emitted, err := consumeProviderSSE(resp.Body, onDelta)
	result.Content = content
	result.DeltaEmitted = emitted
	if err != nil {
		return result, err
	}
	return result, nil
}

func consumeProviderSSE(reader io.Reader, onDelta func(string) error) (string, bool, error) {
	br := bufio.NewReader(reader)
	var content strings.Builder
	emitted := false
	for {
		data, done, err := readNextSSEData(br)
		if err != nil {
			return content.String(), emitted, err
		}
		if done {
			break
		}
		trimmed := strings.TrimSpace(data)
		if trimmed == "" {
			continue
		}
		if trimmed == "[DONE]" {
			break
		}
		delta, err := extractStreamingDeltaText(trimmed)
		if err != nil {
			return content.String(), emitted, err
		}
		if delta == "" {
			continue
		}
		content.WriteString(delta)
		emitted = true
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				return content.String(), emitted, err
			}
		}
	}
	return content.String(), emitted, nil
}

func readNextSSEData(reader *bufio.Reader) (string, bool, error) {
	if reader == nil {
		return "", true, nil
	}
	dataLines := make([]string, 0, 1)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", false, err
		}
		if line != "" {
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if len(dataLines) > 0 {
					return strings.Join(dataLines, "\n"), false, nil
				}
			} else if strings.HasPrefix(line, ":") {
				// Heartbeat/comment.
			} else if strings.HasPrefix(line, "data:") {
				value := strings.TrimPrefix(line, "data:")
				if strings.HasPrefix(value, " ") {
					value = value[1:]
				}
				dataLines = append(dataLines, value)
			}
		}
		if errors.Is(err, io.EOF) {
			if len(dataLines) > 0 {
				return strings.Join(dataLines, "\n"), false, nil
			}
			return "", true, nil
		}
	}
}

func extractStreamingDeltaText(raw string) (string, error) {
	payload := strings.TrimSpace(raw)
	if payload == "" {
		return "", nil
	}
	var envelope struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return "", err
	}
	if len(envelope.Choices) == 0 {
		return "", nil
	}
	choice := envelope.Choices[0]
	if choice.Delta.Content != "" {
		return choice.Delta.Content, nil
	}
	if choice.Text != "" {
		return choice.Text, nil
	}
	if choice.Message.Content != "" {
		return choice.Message.Content, nil
	}
	return "", nil
}

func parseProviderErrorBody(body []byte) any {
	if len(body) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		if value, ok := payload["error"]; ok {
			return value
		}
		return payload
	}
	return strings.TrimSpace(string(body))
}

func (m *ProviderModel) doChatCompletionOnce(ctx context.Context, raw []byte, payload any) (int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range m.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(payload); err != nil {
		return 0, err
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return resp.StatusCode, fmt.Errorf("retryable provider status: %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func shouldRetryProviderError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "retryable provider status") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "unexpected eof")
}

func ensureProviderRequestTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func appendToolResultsPrompt(prompt string, results []agent.ToolCallResult) string {
	if len(results) == 0 {
		return prompt
	}

	start := 0
	if len(results) > maxPromptToolResults {
		start = len(results) - maxPromptToolResults
	}

	var b strings.Builder
	b.WriteString(prompt)
	if prompt != "" && !strings.HasSuffix(prompt, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n## Tool Results\n")
	b.WriteString("- total_results_so_far: ")
	b.WriteString(strconv.Itoa(len(results)))
	b.WriteString("\n")
	b.WriteString("- If the user requested a specific number of tool calls and that count has been reached, stop calling tools and provide the final answer.\n")
	if start > 0 {
		b.WriteString("- older_results_omitted: ")
		b.WriteString(strconv.Itoa(start))
		b.WriteString("\n")
	}
	if toolResultsContainErrors(results[start:]) {
		b.WriteString("\n## Tool Failure Recovery\n")
		b.WriteString("- One or more tool calls failed. Diagnose the cause from error/output, then continue with a revised plan.\n")
		b.WriteString("- Do not repeat the exact same failing call without changing inputs/approach.\n")
		b.WriteString("- Ask the user only if blocked by missing credentials, permissions, or an irreversible decision.\n")
	}
	for _, tr := range results[start:] {
		b.WriteString("- id: ")
		b.WriteString(tr.ID)
		b.WriteString("\n")
		if tr.Error != "" {
			b.WriteString("  error: ")
			b.WriteString(truncateForPrompt(tr.Error, maxPromptToolError))
			b.WriteString("\n")
		}
		if strings.TrimSpace(tr.Output) != "" {
			output := truncateForPrompt(tr.Output, maxPromptToolOutput)
			b.WriteString("  output:\n")
			b.WriteString("  ```\n")
			b.WriteString(output)
			if output != "" && !strings.HasSuffix(output, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("  ```\n")
		}
	}

	return b.String()
}

func toolResultsContainErrors(results []agent.ToolCallResult) bool {
	for _, tr := range results {
		if strings.TrimSpace(tr.Error) != "" {
			return true
		}
	}
	return false
}

func truncateForPrompt(value string, maxChars int) string {
	if maxChars <= 0 {
		return strings.TrimSpace(value)
	}
	text := strings.TrimSpace(value)
	if len(text) <= maxChars {
		return text
	}
	if maxChars <= 3 {
		return text[:maxChars]
	}
	return strings.TrimSpace(text[:maxChars-3]) + "..."
}

func compactMessagesForContext(systemPrompt string, messages []agent.ChatMessage, contextWindow int) []agent.ChatMessage {
	if len(messages) == 0 {
		return messages
	}
	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}
	budget := int(float64(contextWindow) * contextCompactionRatio)
	if budget <= 0 {
		return messages
	}
	if estimateConversationTokens(systemPrompt, messages) <= budget {
		return messages
	}

	keepRecent := compactionKeepRecent
	if keepRecent >= len(messages) {
		keepRecent = len(messages) / 2
	}
	if keepRecent < 8 {
		keepRecent = 8
	}
	if keepRecent > len(messages) {
		keepRecent = len(messages)
	}

	dropCount := len(messages) - keepRecent
	if dropCount < 1 {
		dropCount = 1
	}

	dropped := append([]agent.ChatMessage(nil), messages[:dropCount]...)
	kept := append([]agent.ChatMessage(nil), messages[dropCount:]...)

	compacted := make([]agent.ChatMessage, 0, len(kept)+1)
	if summary := buildCompactionSummary(dropped); summary != "" {
		compacted = append(compacted, agent.ChatMessage{Role: "system", Content: summary})
	}
	compacted = append(compacted, kept...)

	for estimateConversationTokens(systemPrompt, compacted) > budget && len(compacted) > 2 {
		if strings.EqualFold(strings.TrimSpace(compacted[0].Role), "system") {
			compacted = append(compacted[:1], compacted[2:]...)
			continue
		}
		compacted = compacted[1:]
	}

	if estimateConversationTokens(systemPrompt, compacted) > budget {
		compacted = truncateCompactedMessages(systemPrompt, compacted, budget)
	}

	return compacted
}

func buildCompactionSummary(messages []agent.ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	maxLines := 24
	if maxLines > len(messages) {
		maxLines = len(messages)
	}

	var b strings.Builder
	b.WriteString("Conversation compaction summary (older turns):\n")
	for i := 0; i < maxLines; i++ {
		role := strings.ToLower(strings.TrimSpace(messages[i].Role))
		if role == "" {
			role = "user"
		}
		content := compactSummaryText(messages[i].Content, 180)
		if content == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteString("\n")
	}
	if len(messages) > maxLines {
		b.WriteString("- ... ")
		b.WriteString(strconv.Itoa(len(messages) - maxLines))
		b.WriteString(" older turn(s) omitted")
	}

	return strings.TrimSpace(b.String())
}

func compactSummaryText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	return truncateRunes(value, maxRunes)
}

func truncateCompactedMessages(systemPrompt string, messages []agent.ChatMessage, budget int) []agent.ChatMessage {
	if len(messages) == 0 {
		return messages
	}
	maxPerMessageRunes := 640

	copyMessages := make([]agent.ChatMessage, 0, len(messages))
	copyMessages = append(copyMessages, messages...)

	for i := 0; i < len(copyMessages)-1 && estimateConversationTokens(systemPrompt, copyMessages) > budget; i++ {
		copyMessages[i].Content = truncateRunes(copyMessages[i].Content, maxPerMessageRunes)
	}
	for estimateConversationTokens(systemPrompt, copyMessages) > budget && len(copyMessages) > 2 {
		if strings.EqualFold(strings.TrimSpace(copyMessages[0].Role), "system") {
			copyMessages = append(copyMessages[:1], copyMessages[2:]...)
			continue
		}
		copyMessages = copyMessages[1:]
	}
	return copyMessages
}

func estimateConversationTokens(systemPrompt string, messages []agent.ChatMessage) int {
	total := estimateTokens(systemPrompt) + 8
	for _, msg := range messages {
		total += estimateTokens(msg.Role)
		total += estimateTokens(msg.Content)
		total += 4
	}
	return total
}

func estimateTokens(value string) int {
	runes := len([]rune(strings.TrimSpace(value)))
	if runes <= 0 {
		return 0
	}
	tokens := runes / 4
	if runes%4 != 0 {
		tokens++
	}
	if tokens < 1 {
		return 1
	}
	return tokens
}

func truncateRunes(value string, maxRunes int) string {
	trimmed := strings.TrimSpace(value)
	if maxRunes <= 0 || len([]rune(trimmed)) <= maxRunes {
		return trimmed
	}
	r := []rune(trimmed)
	if maxRunes <= 3 {
		return string(r[:maxRunes])
	}
	return strings.TrimSpace(string(r[:maxRunes-3])) + "..."
}

func toolResultsText(results []agent.ToolCallResult) string {
	var b strings.Builder
	for i, tr := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("tool ")
		b.WriteString(tr.ID)
		if tr.Error != "" {
			b.WriteString(" error: ")
			b.WriteString(tr.Error)
			continue
		}
		b.WriteString(" output:\n")
		b.WriteString(tr.Output)
	}
	return strings.TrimSpace(b.String())
}

func ExtractThinking(text string) (visibleText, thinkingText string, thinkingPresent bool) {
	type markerPair struct {
		open  string
		close string
	}

	markerPairs := []markerPair{
		{open: "<think>", close: "</think>"},
		{open: "<analysis>", close: "</analysis>"},
		{open: "<!-- think -->", close: "<!-- /think -->"},
	}

	lower := strings.ToLower(text)
	for _, pair := range markerPairs {
		if strings.Contains(lower, pair.open) || strings.Contains(lower, pair.close) {
			thinkingPresent = true
			break
		}
	}

	if text == "" {
		return "", "", thinkingPresent
	}

	var visible strings.Builder
	segments := make([]string, 0, 2)
	pos := 0

	for pos < len(text) {
		nextStart := -1
		nextOpen := ""
		nextClose := ""

		for _, pair := range markerPairs {
			idx := strings.Index(lower[pos:], pair.open)
			if idx < 0 {
				continue
			}
			abs := pos + idx
			if nextStart == -1 || abs < nextStart {
				nextStart = abs
				nextOpen = pair.open
				nextClose = pair.close
			}
		}

		if nextStart < 0 {
			visible.WriteString(text[pos:])
			break
		}

		visible.WriteString(text[pos:nextStart])
		innerStart := nextStart + len(nextOpen)
		cursor := innerStart
		depth := 1

		for cursor <= len(text) {
			openIdx := strings.Index(lower[cursor:], nextOpen)
			closeIdx := strings.Index(lower[cursor:], nextClose)

			if closeIdx < 0 {
				depth = -1
				break
			}

			openAbs := -1
			if openIdx >= 0 {
				openAbs = cursor + openIdx
			}
			closeAbs := cursor + closeIdx

			if openAbs >= 0 && openAbs < closeAbs {
				depth++
				cursor = openAbs + len(nextOpen)
				continue
			}

			depth--
			if depth == 0 {
				segment := strings.TrimSpace(text[innerStart:closeAbs])
				if segment != "" {
					segments = append(segments, segment)
				}
				pos = closeAbs + len(nextClose)
				break
			}
			cursor = closeAbs + len(nextClose)
		}

		if depth != 0 {
			visible.WriteString(text[nextStart:])
			break
		}
	}

	cleanVisible := visible.String()
	marker := regexp.MustCompile(`(?is)</?think>|</?analysis>|<!--\s*/?\s*think\s*-->`)
	cleanVisible = marker.ReplaceAllString(cleanVisible, "")

	if len(segments) > 0 {
		thinkingText = strings.Join(segments, "\n\n")
	}

	return strings.TrimSpace(cleanVisible), strings.TrimSpace(thinkingText), thinkingPresent
}

func parseToolDirective(message string, allowedTools []string) (agent.ModelResponse, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(message, "/tool "))
	parts := strings.SplitN(rest, " ", 2)
	toolName, ok := toolparse.CanonicalToolName(parts[0])
	if !ok {
		return agent.ModelResponse{}, fmt.Errorf("unsupported tool name: %s", strings.TrimSpace(parts[0]))
	}
	if !toolparse.IsAllowed(toolName, allowedTools) {
		return agent.ModelResponse{}, fmt.Errorf("tool %q is not allowed", toolName)
	}
	if toolName == "" {
		return agent.ModelResponse{}, errors.New("tool name is required")
	}
	args := map[string]any{}
	if len(parts) == 2 {
		parsed, err := parseToolArgsJSONRelaxed(parts[1])
		if err != nil {
			return agent.ModelResponse{}, fmt.Errorf("invalid tool args JSON: %w", err)
		}
		args = parsed
	}
	args = normalizeToolArgs(toolName, args)
	argBytes, _ := json.Marshal(args)
	return agent.ModelResponse{ToolCalls: []agent.ToolCallRequest{{ID: "tool-1", Name: toolName, Arguments: argBytes}}}, nil
}

func parseToolCallsFromResponse(content string, allowedTools []string, trace *runTraceCollector) ([]agent.ToolCallRequest, bool, string) {
	parsedCalls, diag := toolparse.ParseToolCalls(content, allowedTools)
	parsedCalls = normalizeParsedToolCalls(parsedCalls)
	if len(parsedCalls) > maxToolCallsPerReply {
		parsedCalls = parsedCalls[:maxToolCallsPerReply]
	}
	if trace != nil {
		for _, extraction := range diag.Candidates {
			trace.RecordToolExtraction(extraction.RawSnippet, extraction.ParsedToolName, extraction.ParsedArguments, extraction.Accepted, extraction.Reason)
		}
	}
	parseFailure := len(parsedCalls) == 0 && len(diag.Rejected) > 0
	return parsedCalls, parseFailure, summarizeParseFailureReasons(diag.Rejected)
}

func normalizeParsedToolCalls(calls []agent.ToolCallRequest) []agent.ToolCallRequest {
	if len(calls) == 0 {
		return calls
	}
	out := make([]agent.ToolCallRequest, 0, len(calls))
	for _, call := range calls {
		normalized := call
		args := map[string]any{}
		if err := json.Unmarshal(call.Arguments, &args); err == nil {
			args = normalizeToolArgs(call.Name, args)
			if b, err := json.Marshal(args); err == nil {
				normalized.Arguments = b
			}
		}
		out = append(out, normalized)
	}
	return dedupeToolCalls(out)
}

func parseToolArgsJSONRelaxed(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}, nil
	}

	args := map[string]any{}
	if err := json.Unmarshal([]byte(trimmed), &args); err == nil {
		return args, nil
	} else {
		parseErr := err
		repaired := repairLikelyJSONObject(trimmed)
		if repaired == "" || repaired == trimmed {
			return nil, parseErr
		}
		if err := json.Unmarshal([]byte(repaired), &args); err != nil {
			return nil, err
		}
		return args, nil
	}
}

func repairLikelyJSONObject(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	text = strings.Trim(text, "`")
	text = strings.TrimSpace(text)

	if !strings.HasPrefix(text, "{") {
		start := strings.Index(text, "{")
		if start >= 0 {
			text = strings.TrimSpace(text[start:])
		}
	}

	if !strings.HasPrefix(text, "{") {
		return strings.TrimSpace(text)
	}

	text = stripTrailingJSONCommas(text)
	text = closeOpenJSONDelimiters(text)
	return strings.TrimSpace(text)
}

func stripTrailingJSONCommas(text string) string {
	if text == "" {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	inString := false
	escaped := false

	for i := 0; i < len(text); i++ {
		ch := text[i]
		if inString {
			b.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			b.WriteByte(ch)
			continue
		}
		if ch == ',' {
			j := i + 1
			for j < len(text) {
				next := text[j]
				if next == ' ' || next == '\n' || next == '\r' || next == '\t' {
					j++
					continue
				}
				break
			}
			if j < len(text) && (text[j] == '}' || text[j] == ']') {
				continue
			}
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func closeOpenJSONDelimiters(text string) string {
	if text == "" {
		return text
	}
	inString := false
	escaped := false
	stack := make([]byte, 0, 16)

	for i := 0; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == '{' || ch == '[' {
			stack = append(stack, ch)
			continue
		}
		if ch == '}' || ch == ']' {
			if len(stack) == 0 {
				continue
			}
			last := stack[len(stack)-1]
			if (ch == '}' && last == '{') || (ch == ']' && last == '[') {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if inString {
		text += `"`
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			text += "}"
		} else if stack[i] == '[' {
			text += "]"
		}
	}
	return text
}

func summarizeParseFailureReasons(rejected []toolparse.Extraction) string {
	if len(rejected) == 0 {
		return ""
	}

	reasons := make([]string, 0, len(rejected))
	seen := map[string]struct{}{}
	for _, entry := range rejected {
		reason := strings.TrimSpace(entry.Reason)
		if reason == "" {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		reasons = append(reasons, reason)
	}
	if len(reasons) == 0 {
		return ""
	}
	sort.Strings(reasons)
	if len(reasons) > 3 {
		reasons = reasons[:3]
	}
	return strings.Join(reasons, "; ")
}

func mergeParseFailureReasons(base, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	if base == "" {
		return extra
	}
	if extra == "" {
		return base
	}
	if strings.Contains(base, extra) {
		return base
	}
	if strings.Contains(extra, base) {
		return extra
	}
	return base + "; " + extra
}

func formatToolParseFailureUserMessage(visibleText, parseReason string) (string, bool) {
	text := strings.ToLower(strings.TrimSpace(visibleText))
	if text == "" {
		return "", false
	}

	toolAttempt := strings.Contains(text, `"tool_name"`) ||
		strings.Contains(text, `"arguments"`) ||
		strings.Contains(text, "```json") ||
		strings.Contains(text, `"function"`)
	if !toolAttempt {
		return "", false
	}

	reason := strings.TrimSpace(parseReason)
	if matches := toolNotAllowedReasonRE.FindStringSubmatch(reason); len(matches) == 2 {
		toolName := strings.TrimSpace(matches[1])
		if toolName == "http.request" {
			return "I tried to call `http.request`, but that tool is not enabled for this agent right now. Enable `network.enabled=true` (and keep the domain allowlist set), then retry.", true
		}
		if toolName == "shell.exec" {
			return "I tried to call `shell.exec`, but it is not enabled for this agent right now. Enable `shell.enable_exec=true` and a sandbox provider, then retry.", true
		}
		return fmt.Sprintf("I tried to call `%s`, but that tool is not enabled for this agent right now. Please enable it and retry.", toolName), true
	}

	if reason != "" {
		return "I attempted a tool call, but the payload could not be executed (" + reason + "). Please retry and I will run it directly.", true
	}
	return "I attempted a tool call, but the payload could not be executed. Please retry and I will run it directly.", true
}

func parseLooseJSONToolCalls(content string, allowedTools []string, trace *runTraceCollector) []agent.ToolCallRequest {
	candidates := extractJSONObjectCandidates(content)
	if len(candidates) == 0 {
		return nil
	}

	calls := make([]agent.ToolCallRequest, 0, len(candidates))
	for _, raw := range candidates {
		wrapped := "```json\n" + strings.TrimSpace(raw) + "\n```"
		parsed := toolparse.ParseStrict(wrapped, allowedTools, 1)
		if trace != nil {
			if len(parsed.Extractions) == 0 {
				trace.RecordToolExtraction(raw, "", nil, false, "invalid json object candidate")
			} else {
				extraction := parsed.Extractions[0]
				trace.RecordToolExtraction(raw, extraction.ParsedToolName, extraction.ParsedArguments, extraction.Accepted, "loose-json: "+extraction.Reason)
			}
		}
		if len(parsed.Calls) == 0 {
			continue
		}
		call := parsed.Calls[0]
		call.ID = fmt.Sprintf("tool-json-loose-%d", len(calls)+1)
		calls = append(calls, call)
		if len(calls) >= maxToolCallsPerReply {
			break
		}
	}
	return dedupeToolCalls(calls)
}

func synthesizeWriteCallFromResponse(content, userMessage string, ordinal int) (agent.ToolCallRequest, bool) {
	if !looksLikeCreateRequest(userMessage) {
		return agent.ToolCallRequest{}, false
	}

	path := extractFilenameFromText(userMessage)
	if path == "" {
		path = extractFilenameFromText(content)
	}
	if path == "" {
		return agent.ToolCallRequest{}, false
	}

	body := extractCodeBody(content)
	if strings.TrimSpace(body) == "" {
		return agent.ToolCallRequest{}, false
	}

	argBytes, _ := json.Marshal(map[string]any{
		"path":    path,
		"content": body,
	})
	return agent.ToolCallRequest{
		ID:        fmt.Sprintf("tool-synth-%d", ordinal),
		Name:      "fs.write",
		Arguments: argBytes,
	}, true
}

func looksLikeCreateRequest(message string) bool {
	m := strings.ToLower(strings.TrimSpace(message))
	if m == "" {
		return false
	}
	if !strings.Contains(m, "create") && !strings.Contains(m, "write") && !strings.Contains(m, "make") && !strings.Contains(m, "save") {
		return false
	}
	return regexp.MustCompile(`\.[A-Za-z0-9]{1,8}\b`).MatchString(m)
}

func extractFilenameFromText(text string) string {
	re := regexp.MustCompile(`([A-Za-z0-9_./-]+\.[A-Za-z0-9]{1,8})`)
	matches := re.FindAllString(text, -1)
	for _, m := range matches {
		m = strings.TrimSpace(strings.Trim(m, `"'`))
		if strings.HasPrefix(m, "http://") || strings.HasPrefix(m, "https://") {
			continue
		}
		if strings.Contains(m, "..") {
			continue
		}
		return m
	}
	return ""
}

func extractCodeBody(content string) string {
	fenced := regexp.MustCompile("```(?:[A-Za-z0-9_+-]+)?\\s*([\\s\\S]*?)```")
	blocks := fenced.FindAllStringSubmatch(content, -1)
	if len(blocks) > 0 {
		best := ""
		for _, block := range blocks {
			if len(block) < 2 {
				continue
			}
			candidate := strings.TrimSpace(block[1])
			if len(candidate) > len(best) {
				best = candidate
			}
		}
		if best != "" {
			return best
		}
	}

	lines := strings.Split(content, "\n")
	if len(lines) >= 4 {
		if strings.HasPrefix(strings.TrimSpace(lines[0]), "#") {
			return strings.TrimSpace(strings.Join(lines[1:], "\n"))
		}
	}

	return ""
}

func parseBashCodeBlocks(content string, ordinalStart int) []agent.ToolCallRequest {
	re := regexp.MustCompile("(?is)```(?:bash|sh)\\s*([\\s\\S]*?)```")
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	calls := make([]agent.ToolCallRequest, 0, len(matches))
	nextOrdinal := ordinalStart
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		block := strings.TrimSpace(match[1])
		if block == "" {
			continue
		}

		lines := strings.Split(block, "\n")
		cleaned := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "$ ") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "$ "))
			}
			if line == "" {
				continue
			}
			cleaned = append(cleaned, line)
		}
		if len(cleaned) == 0 {
			continue
		}

		script := strings.Join(cleaned, "\n")
		argBytes, _ := json.Marshal(map[string]any{
			"command": "bash",
			"args":    []string{"-lc", script},
		})
		calls = append(calls, agent.ToolCallRequest{
			ID:        fmt.Sprintf("tool-bash-%d", nextOrdinal),
			Name:      "shell.exec",
			Arguments: argBytes,
		})
		nextOrdinal++
	}

	return calls
}

func removeFencedCodeBlocks(content string) string {
	re := regexp.MustCompile("```(?:[A-Za-z0-9_+-]+)?\\s*[\\s\\S]*?```")
	return re.ReplaceAllString(content, "")
}

func parseShellSnippets(content string, ordinalStart int) []agent.ToolCallRequest {
	listRe := regexp.MustCompile(`(?m)^\s*(?:\$\s*)?ls(?:\s+-[A-Za-z]+)*\s*(.*?)\s*$`)
	catRe := regexp.MustCompile(`(?m)^\s*(?:\$\s*)?cat\s+([^\s]+)\s*$`)

	calls := make([]agent.ToolCallRequest, 0, 4)
	nextOrdinal := ordinalStart

	listMatches := listRe.FindAllStringSubmatch(content, -1)
	for _, match := range listMatches {
		path := "."
		if len(match) >= 2 {
			rest := strings.TrimSpace(match[1])
			if rest != "" {
				parts := strings.Fields(rest)
				if len(parts) > 0 {
					path = parts[len(parts)-1]
				}
			}
		}
		argBytes, _ := json.Marshal(map[string]any{"path": path})
		calls = append(calls, agent.ToolCallRequest{ID: fmt.Sprintf("tool-shell-%d", nextOrdinal), Name: "fs.list", Arguments: argBytes})
		nextOrdinal++
	}

	catMatches := catRe.FindAllStringSubmatch(content, -1)
	for _, match := range catMatches {
		if len(match) < 2 {
			continue
		}
		path := strings.TrimSpace(match[1])
		if path == "" {
			continue
		}
		argBytes, _ := json.Marshal(map[string]any{"path": path})
		calls = append(calls, agent.ToolCallRequest{ID: fmt.Sprintf("tool-shell-%d", nextOrdinal), Name: "fs.read", Arguments: argBytes})
		nextOrdinal++
	}

	return calls
}

func dedupeToolCalls(calls []agent.ToolCallRequest) []agent.ToolCallRequest {
	if len(calls) < 2 {
		return calls
	}
	seen := make(map[string]struct{}, len(calls))
	out := make([]agent.ToolCallRequest, 0, len(calls))
	for _, call := range calls {
		key := call.Name + "|" + string(call.Arguments)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, call)
	}
	return out
}

func parseJSONToolCall(raw string, ordinal int) (agent.ToolCallRequest, bool) {
	obj := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return agent.ToolCallRequest{}, false
	}

	toolName, ok := canonicalToolName(firstString(obj, "tool_name", "tool", "tool_code", "name", "function", "function_name"))
	if !ok || toolName == "" {
		return agent.ToolCallRequest{}, false
	}

	args := map[string]any{}
	if nested, ok := obj["arguments"].(map[string]any); ok {
		for k, v := range nested {
			args[k] = v
		}
	} else if nested, ok := obj["args"].(map[string]any); ok {
		for k, v := range nested {
			args[k] = v
		}
	} else if nested, ok := obj["parameters"].(map[string]any); ok {
		for k, v := range nested {
			args[k] = v
		}
	} else {
		for k, v := range obj {
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "tool", "tool_name", "tool_code", "name", "function", "function_name", "id", "arguments", "args", "parameters":
				continue
			default:
				args[k] = v
			}
		}
	}

	argBytes, _ := json.Marshal(args)
	return agent.ToolCallRequest{
		ID:        fmt.Sprintf("tool-json-%d", ordinal),
		Name:      toolName,
		Arguments: argBytes,
	}, true
}

func extractJSONObjectCandidates(content string) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		if json.Valid([]byte(trimmed)) {
			return []string{trimmed}
		}
	}

	candidates := make([]string, 0, 4)
	start := -1
	depth := 0
	inString := false
	escaped := false

	for i, r := range content {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}

		if r == '"' {
			inString = true
			continue
		}

		if r == '{' {
			if depth == 0 {
				start = i
			}
			depth++
			continue
		}

		if r == '}' && depth > 0 {
			depth--
			if depth == 0 && start >= 0 {
				raw := strings.TrimSpace(content[start : i+1])
				if json.Valid([]byte(raw)) {
					candidates = append(candidates, raw)
				}
				start = -1
			}
		}
	}

	return candidates
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := obj[key].(string); ok {
			return v
		}
	}
	return ""
}

func parseFunctionCall(content string) *agent.ToolCallRequest {
	// Parse function call syntax like: fs.list(path=".") or fs.list(".")
	re := regexp.MustCompile(`^(\w+\.\w+)\s*\((.*)\)$`)
	matches := re.FindStringSubmatch(strings.TrimSpace(content))
	if len(matches) < 2 {
		return nil
	}

	toolName, ok := canonicalToolName(matches[1])
	if !ok {
		return nil
	}
	argsStr := ""
	if len(matches) >= 3 {
		argsStr = matches[2]
	}

	args := parseArgsString(argsStr)
	argBytes, _ := json.Marshal(args)

	return &agent.ToolCallRequest{
		ID:        fmt.Sprintf("tool-func-%d", time.Now().UnixNano()),
		Name:      toolName,
		Arguments: argBytes,
	}
}

func parseArgsString(argsStr string) map[string]any {
	args := map[string]any{}
	argsStr = strings.TrimSpace(argsStr)

	if argsStr == "" {
		return args
	}

	// Try to parse as JSON first
	if err := json.Unmarshal([]byte(argsStr), &args); err == nil {
		return args
	}

	// Parse key=value or key:value pairs
	// Pattern: key="value", key='value', key=value, key:value, or just "value" (positional)
	re := regexp.MustCompile(`(\w+)\s*[:=]\s*("[^"]*"|'[^']*'|[^,\s]+)`)
	pairs := re.FindAllStringSubmatch(argsStr, -1)

	for _, pair := range pairs {
		if len(pair) >= 3 {
			key := strings.TrimSpace(pair[1])
			value := strings.TrimSpace(pair[2])
			// Remove quotes
			if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
				(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
				value = value[1 : len(value)-1]
			}
			args[key] = value
		}
	}

	// If no key=value pairs found, treat as single positional argument "path"
	if len(args) == 0 && argsStr != "" {
		value := strings.TrimSpace(argsStr)
		if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
			(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
			value = value[1 : len(value)-1]
		}
		args["path"] = value
	}

	return args
}

func requestMessages(req agent.ModelRequest) []agent.ChatMessage {
	if len(req.Messages) > 0 {
		return append([]agent.ChatMessage(nil), req.Messages...)
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return nil
	}
	return []agent.ChatMessage{{Role: "user", Content: msg}}
}

func lastUserMessage(messages []agent.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		role := strings.ToLower(strings.TrimSpace(messages[i].Role))
		if role == "" || role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	if len(messages) == 0 {
		return ""
	}
	return strings.TrimSpace(messages[len(messages)-1].Content)
}

func isToolAllowed(toolName string, allowed []string) bool {
	if toolName == "tool.result" {
		return false
	}
	if len(allowed) == 0 {
		return true
	}
	for _, raw := range allowed {
		candidate, ok := canonicalToolName(raw)
		if !ok {
			candidate = strings.ToLower(strings.TrimSpace(raw))
		}
		if candidate == toolName {
			return true
		}
	}
	return false
}

func providerEndpoint(cfg config.Config, provider string) (config.ProviderEndpointConfig, error) {
	switch provider {
	case "openai":
		return cfg.Providers.OpenAI, nil
	case "openrouter":
		return cfg.Providers.OpenRouter, nil
	case "requesty":
		return cfg.Providers.Requesty, nil
	case "zai":
		return cfg.Providers.ZAI, nil
	case "generic":
		return cfg.Providers.Generic, nil
	default:
		return config.ProviderEndpointConfig{}, fmt.Errorf("unsupported provider: %s", provider)
	}
}

func canonicalToolName(name string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return "", false
	}
	canonical, ok := toolNameAliases[key]
	if !ok {
		return "", false
	}
	return canonical, true
}
