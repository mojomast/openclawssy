package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	defaultRepeatedFileWriteCap = 5
	journalFileWriteCap         = 1
	journalFileReadCap          = 2
	buildLogFileWriteCap        = 2
	buildLogFileReadCap         = 2
	specFileReadCap             = 3
	agentMessageSendCap         = 1
	agentMessageInboxCap        = 2
	agentRunCap                 = 1
	noChoicesRetryCap           = 2
	noChoicesRetryDelay         = 200 * time.Millisecond
	transientModelRetryCap      = 2
	transientModelRetryDelay    = 250 * time.Millisecond
)

// runState encapsulates the mutable state of a single agent run loop.
type runState struct {
	out                     RunOutput
	messages                []ChatMessage
	toolResults             []ToolCallResult
	toolIterations          int
	toolCallOrdinal         int
	usedToolCallIDs         map[string]struct{}
	cachedToolResults       map[string]ToolCallResult
	cachedFailedToolResults map[string]ToolCallResult
	failedToolCallCounts    map[string]int
	failedToolCallErrors    map[string]string
	consecutiveToolFailures int
	failureRecoveryActive   bool
	failuresSinceRecovery   int
	successesSinceRecovery  int
	toolTimeout             time.Duration
	noProgressIterations    int
	latestThinking          string
	thinkingPresent         bool
	toolParseFailure        bool
	followThroughReprompts  int
	toolCap                 int
	// repetitionPrevention tracks tool calls to detect and prevent loops
	repetitionPrevention map[string]int // key: "tool_name|agent_id" -> count
}

func newRunState(input RunInput, r Runner) *runState {
	assembler := r.PromptAssembler
	if assembler == nil {
		assembler = AssemblePrompt
	}

	toolCap := input.MaxToolIterations
	if toolCap <= 0 {
		toolCap = r.MaxToolIterations
	}
	if toolCap <= 0 {
		toolCap = DefaultToolIterationCap
	}

	out := RunOutput{StartedAt: time.Now().UTC()}
	out.Prompt = assembler(input.ArtifactDocs, input.PerFileByteLimit)

	messages := append([]ChatMessage(nil), input.Messages...)
	if len(messages) == 0 {
		messages = []ChatMessage{{Role: "user", Content: input.Message}}
	}

	toolTimeout := time.Duration(input.ToolTimeoutMS) * time.Millisecond
	if toolTimeout <= 0 {
		toolTimeout = DefaultToolTimeout
	}

	return &runState{
		out:                     out,
		messages:                messages,
		toolResults:             make([]ToolCallResult, 0),
		usedToolCallIDs:         make(map[string]struct{}),
		cachedToolResults:       make(map[string]ToolCallResult),
		cachedFailedToolResults: make(map[string]ToolCallResult),
		failedToolCallCounts:    make(map[string]int),
		failedToolCallErrors:    make(map[string]string),
		toolTimeout:             toolTimeout,
		toolCap:                 toolCap,
		repetitionPrevention:    make(map[string]int),
	}
}

func (s *runState) registerToolOutcome(errText string) {
	if strings.TrimSpace(errText) == "" {
		s.consecutiveToolFailures = 0
		if s.failureRecoveryActive {
			s.successesSinceRecovery++
			if s.successesSinceRecovery >= 3 {
				s.failureRecoveryActive = false
				s.failuresSinceRecovery = 0
				s.successesSinceRecovery = 0
			}
		}
		return
	}
	s.successesSinceRecovery = 0
	s.consecutiveToolFailures++
	if !s.failureRecoveryActive && s.consecutiveToolFailures >= failureRecoveryTrigger {
		s.failureRecoveryActive = true
		s.failuresSinceRecovery = 0
		return
	}
	if s.failureRecoveryActive {
		s.failuresSinceRecovery++
	}
}

func (s *runState) notifyToolCall(record *ToolCallRecord, onToolCall func(ToolCallRecord) error) {
	if onToolCall == nil || record == nil {
		return
	}
	if err := onToolCall(*record); err != nil {
		record.CallbackErr = strings.TrimSpace(err.Error())
	}
}

func (s *runState) prepareSystemPrompt(ctx context.Context, input RunInput) string {
	systemPrompt := s.out.Prompt
	if s.failureRecoveryActive {
		systemPrompt = appendPromptDirective(systemPrompt, "# ERROR_RECOVERY_MODE\n- Recent tool calls failed. Analyze the latest errors and outputs before choosing the next action.\n- Try a materially different approach to resolve the error.\n- Do not repeat the same failing command/arguments unless you explain why it should now work.")
	}
	if s.followThroughReprompts > 0 {
		systemPrompt = appendPromptDirective(systemPrompt, "# ACTION_EXECUTION_MODE\n- You previously replied with intent to act but did not execute.\n- In this turn, either call required tools now or provide a concrete final answer from existing evidence.\n- Do not defer with phrases like 'let me check' or promise future action without execution.")
	}
	if input.SystemPromptExt != nil {
		extended := input.SystemPromptExt(ctx, systemPrompt, append([]ChatMessage(nil), s.messages...), input.Message, append([]ToolCallResult(nil), s.toolResults...))
		if strings.TrimSpace(extended) != "" {
			systemPrompt = extended
		}
	}
	return systemPrompt
}

func (s *runState) executeTools(ctx context.Context, r Runner, toolCalls []ToolCallRequest, input RunInput) bool {
	hadFreshExecution := false
	for _, incoming := range toolCalls {
		s.toolCallOrdinal++
		call := incoming
		call.ID = uniqueToolCallID(call.ID, s.toolCallOrdinal, s.usedToolCallIDs)

		if repetitionKey, cap, ok := repeatedCallRepetitionKey(call); ok {
			s.repetitionPrevention[repetitionKey]++
			if s.repetitionPrevention[repetitionKey] > cap {
				repetitionMsg := fmt.Sprintf("repetition detected: %s has been attempted %d times in this run. Stop rewriting and provide a final response from current evidence.", repetitionKey, s.repetitionPrevention[repetitionKey])
				if strings.HasPrefix(repetitionKey, "agent.run|") {
					repetitionMsg = fmt.Sprintf("repetition detected: %s has been attempted %d times in this run. Do not rerun this same subagent task; move to the next required subagent or finalize with current results.", repetitionKey, s.repetitionPrevention[repetitionKey])
				}
				result := ToolCallResult{
					ID:     call.ID,
					Output: "",
					Error:  repetitionMsg,
				}
				record := ToolCallRecord{
					Request:     call,
					Result:      result,
					StartedAt:   time.Now().UTC(),
					CompletedAt: time.Now().UTC(),
				}
				s.notifyToolCall(&record, input.OnToolCall)
				s.out.ToolCalls = append(s.out.ToolCalls, record)
				s.toolResults = append(s.toolResults, result)
				s.registerToolOutcome(result.Error)
				continue
			}
		}

		callKey := call.Name + "|" + string(call.Arguments)
		if callKey != "|" {
			if cached, ok := s.cachedToolResults[callKey]; ok {
				now := time.Now().UTC()
				cached.ID = call.ID
				record := ToolCallRecord{Request: call, Result: cached, StartedAt: now, CompletedAt: now}
				errText := toolResultErrorText(record.Result)
				if strings.TrimSpace(record.Result.Error) == "" && errText != "" {
					record.Result.Error = errText
				}
				s.notifyToolCall(&record, input.OnToolCall)
				s.out.ToolCalls = append(s.out.ToolCalls, record)
				s.toolResults = append(s.toolResults, record.Result)
				s.registerToolOutcome(record.Result.Error)
				continue
			}
			if cached, ok := s.cachedFailedToolResults[callKey]; ok {
				now := time.Now().UTC()
				cached.ID = call.ID
				record := ToolCallRecord{Request: call, Result: cached, StartedAt: now, CompletedAt: now}
				errText := toolResultErrorText(record.Result)
				if strings.TrimSpace(record.Result.Error) == "" && errText != "" {
					record.Result.Error = errText
				}
				s.notifyToolCall(&record, input.OnToolCall)
				s.out.ToolCalls = append(s.out.ToolCalls, record)
				s.toolResults = append(s.toolResults, record.Result)
				s.registerToolOutcome(record.Result.Error)
				continue
			}
		}

		// Repetition prevention: detect repeated agent.create calls
		if call.Name == "agent.create" || call.Name == "agents.create" {
			agentID := extractAgentIDFromArgs(call.Arguments)
			if agentID != "" {
				key := call.Name + "|" + agentID
				s.repetitionPrevention[key]++
				if s.repetitionPrevention[key] >= 3 {
					// Return cached error to prevent infinite loops
					result := ToolCallResult{
						ID:     call.ID,
						Output: "",
						Error:  fmt.Sprintf("repetition detected: agent '%s' creation was already attempted %d times. If the agent exists, use it directly. If not, check previous errors.", agentID, s.repetitionPrevention[key]),
					}
					record := ToolCallRecord{
						Request:     call,
						Result:      result,
						StartedAt:   time.Now().UTC(),
						CompletedAt: time.Now().UTC(),
					}
					s.notifyToolCall(&record, input.OnToolCall)
					s.out.ToolCalls = append(s.out.ToolCalls, record)
					s.toolResults = append(s.toolResults, result)
					s.registerToolOutcome(result.Error)
					continue
				}
			}
		}

		record := ToolCallRecord{
			Request:   call,
			StartedAt: time.Now().UTC(),
		}

		execCtx, cancel := context.WithTimeout(ctx, s.toolTimeout)
		result, execErr := r.ToolExecutor.Execute(execCtx, call)
		cancel()
		if result.ID == "" {
			result.ID = call.ID
		}
		if execErr != nil {
			if isToolTimeoutError(execErr) && !strings.Contains(strings.ToLower(execErr.Error()), "timeout") {
				result.Error = fmt.Sprintf("timeout: tool execution exceeded %dms", int(s.toolTimeout/time.Millisecond))
			} else {
				result.Error = execErr.Error()
			}
		}
		if strings.TrimSpace(result.Error) == "" {
			if inferred := toolResultErrorText(result); inferred != "" {
				result.Error = inferred
			}
		}

		record.Result = result
		record.CompletedAt = time.Now().UTC()
		hadFreshExecution = true
		s.registerToolOutcome(result.Error)

		if callKey != "|" {
			if strings.TrimSpace(result.Error) == "" {
				s.cachedToolResults[callKey] = ToolCallResult{Output: result.Output}
				delete(s.cachedFailedToolResults, callKey)
				delete(s.failedToolCallCounts, callKey)
				delete(s.failedToolCallErrors, callKey)
			} else {
				errText := strings.TrimSpace(result.Error)
				if s.failedToolCallErrors[callKey] == errText {
					s.failedToolCallCounts[callKey]++
				} else {
					s.failedToolCallErrors[callKey] = errText
					s.failedToolCallCounts[callKey] = 1
				}
				if s.failedToolCallCounts[callKey] >= 2 {
					s.cachedFailedToolResults[callKey] = ToolCallResult{Output: result.Output, Error: result.Error}
				}
			}
		}

		s.notifyToolCall(&record, input.OnToolCall)
		s.out.ToolCalls = append(s.out.ToolCalls, record)
		s.toolResults = append(s.toolResults, result)
	}
	return hadFreshExecution
}

func (s *runState) runLoop(ctx context.Context, r Runner, input RunInput) (RunOutput, error) {
	for {
		systemPrompt := s.prepareSystemPrompt(ctx, input)

		var (
			resp ModelResponse
			err  error
		)
		for attempt := 0; ; attempt++ {
			resp, err = r.Model.Generate(ctx, ModelRequest{
				AgentID:       input.AgentID,
				RunID:         input.RunID,
				SystemPrompt:  systemPrompt,
				Messages:      append([]ChatMessage(nil), s.messages...),
				AllowedTools:  append([]string(nil), input.AllowedTools...),
				ToolTimeoutMS: input.ToolTimeoutMS,
				Prompt:        systemPrompt,
				Message:       input.Message,
				ToolResults:   append([]ToolCallResult(nil), s.toolResults...),
				OnTextDelta:   input.OnTextDelta,
			})
			if err == nil {
				break
			}
			if isProviderNoChoicesError(err) {
				if attempt >= noChoicesRetryCap {
					break
				}
				time.Sleep(noChoicesRetryDelay)
				continue
			}
			if isTransientProviderModelError(err) {
				if attempt >= transientModelRetryCap {
					break
				}
				time.Sleep(transientModelRetryDelay)
				continue
			}
			break
		}
		if resp.ThinkingPresent {
			s.thinkingPresent = true
			if strings.TrimSpace(resp.Thinking) != "" {
				s.latestThinking = strings.TrimSpace(resp.Thinking)
			}
		}
		if resp.ToolParseFailure {
			s.toolParseFailure = true
		}
		if err != nil {
			s.out.Thinking = s.latestThinking
			s.out.ThinkingPresent = s.thinkingPresent
			s.out.ToolParseFailure = s.toolParseFailure
			if len(s.toolResults) > 0 {
				s.out.FinalText = recoverFromModelError(err, s.toolResults, s.toolCap)
				s.out.CompletedAt = time.Now().UTC()
				return s.out, nil
			}
			s.out.CompletedAt = time.Now().UTC()
			return s.out, err
		}

		if len(resp.ToolCalls) == 0 {
			if shouldForceFollowThrough(resp.FinalText, input.AllowedTools, s.toolResults) {
				if s.followThroughReprompts < followThroughRepromptCap {
					s.followThroughReprompts++
					if text := strings.TrimSpace(resp.FinalText); text != "" {
						s.messages = append(s.messages, ChatMessage{Role: "assistant", Content: text})
					}
					continue
				}
				s.out.FinalText = nonActionableFinalText(resp.FinalText)
				s.out.Thinking = s.latestThinking
				s.out.ThinkingPresent = s.thinkingPresent
				s.out.ToolParseFailure = s.toolParseFailure
				s.out.CompletedAt = time.Now().UTC()
				return s.out, nil
			}
			s.out.FinalText = resp.FinalText
			s.out.Thinking = s.latestThinking
			s.out.ThinkingPresent = s.thinkingPresent
			s.out.ToolParseFailure = s.toolParseFailure
			s.out.CompletedAt = time.Now().UTC()
			return s.out, nil
		}

		if r.ToolExecutor == nil {
			s.out.Thinking = s.latestThinking
			s.out.ThinkingPresent = s.thinkingPresent
			s.out.ToolParseFailure = s.toolParseFailure
			s.out.CompletedAt = time.Now().UTC()
			return s.out, ErrToolExecutorRequired
		}

		if s.toolIterations >= s.toolCap {
			s.out.Thinking = s.latestThinking
			s.out.ThinkingPresent = s.thinkingPresent
			s.out.ToolParseFailure = s.toolParseFailure
			if len(s.toolResults) > 0 {
				if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, input.SystemPromptExt, ""); finalized != "" {
					s.out.FinalText = finalized
					s.out.CompletedAt = time.Now().UTC()
					return s.out, nil
				}
				s.out.FinalText = fallbackFromToolResults(s.toolResults, s.toolCap)
				s.out.CompletedAt = time.Now().UTC()
				return s.out, nil
			}
			s.out.CompletedAt = time.Now().UTC()
			return s.out, ErrToolIterationCapExceeded
		}

		hadFreshExecution := s.executeTools(ctx, r, resp.ToolCalls, input)

		if hadFreshExecution {
			s.noProgressIterations = 0
		} else {
			s.noProgressIterations++
		}

		if s.failureRecoveryActive && s.failuresSinceRecovery >= failureGuidanceEscalation && len(s.out.ToolCalls) > 0 {
			s.out.FinalText = requestUserGuidanceFromFailures(input.Message, s.out.ToolCalls)
			s.out.Thinking = s.latestThinking
			s.out.ThinkingPresent = s.thinkingPresent
			s.out.ToolParseFailure = s.toolParseFailure
			s.out.CompletedAt = time.Now().UTC()
			return s.out, nil
		}

		if s.noProgressIterations >= repeatedNoProgressLoopCapTrigger && len(s.toolResults) > 0 {
			if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, input.SystemPromptExt, ""); finalized != "" {
				s.out.FinalText = finalized
			} else {
				s.out.FinalText = fallbackFromToolResults(s.toolResults, s.toolCap)
			}
			s.out.Thinking = s.latestThinking
			s.out.ThinkingPresent = s.thinkingPresent
			s.out.ToolParseFailure = s.toolParseFailure
			s.out.CompletedAt = time.Now().UTC()
			return s.out, nil
		}

		s.toolIterations++
	}
}

func isProviderNoChoicesError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "no choices") || strings.Contains(text, "empty choices")
}

func isTransientProviderModelError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "stream interrupted") ||
		strings.Contains(text, "context deadline exceeded") ||
		strings.Contains(text, "client.timeout") ||
		strings.Contains(text, "timeout while reading body") ||
		strings.Contains(text, "i/o timeout")
}

// extractAgentIDFromArgs extracts the agent_id field from JSON tool arguments
func extractAgentIDFromArgs(args []byte) string {
	if len(args) == 0 {
		return ""
	}
	// Simple JSON extraction: look for "agent_id":"value" pattern
	argsStr := string(args)
	if idx := strings.Index(argsStr, `"agent_id"`); idx != -1 {
		rest := argsStr[idx+len(`"agent_id"`):]
		// Skip whitespace and colon
		rest = strings.TrimLeft(rest, " \t\n\r:")
		if strings.HasPrefix(rest, `"`) {
			rest = rest[1:]
			if endIdx := strings.Index(rest, `"`); endIdx != -1 {
				return rest[:endIdx]
			}
		}
	}
	return ""
}

func repeatedCallRepetitionKey(call ToolCallRequest) (string, int, bool) {
	name := strings.TrimSpace(call.Name)
	if name == "agent.message.send" {
		args, ok := parseToolArgs(call.Arguments)
		if !ok {
			return "", 0, false
		}
		toAgent := firstTrimmedStringFromMap(args, "to_agent_id", "agent_id", "target_agent", "targetAgent", "agentId")
		if toAgent == "" {
			return "", 0, false
		}
		taskID := firstTrimmedStringFromMap(args, "task_id", "taskId")
		if taskID != "" {
			return name + "|" + toAgent + "|" + taskID, agentMessageSendCap, true
		}
		message := firstTrimmedStringFromMap(args, "message", "content", "text", "body", "prompt")
		if message == "" {
			return "", 0, false
		}
		return name + "|" + toAgent + "|" + firstN(strings.ToLower(message), 80), agentMessageSendCap, true
	}
	if name == "agent.message.inbox" {
		args, ok := parseToolArgs(call.Arguments)
		if !ok {
			return "", 0, false
		}
		agentID := firstTrimmedStringFromMap(args, "agent_id", "id", "agent")
		if agentID == "" {
			agentID = "self"
		}
		return name + "|" + agentID, agentMessageInboxCap, true
	}
	if name == "agent.run" {
		args, ok := parseToolArgs(call.Arguments)
		if !ok {
			return "", 0, false
		}
		agentID := firstTrimmedStringFromMap(args, "agent_id", "to_agent_id", "target_agent", "targetAgent", "agentId")
		if agentID == "" {
			return "", 0, false
		}
		taskID := firstTrimmedStringFromMap(args, "task_id", "taskId")
		if taskID != "" {
			return name + "|" + agentID + "|" + taskID, agentRunCap, true
		}
		message := firstTrimmedStringFromMap(args, "message", "content", "text", "body", "prompt")
		if message == "" {
			return "", 0, false
		}
		return name + "|" + agentID + "|" + firstN(strings.ToLower(message), 80), agentRunCap, true
	}

	if name != "fs.write" && name != "fs.append" && name != "fs.read" {
		return "", 0, false
	}
	path := extractPathFromToolArgs(call.Arguments)
	if path == "" {
		return "", 0, false
	}
	cap, ok := repetitionCapForToolPath(name, path)
	if !ok {
		return "", 0, false
	}
	return name + "|" + path, cap, true
}

func repetitionCapForToolPath(toolName, path string) (int, bool) {
	lower := strings.ToLower(strings.TrimSpace(path))
	isJournalPath := strings.Contains(lower, "journal") || strings.Contains(lower, "diary")
	isBuildLogPath := strings.Contains(lower, "build_log")
	isSpecPath := strings.HasSuffix(lower, "_spec.md") || strings.HasSuffix(lower, "/spec.md")
	if toolName == "fs.write" || toolName == "fs.append" {
		if isBuildLogPath {
			return buildLogFileWriteCap, true
		}
		if isJournalPath {
			return journalFileWriteCap, true
		}
		return defaultRepeatedFileWriteCap, true
	}
	if toolName == "fs.read" && isBuildLogPath {
		return buildLogFileReadCap, true
	}
	if toolName == "fs.read" && isSpecPath {
		return specFileReadCap, true
	}
	if toolName == "fs.read" && isJournalPath {
		return journalFileReadCap, true
	}
	return 0, false
}

func parseToolArgs(args []byte) (map[string]any, bool) {
	if len(args) == 0 {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(args, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func firstTrimmedStringFromMap(values map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok || raw == nil {
			continue
		}
		text, ok := raw.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstN(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

func extractPathFromToolArgs(args []byte) string {
	payload, ok := parseToolArgs(args)
	if !ok {
		return ""
	}
	return firstTrimmedStringFromMap(payload, "path", "file", "target", "filename")
}
