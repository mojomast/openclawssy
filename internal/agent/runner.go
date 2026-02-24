package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrModelRequired            = errors.New("agent runner requires model")
	ErrToolExecutorRequired     = errors.New("agent runner requires tool executor for tool calls")
	ErrToolIterationCapExceeded = errors.New("agent runner tool iteration cap exceeded")
)

const (
	DefaultToolIterationCap          = 120
	DefaultToolTimeout               = 900 * time.Second
	repeatedNoProgressLoopCapTrigger = 6
	failureRecoveryTrigger           = 2
	failureGuidanceEscalation        = 3
	followThroughRepromptCap         = 5
)

// Runner executes the model/tool loop for a single run.
type Runner struct {
	Model             Model
	ToolExecutor      ToolExecutor
	PromptAssembler   func([]ArtifactDoc, int) string
	MaxToolIterations int
}

// Run executes: input -> assemble prompt -> model call -> optional tools -> finalize.
func (r Runner) Run(ctx context.Context, input RunInput) (RunOutput, error) {
	if r.Model == nil {
		return RunOutput{}, ErrModelRequired
	}

	state := newRunState(input, r)
	return state.runLoop(ctx, r, input)
}

func shouldForceFollowThrough(finalText string, allowedTools []string, toolResults []ToolCallResult) bool {
	if len(allowedTools) == 0 || len(toolResults) > 0 {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(finalText))
	if text == "" || len(text) > 480 {
		return false
	}

	if strings.Contains(text, "can't") ||
		strings.Contains(text, "cannot") ||
		strings.Contains(text, "unable") ||
		strings.Contains(text, "permission") ||
		strings.Contains(text, "missing") ||
		strings.Contains(text, "blocked") {
		return false
	}

	deferralPhrases := []string{
		"let me",
		"let me try",
		"let me check",
		"let me verify",
		"let me look",
		"i will",
		"i'll",
		"i am going to",
		"i'm going to",
		"give me a moment",
		"hold on",
	}
	for _, phrase := range deferralPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}

	return false
}

func nonActionableFinalText(lastText string) string {
	_ = lastText
	return "I could not complete an actionable execution step in time. Please retry and I will run it directly and report concrete results."
}

func finalizeFromToolResults(ctx context.Context, model Model, agentID, runID, prompt string, messages []ChatMessage, message string, toolTimeoutMS int, toolResults []ToolCallResult, extender SystemPromptExtender, extraDirective string) string {
	if model == nil || len(toolResults) == 0 {
		return ""
	}

	finalPrompt := strings.TrimSpace(prompt)
	if extender != nil {
		if extended := strings.TrimSpace(extender(ctx, finalPrompt, append([]ChatMessage(nil), messages...), message, append([]ToolCallResult(nil), toolResults...))); extended != "" {
			finalPrompt = extended
		}
	}
	if finalPrompt != "" {
		finalPrompt += "\n\n"
	}
	finalPrompt += "# FINAL_RESPONSE_MODE\n- Do not call tools in this turn.\n- Use the latest tool results to answer the user directly.\n- If some commands failed, explain the failure and give the best next step."
	if strings.TrimSpace(extraDirective) != "" {
		finalPrompt += "\n\n" + strings.TrimSpace(extraDirective)
	}

	resp, err := model.Generate(ctx, ModelRequest{
		AgentID:       agentID,
		RunID:         runID,
		SystemPrompt:  finalPrompt,
		Messages:      append([]ChatMessage(nil), messages...),
		AllowedTools:  nil,
		ToolTimeoutMS: toolTimeoutMS,
		Prompt:        finalPrompt,
		Message:       message,
		ToolResults:   append([]ToolCallResult(nil), toolResults...),
	})
	if err != nil {
		return ""
	}
	if len(resp.ToolCalls) > 0 {
		return ""
	}
	return strings.TrimSpace(resp.FinalText)
}

func fallbackFromToolResults(results []ToolCallResult, toolCap int) string {
	if len(results) == 0 {
		return "I hit the tool iteration limit before I could finish."
	}

	var b strings.Builder
	b.WriteString("I reached the tool-iteration limit before producing a final response. Here are the latest tool results:\n")
	b.WriteString(formatLatestToolResults(results))
	b.WriteString(fmt.Sprintf("\n(Iteration cap: %d)", toolCap))
	return b.String()
}

func recoverFromModelError(err error, toolResults []ToolCallResult, toolCap int) string {
	if isProviderNoChoicesError(err) {
		return recoverFromNoChoices(toolResults, toolCap)
	}
	msg := strings.TrimSpace("I hit a model/API error while processing the next step: " + strings.TrimSpace(err.Error()))
	if len(toolResults) == 0 {
		return msg
	}
	return msg + "\n\nLatest tool results before the model/API error:\n" + formatLatestToolResults(toolResults) + fmt.Sprintf("\n(Iteration cap: %d)", toolCap)
}

func recoverFromNoChoices(toolResults []ToolCallResult, toolCap int) string {
	if len(toolResults) == 0 {
		return "I couldn't get a complete response from the model this time. Please try again."
	}
	if latestToolResultsAreEmptySearches(toolResults) {
		return "I couldn't get a complete model response, but the latest search attempts found no matching entries."
	}
	return "I couldn't get a complete model response, but here are the latest tool results:\n" + formatLatestToolResults(toolResults) + fmt.Sprintf("\n(Iteration cap: %d)", toolCap)
}

func latestToolResultsAreEmptySearches(results []ToolCallResult) bool {
	if len(results) == 0 {
		return false
	}
	start := len(results) - 3
	if start < 0 {
		start = 0
	}
	checked := 0
	for i := start; i < len(results); i++ {
		item := results[i]
		if strings.TrimSpace(item.Error) != "" {
			return false
		}
		out := strings.ToLower(strings.TrimSpace(item.Output))
		if out == "" {
			return false
		}
		if !strings.Contains(out, `"count":0`) || !strings.Contains(out, `"items":[]`) {
			return false
		}
		checked++
	}
	return checked > 0
}

func formatLatestToolResults(results []ToolCallResult) string {
	if len(results) == 0 {
		return ""
	}

	start := len(results) - 5
	if start < 0 {
		start = 0
	}

	var b strings.Builder
	for i := start; i < len(results); i++ {
		item := results[i]
		idx := i + 1
		if strings.TrimSpace(item.Error) != "" {
			b.WriteString(fmt.Sprintf("- %d) error: %s\n", idx, strings.TrimSpace(item.Error)))
			out := strings.TrimSpace(item.Output)
			if len(out) > 1200 {
				out = out[:1200] + "..."
			}
			if out != "" {
				b.WriteString(fmt.Sprintf("  output: %s\n", out))
			}
			continue
		}
		out := strings.TrimSpace(item.Output)
		if len(out) > 1200 {
			out = out[:1200] + "..."
		}
		if out == "" {
			out = "(empty output)"
		}
		b.WriteString(fmt.Sprintf("- %d) output: %s\n", idx, out))
	}
	return b.String()
}

func isToolTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "deadline exceeded")
}

func appendPromptDirective(prompt, directive string) string {
	base := strings.TrimSpace(prompt)
	extra := strings.TrimSpace(directive)
	if extra == "" {
		return base
	}
	if base == "" {
		return extra
	}
	return base + "\n\n" + extra
}

func requestUserGuidanceFromFailures(userMessage string, records []ToolCallRecord) string {
	if prompt, ok := networkAllowlistPermissionPrompt(userMessage, records); ok {
		return prompt
	}

	var b strings.Builder
	b.WriteString("I encountered an issue and need your help to continue.\n")
	goal := strings.TrimSpace(userMessage)
	if goal != "" {
		b.WriteString("Goal: ")
		b.WriteString(goal)
		b.WriteString("\n")
	}

	failing := make([]ToolCallRecord, 0, 6)
	for i := len(records) - 1; i >= 0 && len(failing) < 6; i-- {
		if strings.TrimSpace(records[i].Result.Error) == "" {
			continue
		}
		failing = append(failing, records[i])
	}
	if len(failing) == 0 {
		b.WriteString("Please tell me how you want to proceed.")
		return b.String()
	}

	b.WriteString("What I tried:\n")
	for i := len(failing) - 1; i >= 0; i-- {
		rec := failing[i]
		attempt := rec.Request.Name
		args := truncateGuidanceText(strings.TrimSpace(string(rec.Request.Arguments)), 120)
		if args != "" {
			attempt += " " + args
		}
		b.WriteString(fmt.Sprintf("- %s\n", attempt))
	}
	b.WriteString("\nPlease tell me how you'd like to proceed.")
	return b.String()
}

func networkAllowlistPermissionPrompt(userMessage string, records []ToolCallRecord) (string, bool) {
	hostSet := map[string]struct{}{}
	for _, rec := range records {
		toolName := strings.TrimSpace(strings.ToLower(rec.Request.Name))
		if toolName != "http.request" && toolName != "net.fetch" {
			continue
		}
		errText := strings.TrimSpace(rec.Result.Error)
		if errText == "" {
			continue
		}
		host := extractNetworkAllowlistDeniedHost(errText)
		if host == "" {
			continue
		}
		hostSet[host] = struct{}{}
	}
	if len(hostSet) == 0 {
		return "", false
	}

	hosts := make([]string, 0, len(hostSet))
	for host := range hostSet {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)

	goal := strings.TrimSpace(userMessage)
	quoted := make([]string, 0, len(hosts))
	for _, host := range hosts {
		quoted = append(quoted, "`"+host+"`")
	}

	var b strings.Builder
	b.WriteString("I can continue, but I need your permission first.\n")
	if goal != "" {
		b.WriteString("Goal: ")
		b.WriteString(goal)
		b.WriteString("\n")
	}
	b.WriteString("The network tool is blocked because these hosts are not in `network.allowed_domains`: ")
	b.WriteString(strings.Join(quoted, ", "))
	b.WriteString(".\n")
	b.WriteString("If you approve, reply exactly: `yes, add allowed domains` and I will add them via `config.set` and retry immediately.\n")
	b.WriteString("Note: Use config.set with the format: `config.set {\"updates\": {\"network.allowed_domains\": [\"domain1\", \"domain2\", ...]}}`")
	return b.String(), true
}

func extractNetworkAllowlistDeniedHost(errText string) string {
	lower := strings.ToLower(strings.TrimSpace(errText))
	if !strings.Contains(lower, "is not in network.allowed_domains") {
		return ""
	}
	const prefix = "host \""
	start := strings.Index(lower, prefix)
	if start == -1 {
		return ""
	}
	start += len(prefix)
	endRel := strings.Index(lower[start:], "\"")
	if endRel <= 0 {
		return ""
	}
	host := strings.TrimSpace(lower[start : start+endRel])
	if host == "" {
		return ""
	}
	return host
}

func truncateGuidanceText(value string, maxChars int) string {
	text := strings.TrimSpace(value)
	if text == "" || maxChars <= 0 {
		return ""
	}
	if len(text) <= maxChars {
		return text
	}
	if maxChars <= 3 {
		return text[:maxChars]
	}
	return strings.TrimSpace(text[:maxChars-3]) + "..."
}

func toolResultErrorText(result ToolCallResult) string {
	if text := strings.TrimSpace(result.Error); text != "" {
		return text
	}
	raw := strings.TrimSpace(result.Output)
	if raw == "" || (!strings.HasPrefix(raw, "{") && !strings.HasPrefix(raw, "[")) {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	if v, ok := payload["error"]; ok {
		errText := strings.TrimSpace(fmt.Sprintf("%v", v))
		if errText != "" && errText != "<nil>" {
			return errText
		}
	}
	if v, ok := payload["exit_code"]; ok {
		switch n := v.(type) {
		case float64:
			if int(n) != 0 {
				return fmt.Sprintf("exit status %d", int(n))
			}
		case int:
			if n != 0 {
				return fmt.Sprintf("exit status %d", n)
			}
		}
	}
	return ""
}

func uniqueToolCallID(rawID string, ordinal int, used map[string]struct{}) string {
	base := strings.TrimSpace(rawID)
	if base == "" {
		base = fmt.Sprintf("tool-call-%d", ordinal)
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, exists := used[candidate]; !exists {
			used[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}
