package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	defaultRepeatedFileWriteCap  = 12
	defaultRepeatedFileAppendCap = 16
	journalFileWriteCap          = 4
	journalFileAppendCap         = 24
	journalFileReadCap           = 4
	buildLogFileWriteCap         = 6
	buildLogFileAppendCap        = 16
	buildLogFileReadCap          = 6
	specFileReadCap              = 6
	agentMessageSendCap          = 1
	agentMessageInboxCap         = 2
	agentRunCap                  = 1
	agentIdentitySetCap          = 2
	agentCreateGlobalCap         = 8
	shellExecRepetitionCap       = 3
	memoryWriteRepetitionCap     = 2
	stateMutationRepetitionCap   = 1
	becomussyWriteRepetitionCap  = 2
	becomussyReadRepetitionCap   = 6
	stateReaderRepetitionCap     = 3 // control-plane state readers (scheduler.list, policy.list, etc.)
	defaultRepeatedFileReadCap   = 6 // fs.read on non-special paths (per path)
	defaultRepeatedFileListCap   = 4 // fs.list (per directory path)
	noChoicesRetryCap            = 3
	noChoicesRetryDelay          = 300 * time.Millisecond
	transientModelRetryCap       = 3
	transientModelRetryBaseDelay = 500 * time.Millisecond
	toolParseRetryCap            = 4
	maxToolRewriteBudget         = 3 // Maximum rewrites to prevent infinite loops
	defaultContextWindow         = 120000
)

// delegationAllowedTools is the fixed set of tools permitted when delegation
// mode is ToolGated or AutoExecute. Defined at package level to avoid
// allocating a new map on every isToolAllowedInDelegationMode call.
var delegationAllowedTools = map[string]bool{
	"agent.list": true,
	"agent.run":  true,
}

// runState encapsulates the mutable state of a single agent run loop.
type runState struct {
	out                        RunOutput
	runID                      string
	agentID                    string
	parentRunID                string
	decisionLedger             *DecisionLedger
	messages                   []ChatMessage
	toolResults                []ToolCallResult
	toolIterations             int
	toolCallOrdinal            int
	usedToolCallIDs            map[string]struct{}
	cachedToolResults          map[string]ToolCallResult
	cachedFailedToolResults    map[string]ToolCallResult
	failedToolCallCounts       map[string]int
	failedToolCallErrors       map[string]string
	consecutiveToolFailures    int
	failureRecoveryActive      bool
	failuresSinceRecovery      int
	successesSinceRecovery     int
	toolTimeout                time.Duration
	noProgressIterations       int
	allBlockedIterations       int
	latestThinking             string
	thinkingPresent            bool
	toolParseFailure           bool
	toolParseReprompts         int
	followThroughReprompts     int
	lastIterationMixed         bool
	lastIterationSucceeded     []string
	lastIterationFailed        []string
	toolCap                    int
	successfulOneShotMutations map[string]struct{}
	// repetitionPrevention tracks tool calls to detect and prevent loops
	repetitionPrevention map[string]int // key: "tool_name|agent_id" -> count

	// structuralBlockerCounts tracks infrastructure-level error categories
	// that cannot be fixed by the agent retrying (e.g. missing parent
	// directory, capability denied).  When any category reaches the cap
	// the run is hard-stopped with an escalation message to the owner.
	structuralBlockerCounts map[string]int

	// lastUncacheableOutputs tracks the most recent output for uncacheable
	// tool calls (keyed by tool name + normalized args).  When a fresh
	// execution produces the exact same output as the last invocation with
	// the same key, it is not counted as genuine progress — this prevents
	// the no-progress detector from being defeated by tools that always
	// return the same result but are intentionally excluded from caching.
	lastUncacheableOutputs map[string]string

	// Context tracking from model responses
	lastPromptTokens int
	contextWindow    int

	// Delegation state
	delegationMode      DelegationMode
	delegationCooldown  int
	delegationActive    bool
	pendingSubtasks     []DecomposedTask
	completedSubtasks   map[string]string
	delegationArtifacts map[string]string
	lastModelOutput     string
	recentIntents       []string
	toolRewriteCount    int  // tracks rewrites to prevent infinite loops
	delegationLocked    bool // once true, stays true until subtasks complete
	delegationReason    string
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

	// Prefer the context window size configured on the Runner; fall back to
	// the compile-time default so delegation triggers fire at the right threshold.
	ctxWindow := r.ContextWindowSize
	if ctxWindow <= 0 {
		ctxWindow = defaultContextWindow
	}

	return &runState{
		out:                        out,
		runID:                      strings.TrimSpace(input.RunID),
		agentID:                    strings.TrimSpace(input.AgentID),
		parentRunID:                strings.TrimSpace(input.ParentRunID),
		decisionLedger:             input.DecisionLedger,
		messages:                   messages,
		toolResults:                make([]ToolCallResult, 0),
		usedToolCallIDs:            make(map[string]struct{}),
		cachedToolResults:          make(map[string]ToolCallResult),
		cachedFailedToolResults:    make(map[string]ToolCallResult),
		failedToolCallCounts:       make(map[string]int),
		failedToolCallErrors:       make(map[string]string),
		toolTimeout:                toolTimeout,
		toolCap:                    toolCap,
		successfulOneShotMutations: make(map[string]struct{}),
		repetitionPrevention:       make(map[string]int),
		structuralBlockerCounts:    make(map[string]int),
		lastUncacheableOutputs:     make(map[string]string),
		lastPromptTokens:           0,
		contextWindow:              ctxWindow,
		delegationMode:             "",
		delegationCooldown:         0,
		delegationActive:           false,
		pendingSubtasks:            nil,
		completedSubtasks:          make(map[string]string),
		delegationArtifacts:        make(map[string]string),
		lastModelOutput:            "",
		recentIntents:              make([]string, 0),
		toolRewriteCount:           0,
		delegationLocked:           false,
		delegationReason:           "",
	}
}

// finalizeOutput stamps the terminal fields on s.out and returns it together
// with err. Every early-return path in runLoop should call this instead of
// duplicating the three-line assignment block.
func (s *runState) finalizeOutput(err error) (RunOutput, error) {
	s.out.Thinking = s.latestThinking
	s.out.ThinkingPresent = s.thinkingPresent
	s.out.ToolParseFailure = s.toolParseFailure
	s.out.CompletedAt = time.Now().UTC()
	return s.out, err
}

func (s *runState) registerToolOutcome(errText string) {
	trimmed := strings.TrimSpace(errText)
	if strings.Contains(strings.ToLower(trimmed), "repetition detected") {
		return
	}
	if trimmed == "" {
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
	// Track structural blocker categories separately.
	if cat := structuralBlockerCategory(trimmed); cat != "" {
		s.structuralBlockerCounts[cat]++
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

// structuralBlockerCategory returns a non-empty category string when the
// error indicates an infrastructure-level problem that the agent cannot
// fix by retrying with different arguments.  These are problems that
// require owner intervention (missing directories, permission denials,
// workspace boundary issues).
func structuralBlockerCategory(errText string) string {
	lower := strings.ToLower(errText)
	switch {
	case strings.Contains(lower, "write parent does not exist"):
		return "missing_parent_directory"
	case strings.Contains(lower, "no existing ancestor"):
		return "missing_parent_directory"
	case strings.Contains(lower, "capability denied"):
		return "capability_denied"
	case strings.Contains(lower, "outside workspace"):
		return "outside_workspace"
	case strings.Contains(lower, "protected control-plane path"):
		return "protected_path"
	default:
		return ""
	}
}

// structuralBlockerTriggered returns the category and count if any
// structural blocker has reached the hard-stop cap.
func (s *runState) structuralBlockerTriggered() (string, int) {
	for cat, count := range s.structuralBlockerCounts {
		if count >= structuralBlockerCap {
			return cat, count
		}
	}
	return "", 0
}

func (s *runState) notifyToolCall(record *ToolCallRecord, onToolCall func(ToolCallRecord) error) {
	if onToolCall == nil || record == nil {
		return
	}
	if err := onToolCall(*record); err != nil {
		record.CallbackErr = strings.TrimSpace(err.Error())
	}
}

func (s *runState) emitDecision(ctx context.Context, recordType string, payload map[string]any, summary string) {
	if s == nil || s.decisionLedger == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if strings.TrimSpace(s.parentRunID) != "" {
		if _, ok := payload["parent_run_id"]; !ok {
			payload["parent_run_id"] = s.parentRunID
		}
	}
	_ = s.decisionLedger.Record(ctx, DecisionRecord{
		Timestamp:    time.Now().UTC(),
		RunID:        strings.TrimSpace(s.runID),
		AgentID:      strings.TrimSpace(s.agentID),
		RecordType:   strings.TrimSpace(recordType),
		Payload:      payload,
		HumanSummary: firstN(strings.TrimSpace(summary), 280),
	})
}

func (s *runState) delegationParentRunID() string {
	if s == nil {
		return ""
	}
	if parent := strings.TrimSpace(s.parentRunID); parent != "" {
		return parent
	}
	return strings.TrimSpace(s.runID)
}

func (s *runState) normalizeDelegationTriggerPayload(payload map[string]any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}

	normalized := make(map[string]any, len(payload)+8)
	for k, v := range payload {
		normalized[k] = v
	}

	triggerReason := strings.TrimSpace(stringValueFromAny(normalized["trigger_reason"]))
	if triggerReason == "" {
		triggerReason = strings.TrimSpace(stringValueFromAny(normalized["reason"]))
	}

	selectedRole := strings.TrimSpace(stringValueFromAny(normalized["selected_role"]))
	if selectedRole == "" {
		selectedRole = "delegation_controller"
	}

	taskAssignment := strings.TrimSpace(stringValueFromAny(normalized["task_assignment"]))
	if taskAssignment == "" {
		taskAssignment = "delegation trigger evaluation"
	}

	confidence, ok := floatValueFromAny(normalized["confidence"])
	if !ok {
		confidence = 0
	}

	parentRunID := strings.TrimSpace(stringValueFromAny(normalized["parent_run_id"]))
	if parentRunID == "" {
		parentRunID = s.delegationParentRunID()
	}

	eventTimestamp := strings.TrimSpace(stringValueFromAny(normalized["event_timestamp"]))
	if eventTimestamp == "" {
		eventTimestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	sourceRunID := strings.TrimSpace(stringValueFromAny(normalized["source_run_id"]))
	if sourceRunID == "" {
		sourceRunID = strings.TrimSpace(s.runID)
	}

	sourceAgentID := strings.TrimSpace(stringValueFromAny(normalized["source_agent_id"]))
	if sourceAgentID == "" {
		sourceAgentID = strings.TrimSpace(s.agentID)
	}

	fromAgentID := strings.TrimSpace(stringValueFromAny(normalized["from_agent_id"]))
	if fromAgentID == "" {
		fromAgentID = sourceAgentID
	}

	toAgentID := strings.TrimSpace(stringValueFromAny(normalized["to_agent_id"]))

	normalized["trigger_reason"] = triggerReason
	normalized["reason"] = triggerReason
	normalized["selected_role"] = selectedRole
	normalized["task_assignment"] = taskAssignment
	normalized["confidence"] = confidence
	normalized["parent_run_id"] = parentRunID
	normalized["event_timestamp"] = eventTimestamp
	normalized["source_run_id"] = sourceRunID
	normalized["source_agent_id"] = sourceAgentID
	normalized["from_agent_id"] = fromAgentID
	normalized["to_agent_id"] = toAgentID

	return normalized
}

func (s *runState) emitDelegationTriggerDecision(ctx context.Context, payload map[string]any, summary string) {
	s.emitDecision(ctx, DecisionRecordTypeDelegationTrigger, s.normalizeDelegationTriggerPayload(payload), summary)
}

func stringValueFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func floatValueFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func (s *runState) emitToolDecision(ctx context.Context, call ToolCallRequest, result ToolCallResult) {
	decision := "allowed"
	policyReference := "policy.capability_allowlist"
	reason := ""
	errText := strings.TrimSpace(result.Error)
	if errText != "" {
		lowerErr := strings.ToLower(errText)
		if strings.Contains(lowerErr, "policy.denied") || strings.Contains(lowerErr, "capability denied") {
			decision = "denied"
			reason = errText
		}
	}

	payload := map[string]any{
		"tool":             strings.TrimSpace(call.Name),
		"tool_call_id":     strings.TrimSpace(call.ID),
		"decision":         decision,
		"policy_reference": policyReference,
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if errText != "" && decision == "allowed" {
		payload["execution_error"] = errText
	}

	summary := fmt.Sprintf("Tool decision: %s was %s under %s.", strings.TrimSpace(call.Name), decision, policyReference)
	if reason != "" {
		summary = fmt.Sprintf("Tool decision: %s was denied (%s).", strings.TrimSpace(call.Name), firstN(reason, 120))
	}
	s.emitDecision(ctx, DecisionRecordTypeToolDecision, payload, summary)
}

func (s *runState) emitTerminationDecision(ctx context.Context, out RunOutput, runErr error) {
	cause := runTerminationCause(runErr)
	payload := map[string]any{"cause": cause}
	if runErr != nil {
		payload["error"] = strings.TrimSpace(runErr.Error())
	}
	if strings.TrimSpace(out.FinalText) != "" {
		payload["final_text_preview"] = firstN(strings.TrimSpace(out.FinalText), 160)
	}
	summary := "Run terminated."
	if cause == "completed" {
		summary = "Run completed successfully."
	} else {
		summary = fmt.Sprintf("Run terminated with cause: %s.", cause)
	}
	s.emitDecision(ctx, DecisionRecordTypeTermination, payload, summary)
}

func runTerminationCause(runErr error) string {
	if runErr == nil {
		return "completed"
	}
	if errors.Is(runErr, context.Canceled) {
		return "canceled"
	}
	if errors.Is(runErr, ErrToolIterationCapExceeded) {
		return "tool_iteration_cap_exceeded"
	}
	if errors.Is(runErr, ErrToolExecutorRequired) {
		return "tool_executor_missing"
	}
	if errors.Is(runErr, ErrModelRequired) {
		return "model_missing"
	}
	return "error"
}

func (s *runState) prepareSystemPrompt(ctx context.Context, input RunInput) string {
	systemPrompt := s.out.Prompt
	if s.lastIterationMixed {
		directive := "# PARTIAL_SUCCESS_MODE\n- The previous tool batch had mixed outcomes.\n- Do not repeat tools that already succeeded in that batch.\n- Retry only the failed tools with corrected arguments, or finalize if the core request is already satisfied."
		if len(s.lastIterationSucceeded) > 0 {
			directive += "\n- Successful tools in previous batch: " + strings.Join(s.lastIterationSucceeded, ", ") + "."
		}
		if len(s.lastIterationFailed) > 0 {
			directive += "\n- Failed tools in previous batch: " + strings.Join(s.lastIterationFailed, ", ") + "."
		}
		systemPrompt = appendPromptDirective(systemPrompt, directive)
	}
	if s.failureRecoveryActive {
		systemPrompt = appendPromptDirective(systemPrompt, "# ERROR_RECOVERY_MODE\n- Recent tool calls failed. Analyze the latest errors and outputs before choosing the next action.\n- Try a materially different approach to resolve the error.\n- Do not repeat the same failing command/arguments unless you explain why it should now work.")
	}
	if s.noProgressIterations > 0 {
		systemPrompt = appendPromptDirective(systemPrompt, "# NO_PROGRESS_MODE\n- The previous tool call(s) produced no new progress.\n- Do not repeat the same tool with unchanged arguments.\n- Either use a materially different tool/action, or provide a final response from current evidence.")
	}
	if s.followThroughReprompts > 0 {
		systemPrompt = appendPromptDirective(systemPrompt, "# ACTION_EXECUTION_MODE\n- You previously replied with intent to act but did not execute.\n- In this turn, either call required tools now or provide a concrete final answer from existing evidence.\n- Do not defer with phrases like 'let me check' or promise future action without execution.")
	}
	if s.toolParseReprompts > 0 {
		systemPrompt = appendPromptDirective(systemPrompt, "# TOOL_PARSE_RECOVERY_MODE\n- Your previous response attempted tool calls in an invalid format and no tool executed.\n- Output tool calls only as fenced JSON objects with this exact shape:\n```json\n{\"tool_name\":\"<tool.name>\",\"arguments\":{...}}\n```\n- Do not use pseudo-XML tags such as <tool_call> or <arg_value>.")
	}
	if input.SystemPromptExt != nil {
		extended := input.SystemPromptExt(ctx, systemPrompt, append([]ChatMessage(nil), s.messages...), input.Message, append([]ToolCallResult(nil), s.toolResults...), input.Source)
		if strings.TrimSpace(extended) != "" {
			systemPrompt = extended
		}
	}
	return systemPrompt
}

type toolExecutionOutcome struct {
	hadFreshExecution                bool
	blockedSuccessfulOneShotMutation bool
}

func (s *runState) executeTools(ctx context.Context, r Runner, toolCalls []ToolCallRequest, input RunInput) toolExecutionOutcome {
	outcome := toolExecutionOutcome{}
	for _, incoming := range toolCalls {
		s.toolCallOrdinal++
		call := incoming
		call.ID = uniqueToolCallID(call.ID, s.toolCallOrdinal, s.usedToolCallIDs)
		oneShotKey, hasOneShotKey := oneShotMutationRepetitionKey(call.Name, call.Arguments)
		skipGenericRepetitionGuard := false
		if hasOneShotKey {
			if _, alreadySucceeded := s.successfulOneShotMutations[oneShotKey]; alreadySucceeded {
				repetitionMsg := fmt.Sprintf("repetition detected: %s has already succeeded in this run. Do not rerun it; provide a final response from current evidence.", oneShotKey)
				result := ToolCallResult{ID: call.ID, Output: "", Error: repetitionMsg}
				record := ToolCallRecord{Request: call, Result: result, StartedAt: time.Now().UTC(), CompletedAt: time.Now().UTC()}
				s.notifyToolCall(&record, input.OnToolCall)
				s.out.ToolCalls = append(s.out.ToolCalls, record)
				s.toolResults = append(s.toolResults, result)
				s.registerToolOutcome(result.Error)
				s.emitToolDecision(ctx, call, result)
				outcome.blockedSuccessfulOneShotMutation = true
				continue
			}
			skipGenericRepetitionGuard = call.Name != "agent.identity.set"
		}

		if !skipGenericRepetitionGuard {
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
					s.emitToolDecision(ctx, call, result)
					if hasOneShotKey {
						if _, alreadySucceeded := s.successfulOneShotMutations[oneShotKey]; alreadySucceeded {
							outcome.blockedSuccessfulOneShotMutation = true
						}
					}
					continue
				}
			}
		}

		callKey := toolCallCacheKey(call)
		if callKey != "|" {
			if cached, ok := s.cachedToolResults[callKey]; ok {
				now := time.Now().UTC()
				cached.ID = call.ID
				record := ToolCallRecord{Request: call, Result: cached, StartedAt: now, CompletedAt: now}
				s.notifyToolCall(&record, input.OnToolCall)
				s.out.ToolCalls = append(s.out.ToolCalls, record)
				s.toolResults = append(s.toolResults, record.Result)
				s.registerToolOutcome(record.Result.Error)
				s.emitToolDecision(ctx, call, record.Result)
				continue
			}
			if cached, ok := s.cachedFailedToolResults[callKey]; ok {
				now := time.Now().UTC()
				cached.ID = call.ID
				record := ToolCallRecord{Request: call, Result: cached, StartedAt: now, CompletedAt: now}
				s.notifyToolCall(&record, input.OnToolCall)
				s.out.ToolCalls = append(s.out.ToolCalls, record)
				s.toolResults = append(s.toolResults, record.Result)
				s.registerToolOutcome(record.Result.Error)
				s.emitToolDecision(ctx, call, record.Result)
				continue
			}
		}

		// NOTE: agent.create repetition is fully handled by the generic
		// repeatedCallRepetitionKey / oneShotMutationRepetitionKey guards
		// above via the agentCreateGlobalCap constant. The duplicate inline
		// block that previously appeared here was unreachable and has been
		// removed to avoid confusion.

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
		record.Result = result
		record.CompletedAt = time.Now().UTC()
		s.registerToolOutcome(result.Error)

		if callKey != "|" {
			// Cacheable tool: mark as fresh progress and update cache.
			outcome.hadFreshExecution = true
			if strings.TrimSpace(result.Error) == "" {
				if hasOneShotKey {
					s.successfulOneShotMutations[oneShotKey] = struct{}{}
				}
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
		} else {
			// Uncacheable tool: track whether the output actually changed.
			stableKey := call.Name + "|" + string(call.Arguments)
			prevOutput, seen := s.lastUncacheableOutputs[stableKey]
			currentOutput := strings.TrimSpace(result.Output) + "\x00" + strings.TrimSpace(result.Error)
			s.lastUncacheableOutputs[stableKey] = currentOutput
			if !seen || prevOutput != currentOutput {
				outcome.hadFreshExecution = true
				if strings.TrimSpace(result.Error) == "" && hasOneShotKey {
					s.successfulOneShotMutations[oneShotKey] = struct{}{}
				}
			}
		}

		s.notifyToolCall(&record, input.OnToolCall)
		s.out.ToolCalls = append(s.out.ToolCalls, record)
		s.toolResults = append(s.toolResults, result)
		s.emitToolDecision(ctx, call, result)
	}
	return outcome
}

func (s *runState) runLoop(ctx context.Context, r Runner, input RunInput) (finalOut RunOutput, finalErr error) {
	defer func() {
		s.emitTerminationDecision(ctx, finalOut, finalErr)
	}()

	goalText := strings.TrimSpace(input.Message)
	if goalText == "" && len(input.Messages) > 0 {
		goalText = strings.TrimSpace(input.Messages[len(input.Messages)-1].Content)
	}
	if goalText == "" {
		goalText = "Run without explicit user message"
	}
	s.emitDecision(ctx, DecisionRecordTypeGoalInterpretation, map[string]any{
		"goal":          goalText,
		"message_count": len(input.Messages),
	}, fmt.Sprintf("Interpreted run goal: %s", firstN(goalText, 160)))

	strategy := "direct_execution"
	if mode := normalizeDelegationMode(input.DelegationMode); mode != "" {
		strategy = fmt.Sprintf("delegation_mode:%s", mode)
	}
	s.emitDecision(ctx, DecisionRecordTypeStrategySelection, map[string]any{
		"strategy":        strategy,
		"delegation_mode": strings.TrimSpace(input.DelegationMode),
		"auto_delegate":   input.AutoDelegate,
	}, fmt.Sprintf("Selected strategy %s for this run.", strategy))

	s.emitDecision(ctx, DecisionRecordTypeConstraintActive, map[string]any{
		"allowed_tools":       append([]string(nil), input.AllowedTools...),
		"max_tool_iterations": s.toolCap,
		"tool_timeout_ms":     int(s.toolTimeout / time.Millisecond),
	}, "Activated run constraints (tool allowlist, iteration cap, timeout).")

	for {
		systemPrompt := s.prepareSystemPrompt(ctx, input)

		// === DELEGATION TRIGGER CHECK ===
		snapshot := StateSnapshot{
			LastToolAttempted: s.getLastToolName(),
			LastErrorTypes:    s.getRecentErrorTypes(),
			LastModelOutput:   s.getLastModelOutput(),
			AskedUserQuestion: DetectUserQuestion(s.getLastModelOutput()),
		}

		promptTokens := s.lastPromptTokens
		contextWindow := s.contextWindow

		if !s.delegationLocked {
			if trigger := s.computeDelegationTrigger(promptTokens, contextWindow, snapshot); trigger != nil {
				s.delegationReason = strings.TrimSpace(trigger.Reason)
				configuredMode := normalizeDelegationMode(input.DelegationMode)
				selectedRole := "delegation_controller"
				taskAssignment := "evaluate delegation trigger conditions"
				if isPlannerDelegationMode(configuredMode) {
					selectedRole = "planner"
					taskAssignment = "generate delegation decomposition plan"
				}

				s.emitDelegationTriggerDecision(ctx, map[string]any{
					"trigger_reason":  strings.TrimSpace(trigger.Reason),
					"selected_role":   selectedRole,
					"task_assignment": taskAssignment,
					"confidence":      0.0,
					"mode":            string(trigger.Mode),
					"cooldown_for":    trigger.CooldownFor,
					"context_tokens":  promptTokens,
					"context_window":  contextWindow,
					"subtask_count":   len(trigger.Subtasks),
					"snapshot_tool":   strings.TrimSpace(snapshot.LastToolAttempted),
					"snapshot_errors": append([]string(nil), snapshot.LastErrorTypes...),
				}, fmt.Sprintf("Delegation trigger fired (%s): %s", strings.TrimSpace(string(trigger.Mode)), firstN(strings.TrimSpace(trigger.Reason), 140)))

				if isPlannerDelegationMode(configuredMode) && trigger.Mode == DelegationModePromptOnly {
					if plan, _, planErr := GenerateDecompositionPlan(trigger.Reason, configuredMode, trigger.Subtasks, nil); planErr == nil {
						if s.out.DecompositionPlan == nil {
							s.out.DecompositionPlan = &plan
						}
						for _, task := range plan.Tasks {
							s.emitDecision(ctx, DecisionRecordTypeRoleSelection, map[string]any{
								"task_id":        strings.TrimSpace(task.TaskID),
								"role":           strings.TrimSpace(task.AssignedRole),
								"confidence":     task.Confidence,
								"task":           strings.TrimSpace(task.Description),
								"role_rationale": strings.TrimSpace(task.Rationale),
							}, fmt.Sprintf("Selected role %s for task %s.", strings.TrimSpace(task.AssignedRole), strings.TrimSpace(task.TaskID)))
						}
					}
				}

				if isPlannerDelegationMode(configuredMode) && trigger.Mode != DelegationModePromptOnly {
					plan, plannedTasks, planErr := GenerateDecompositionPlan(trigger.Reason, configuredMode, trigger.Subtasks, nil)
					if planErr != nil {
						return s.finalizeOutput(planErr)
					}

					s.out.DecompositionPlan = &plan
					s.pendingSubtasks = plannedTasks
					s.delegationActive = true
					s.appendPlannedDelegationEvents(ctx, plan)
					s.emitDecision(ctx, DecisionRecordTypeConstraintActive, map[string]any{
						"delegation_mode": string(configuredMode),
						"subtask_count":   len(plannedTasks),
						"auto_delegate":   input.AutoDelegate,
					}, fmt.Sprintf("Activated planner-led delegation constraints for %d subtasks.", len(plannedTasks)))
					for _, task := range plan.Tasks {
						s.emitDecision(ctx, DecisionRecordTypeRoleSelection, map[string]any{
							"task_id":        strings.TrimSpace(task.TaskID),
							"role":           strings.TrimSpace(task.AssignedRole),
							"confidence":     task.Confidence,
							"task":           strings.TrimSpace(task.Description),
							"role_rationale": strings.TrimSpace(task.Rationale),
						}, fmt.Sprintf("Selected role %s for task %s.", strings.TrimSpace(task.AssignedRole), strings.TrimSpace(task.TaskID)))
					}

					autoExecute, decisionSummary := shouldAutoExecutePlan(configuredMode, plan, input.DelegationApproved)
					if autoExecute && r.SubAgentRunner == nil {
						autoExecute = false
						decisionSummary = "delegation plan generated, but subagent runner is unavailable"
					}

					if !autoExecute {
						s.out.FinalText = formatDecompositionPlanForOperator(plan, decisionSummary)
						return s.finalizeOutput(nil)
					}

					if err := r.executeDelegatedTasks(ctx, s, plannedTasks, input); err != nil {
						return s.finalizeOutput(err)
					}
					return s.finalizeOutput(nil)
				}

				// Downgrade execution-dependent modes when no SubAgentRunner is available.
				if r.SubAgentRunner == nil && (trigger.Mode == DelegationModeToolGated || trigger.Mode == DelegationModeAutoExecute) {
					trigger.Mode = DelegationModePromptOnly
					trigger.AllowedTools = nil
				}

				s.delegationMode = trigger.Mode
				s.delegationCooldown = trigger.CooldownFor
				s.pendingSubtasks = trigger.Subtasks
				s.delegationActive = true
				s.emitDecision(ctx, DecisionRecordTypeConstraintActive, map[string]any{
					"delegation_mode": string(trigger.Mode),
					"allowed_tools":   append([]string(nil), trigger.AllowedTools...),
					"cooldown_for":    trigger.CooldownFor,
				}, fmt.Sprintf("Activated delegation mode constraints (%s).", strings.TrimSpace(string(trigger.Mode))))

				if trigger.Mode == DelegationModeToolGated || trigger.Mode == DelegationModeAutoExecute {
					s.delegationLocked = true
				}

				if trigger.Mode == DelegationModeAutoExecute && input.AutoDelegate {
					if err := r.executeDelegatedTasks(ctx, s, trigger.Subtasks, input); err != nil {
						return s.finalizeOutput(err)
					}
					return s.finalizeOutput(nil)
				}

				if trigger.Mode == DelegationModeToolGated {
					systemPrompt = appendPromptDirective(systemPrompt, buildForcedDelegationDirective(trigger))
				}
				if trigger.Mode == DelegationModePromptOnly {
					systemPrompt = appendPromptDirective(systemPrompt, buildSoftDelegationHint(trigger))
				}
			}
		}

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
				ToolSchemas:   append([]ToolSchema(nil), input.ToolSchemas...),
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
				if len(s.toolResults) > 0 {
					break
				}
				if attempt >= noChoicesRetryCap {
					break
				}
				time.Sleep(noChoicesRetryDelay)
				continue
			}
			if isTransientProviderModelError(err) {
				if len(s.toolResults) > 0 {
					break
				}
				if attempt >= transientModelRetryCap {
					break
				}
				backoff := transientModelRetryBaseDelay * time.Duration(1<<uint(attempt))
				if backoff > 4*time.Second {
					backoff = 4 * time.Second
				}
				time.Sleep(backoff)
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
		if resp.PromptTokens > 0 {
			s.lastPromptTokens = resp.PromptTokens
		}
		if err != nil {
			if isContextCanceledModelError(ctx, err) {
				return s.finalizeOutput(context.Canceled)
			}
			if len(s.toolResults) > 0 {
				if isTransientProviderModelError(err) {
					if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, input.SystemPromptExt, input.Source, "# TRANSIENT_MODEL_ERROR_RECOVERY\n- Your previous model turn ended with a transient provider interruption after tool work.\n- Do not call tools in this recovery turn.\n- Use the latest tool results to answer the user directly.\n- If the tool results are incomplete, say what remains and mention that the stream was interrupted."); finalized != "" {
						s.out.FinalText = strings.TrimSpace(finalized)
						return s.finalizeOutput(nil)
					}
				}
				s.out.FinalText = recoverFromModelError(err, s.toolResults, s.toolCap)
				return s.finalizeOutput(nil)
			}
			if input.OnTextDelta != nil && (isTransientProviderModelError(err) || isProviderNoChoicesError(err)) {
				s.out.FinalText = recoverFromInterruptedStream(err)
				return s.finalizeOutput(nil)
			}
			return s.finalizeOutput(err)
		}

		if len(resp.ToolCalls) == 0 {
			if resp.ToolParseFailure && shouldRetryToolParseFailure(resp.FinalText, input.AllowedTools) {
				if s.toolParseReprompts < toolParseRetryCap {
					s.toolParseReprompts++
					if text := strings.TrimSpace(resp.FinalText); text != "" {
						s.messages = append(s.messages, ChatMessage{Role: "assistant", Content: text})
					}
					continue
				}
			}
			s.toolParseReprompts = 0
			if shouldForceFollowThrough(resp.FinalText, input.AllowedTools, s.toolResults) {
				if s.followThroughReprompts < followThroughRepromptCap {
					s.followThroughReprompts++
					if text := strings.TrimSpace(resp.FinalText); text != "" {
						s.messages = append(s.messages, ChatMessage{Role: "assistant", Content: text})
					}
					continue
				}
				s.out.FinalText = nonActionableFinalText(resp.FinalText)
				return s.finalizeOutput(nil)
			}
			finalText := strings.TrimSpace(resp.FinalText)
			if finalText == "" {
				if len(s.toolResults) > 0 {
					if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, input.SystemPromptExt, input.Source, "# EMPTY_FINAL_TEXT_RECOVERY\n- Your previous final response was empty.\n- Provide a concise user-facing final answer from the latest tool results."); finalized != "" {
						finalText = strings.TrimSpace(finalized)
					}
				}
				if finalText == "" {
					finalText = recoverFromEmptyFinal(s.toolResults)
				}
			}
			s.out.FinalText = finalText
			return s.finalizeOutput(nil)
		}

		if r.ToolExecutor == nil {
			return s.finalizeOutput(ErrToolExecutorRequired)
		}

		if s.toolIterations >= s.toolCap {
			if len(s.toolResults) > 0 {
				if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, input.SystemPromptExt, input.Source, ""); finalized != "" {
					s.out.FinalText = finalized
					return s.finalizeOutput(nil)
				}
				s.out.FinalText = fallbackFromToolResults(s.toolResults, s.toolCap)
				return s.finalizeOutput(nil)
			}
			return s.finalizeOutput(ErrToolIterationCapExceeded)
		}

		// === TOOL GATING IN FORCED MODE ===
		if (s.delegationMode == DelegationModeToolGated || s.delegationMode == DelegationModeAutoExecute) && len(resp.ToolCalls) > 0 {
			filteredCalls := make([]ToolCallRequest, 0, len(resp.ToolCalls))
		gatingLoop:
			for _, call := range resp.ToolCalls {
				if s.isToolAllowedInDelegationMode(call.Name) {
					filteredCalls = append(filteredCalls, call)
					continue
				}
				s.emitDecision(ctx, DecisionRecordTypeToolDecision, map[string]any{
					"tool":             strings.TrimSpace(call.Name),
					"tool_call_id":     strings.TrimSpace(call.ID),
					"decision":         "denied",
					"policy_reference": "policy.delegation_mode",
					"reason":           fmt.Sprintf("tool %s is blocked while delegation mode is %s", strings.TrimSpace(call.Name), strings.TrimSpace(string(s.delegationMode))),
				}, fmt.Sprintf("Denied tool %s due to delegation mode constraints.", strings.TrimSpace(call.Name)))

				if s.toolRewriteCount >= maxToolRewriteBudget {
					if len(s.pendingSubtasks) > 0 {
						slog.Debug("delegation: executing pending subtasks (rewrite budget exceeded)", "pending_count", len(s.pendingSubtasks))
						if err := r.executeDelegatedTasks(ctx, s, s.pendingSubtasks, input); err != nil {
							s.out.FinalText = fmt.Sprintf("Delegation failed: %v", err)
							return s.finalizeOutput(nil)
						}
						s.pendingSubtasks = nil
						s.delegationLocked = false
						s.delegationMode = ""
						slog.Debug("delegation: subtasks completed, unlocking delegation mode")
						break gatingLoop
					}
					s.out.FinalText = fmt.Sprintf("DELEGATION_REWRITE_BUDGET_EXCEEDED: Task requires delegation but exceeded maximum rewrite attempts (%d). The subagent keeps trying to use forbidden tools. Please break this task into smaller, independent subtasks manually.", maxToolRewriteBudget)
					return s.finalizeOutput(nil)
				}

				if len(s.pendingSubtasks) > 0 {
					slog.Debug("delegation: executing pending subtasks (forbidden tool attempted)", "pending_count", len(s.pendingSubtasks), "tool", call.Name)
					if err := r.executeDelegatedTasks(ctx, s, s.pendingSubtasks, input); err != nil {
						s.out.FinalText = fmt.Sprintf("Delegation failed: %v", err)
						return s.finalizeOutput(nil)
					}
					s.pendingSubtasks = nil
					s.delegationLocked = false
					s.delegationMode = ""
					slog.Debug("delegation: subtasks completed, unlocking delegation mode")
					break gatingLoop
				}
				// Rewrite to delegation call (fallback when no pending subtasks)
				rewritten := s.rewriteToDelegation(call)
				filteredCalls = append(filteredCalls, rewritten)
				s.toolRewriteCount++
			}
			resp.ToolCalls = filteredCalls

			if len(resp.ToolCalls) == 0 && strings.TrimSpace(resp.FinalText) != "" {
				s.noProgressIterations++
				if s.noProgressIterations >= 1 && len(s.pendingSubtasks) > 0 {
					slog.Debug("delegation: executing pending subtasks (model produced no tool calls)", "pending_count", len(s.pendingSubtasks))
					if err := r.executeDelegatedTasks(ctx, s, s.pendingSubtasks, input); err != nil {
						s.out.FinalText = fmt.Sprintf("Delegation failed: %v", err)
						return s.finalizeOutput(nil)
					}
					s.pendingSubtasks = nil
					s.delegationLocked = false
					s.delegationMode = ""
					slog.Debug("delegation: subtasks completed, unlocking delegation mode")
					continue
				}
				if s.noProgressIterations <= 1 {
					s.setLastModelOutput(resp.FinalText)
					s.messages = append(s.messages, ChatMessage{Role: "assistant", Content: resp.FinalText})
					continue
				}
			}
		}

		s.setLastModelOutput(resp.FinalText)

		toolOutcome := s.executeTools(ctx, r, resp.ToolCalls, input)
		s.toolParseReprompts = 0
		s.updateLastIterationOutcome(len(resp.ToolCalls))

		if blockerCat, blockerCount := s.structuralBlockerTriggered(); blockerCat != "" {
			s.out.FinalText = formatStructuralBlockerEscalation(blockerCat, blockerCount, s.toolResults)
			return s.finalizeOutput(nil)
		}

		if toolOutcome.blockedSuccessfulOneShotMutation && len(s.toolResults) > 0 {
			if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, input.SystemPromptExt, input.Source, "# SUCCESSFUL_MUTATION_REPEAT_RECOVERY\n- A previous successful one-shot mutation was requested again.\n- Do not call tools in this recovery turn.\n- Finalize from the current successful tool results."); finalized != "" {
				s.out.FinalText = finalized
			} else {
				s.out.FinalText = fallbackFromNoProgressToolResults(s.toolResults)
			}
			return s.finalizeOutput(nil)
		}

		if toolOutcome.hadFreshExecution {
			s.noProgressIterations = 0
			s.allBlockedIterations = 0
		} else {
			s.noProgressIterations++
			allRepetitionBlocked := len(resp.ToolCalls) > 0
			for _, tr := range s.toolResults[len(s.toolResults)-len(resp.ToolCalls):] {
				if !strings.Contains(tr.Error, "repetition detected") {
					allRepetitionBlocked = false
					break
				}
			}
			if allRepetitionBlocked {
				s.allBlockedIterations++
			} else {
				s.allBlockedIterations = 0
			}
		}

		if s.allBlockedIterations >= 2 && len(s.toolResults) > 0 {
			if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, input.SystemPromptExt, input.Source, ""); finalized != "" {
				s.out.FinalText = finalized
			} else {
				s.out.FinalText = fallbackFromNoProgressToolResults(s.toolResults)
			}
			return s.finalizeOutput(nil)
		}

		if s.failureRecoveryActive && s.failuresSinceRecovery >= failureGuidanceEscalation && len(s.out.ToolCalls) > 0 {
			s.out.FinalText = finalizeAfterFailureEscalation(input.Message, s.out.ToolCalls)
			return s.finalizeOutput(nil)
		}

		if s.noProgressIterations >= repeatedNoProgressLoopCapTr