package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"openclawssy/internal/config"
)

type RunStatus struct {
	Status       string
	Output       string
	Error        string
	ArtifactPath string
	Trace        map[string]any
}

type RunStatusFunc func(ctx context.Context, runID string) (RunStatus, error)

func NormalizeInboundMessage(content, commandPrefix string) string {
	if content == "" {
		return ""
	}
	if strings.HasPrefix(content, "/") {
		return strings.TrimSpace(content)
	}
	if commandPrefix == "" {
		return strings.TrimSpace(content)
	}
	if !strings.HasPrefix(content, commandPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(content, commandPrefix))
}

func ParseThinkingOverride(content string) (string, string, error) {
	clean := strings.TrimSpace(content)
	if clean == "" {
		return "", "", errors.New("message is required")
	}
	// Strip optional /ask prefix so thinking= overrides still work when the
	// command is sent (e.g. "/ask thinking=always summarize").
	lower := strings.ToLower(clean)
	if strings.HasPrefix(lower, "/ask") {
		clean = strings.TrimSpace(clean[4:])
		if clean == "" {
			return "", "", errors.New("message is required")
		}
	} else if strings.HasPrefix(clean, "/") {
		// Any other slash command passes through without thinking-mode parsing.
		return clean, "", nil
	}
	parts := strings.Fields(clean)
	if len(parts) == 0 {
		return "", "", errors.New("message is required")
	}
	first := strings.ToLower(strings.TrimSpace(parts[0]))
	if !strings.HasPrefix(first, "thinking=") {
		return clean, "", nil
	}
	rawMode := strings.TrimSpace(strings.TrimPrefix(parts[0], "thinking="))
	normalized := config.NormalizeThinkingMode(rawMode)
	if !config.IsValidThinkingMode(normalized) {
		return "", "", fmt.Errorf("request.invalid_thinking_mode: thinking must be one of never|on_error|always")
	}
	clean = strings.TrimSpace(strings.TrimPrefix(clean, parts[0]))
	if clean == "" {
		return "", "", errors.New("message is required")
	}
	return clean, normalized, nil
}

func FormatBridgeError(err error, allowlistScope string) string {
	if err == nil {
		return "request failed"
	}
	if retryAfter := RetryAfterFromError(err); retryAfter > 0 {
		return FormatRateLimit(retryAfter)
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		msg = "request failed"
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "rate limited") {
		return "rate limited, try again soon"
	}
	if strings.Contains(lower, "not allowlisted") {
		scope := strings.TrimSpace(allowlistScope)
		if scope == "" {
			scope = "channel or user scope"
		}
		return "not allowed in this " + scope
	}
	if strings.Contains(lower, "run queue is full") {
		return "run queue is full, retry shortly"
	}
	if strings.Contains(msg, "request.invalid_thinking_mode") {
		return "error[request.invalid_thinking_mode]: thinking must be one of never|on_error|always"
	}
	return "request failed: " + msg
}

type retryAfterError interface {
	RetryAfter() time.Duration
}

func RetryAfterFromError(err error) time.Duration {
	var cooldown retryAfterError
	if errors.As(err, &cooldown) {
		return cooldown.RetryAfter()
	}
	return 0
}

func FormatRateLimit(retryAfter time.Duration) string {
	if retryAfter <= 0 {
		return "rate limited, try again soon"
	}
	seconds := int(retryAfter / time.Second)
	if retryAfter%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("rate limited, retry in %ds", seconds)
}

func WaitForTerminalRun(ctx context.Context, runID string, runStatus RunStatusFunc, interval time.Duration, defaultInterval time.Duration) (RunStatus, error) {
	if runStatus == nil {
		return RunStatus{}, errors.New("run status lookup is not configured")
	}
	if interval <= 0 {
		interval = defaultInterval
	}

	for {
		run, err := runStatus(ctx, runID)
		if err != nil {
			return RunStatus{}, err
		}
		switch strings.ToLower(strings.TrimSpace(run.Status)) {
		case "completed", "failed":
			return run, nil
		}

		select {
		case <-ctx.Done():
			return RunStatus{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func FormatToolActivity(runID string, trace map[string]any) string {
	if len(trace) == 0 {
		return ""
	}
	rawEntries, ok := trace["tool_execution_results"].([]any)
	if !ok || len(rawEntries) == 0 {
		return ""
	}

	lines := []string{"Tool activity for run `" + strings.TrimSpace(runID) + "`:"}
	for i, raw := range rawEntries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tool := strings.TrimSpace(fmt.Sprintf("%v", entry["tool"]))
		if tool == "" {
			tool = "unknown.tool"
		}
		callID := strings.TrimSpace(fmt.Sprintf("%v", entry["tool_call_id"]))
		summary := fmt.Sprintf("%d) %s", i+1, tool)
		if callID != "" && callID != "<nil>" {
			summary += " [" + callID + "]"
		}
		if short := strings.TrimSpace(fmt.Sprintf("%v", entry["summary"])); short != "" && short != "<nil>" {
			summary += " -> " + short
		} else if errText := strings.TrimSpace(fmt.Sprintf("%v", entry["error"])); errText != "" && errText != "<nil>" {
			summary += " -> error: " + errText
		} else if outText := strings.TrimSpace(fmt.Sprintf("%v", entry["output"])); outText != "" && outText != "<nil>" {
			if len(outText) > 180 {
				outText = outText[:180] + "..."
			}
			summary += " -> output: " + outText
		}
		lines = append(lines, summary)
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func SplitMessage(text string, maxLen int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return []string{"(empty)"}
	}
	if maxLen <= 0 {
		maxLen = 1900
	}

	var out []string
	remaining := trimmed
	for len(remaining) > maxLen {
		cut := strings.LastIndex(remaining[:maxLen], "\n")
		if cut <= 0 {
			cut = maxLen
		}
		part := strings.TrimSpace(remaining[:cut])
		if part != "" {
			out = append(out, part)
		}
		remaining = strings.TrimSpace(remaining[cut:])
	}
	if remaining != "" {
		out = append(out, remaining)
	}
	if len(out) == 0 {
		return []string{"(empty)"}
	}
	return out
}
