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
	agentCreateGlobalCap         = 8
	shellExecRepetitionCap       = 3
	memoryWriteRepetitionCap     = 2
	becomussyWriteRepetitionCap  = 2
	becomussyReadRepetitionCap   = 6
	noChoicesRetryCap            = 3
	noChoicesRetryDelay          = 300 * time.Millisecond
	transientModelRetryCap       = 3
	transientModelRetryBaseDelay = 500 * time.Millisecond
	toolParseRetryCap            = 4
	maxToolRewriteBudget         = 3 // Maximum rewrites to prevent infinite loops
)

// runState encapsulates the mutable state of a single agent run loop.
type runState struct {
	out                     RunOutput
	runID                   string
	agentID                 string
	parentRunID             string
	decisionLedger          *DecisionLedger
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
	allBlockedIterations    int
	latestThinking          string
	thinkingPresent         bool
	toolParseFailure        bool
	toolParseReprompts      int
	followThroughReprompts  int
	lastIterationMixed      bool
	lastIterationSucceeded  []string
	lastIterationFailed     []string
	toolCap                 int
	// repetitionPrevention tracks tool calls to detect and prevent loops
	repetitionPrevention map[string]int // key: "tool_name|agent_id" -> count

	// structuralBlockerCounts tracks infrastructure-level error categories
	// that cannot be fixed by the agent retrying (e.g. missing parent
	// directory, capability denied).  When any category reaches the cap
	// the run is hard-stopped with an escalation message to the owner.
	structuralBlockerCounts map[string]int

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

	return &runState{
		out:                     out,
		runID:                   strings.TrimSpace(input.RunID),
		agentID:                 strings.TrimSpace(input.AgentID),
		parentRunID:             strings.TrimSpace(input.ParentRunID),
		decisionLedger:          input.DecisionLedger,
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
		structuralBlockerCounts: make(map[string]int),
		lastPromptTokens:        0,
		contextWindow:           120000, // default context window
		delegationMode:          "",
		delegationCooldown:      0,
		delegationActive:        false,
		pendingSubtasks:         nil,
		completedSubtasks:       make(map[string]string),
		delegationArtifacts:     make(map[string]string),
		lastModelOutput:         "",
		recentIntents:           make([]string, 0),
		toolRewriteCount:        0,
		delegationLocked:        false,
		delegationReason:        "",
	}
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
				s.emitToolDecision(ctx, call, result)
				continue
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
					s.emitToolDecision(ctx, call, result)
					continue
				}
			}
			// Global cap on total agent.create calls to prevent agent spam
			globalCreateKey := "agent.create|total"
			s.repetitionPrevention[globalCreateKey]++
			if s.repetitionPrevention[globalCreateKey] > agentCreateGlobalCap {
				result := ToolCallResult{
					ID:     call.ID,
					Output: "",
					Error:  fmt.Sprintf("agent creation limit reached: you have already created %d agents in this run. Reuse existing agents or finish with the current roster before creating more.", s.repetitionPrevention[globalCreateKey]-1),
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
				continue
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
		s.emitToolDecision(ctx, call, result)
	}
	return hadFreshExecution
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

		// Use tracked prompt tokens and context window for delegation evaluation
		promptTokens := s.lastPromptTokens
		contextWindow := s.contextWindow

		// Only evaluate delegation trigger if not already locked in forced mode
		if !s.delegationLocked {
			if trigger := s.computeDelegationTrigger(promptTokens, contextWindow, snapshot); trigger != nil {
				s.delegationReason = strings.TrimSpace(trigger.Reason)
				s.emitDecision(ctx, DecisionRecordTypeDelegationTrigger, map[string]any{
					"reason":          strings.TrimSpace(trigger.Reason),
					"mode":            string(trigger.Mode),
					"cooldown_for":    trigger.CooldownFor,
					"context_tokens":  promptTokens,
					"context_window":  contextWindow,
					"subtask_count":   len(trigger.Subtasks),
					"snapshot_tool":   strings.TrimSpace(snapshot.LastToolAttempted),
					"snapshot_errors": append([]string(nil), snapshot.LastErrorTypes...),
				}, fmt.Sprintf("Delegation trigger fired (%s): %s", strings.TrimSpace(string(trigger.Mode)), firstN(strings.TrimSpace(trigger.Reason), 140)))

				configuredMode := normalizeDelegationMode(input.DelegationMode)
				if isPlannerDelegationMode(configuredMode) {
					plan, plannedTasks, planErr := GenerateDecompositionPlan(trigger.Reason, configuredMode, trigger.Subtasks, nil)
					if planErr != nil {
						s.out.Thinking = s.latestThinking
						s.out.ThinkingPresent = s.thinkingPresent
						s.out.ToolParseFailure = s.toolParseFailure
						s.out.CompletedAt = time.Now().UTC()
						return s.out, planErr
					}

					s.out.DecompositionPlan = &plan
					s.pendingSubtasks = plannedTasks
					s.delegationActive = true
					s.appendPlannedDelegationEvents(plan)
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
						s.out.Thinking = s.latestThinking
						s.out.ThinkingPresent = s.thinkingPresent
						s.out.ToolParseFailure = s.toolParseFailure
						s.out.CompletedAt = time.Now().UTC()
						return s.out, nil
					}

					if err := r.executeDelegatedTasks(ctx, s, plannedTasks, input); err != nil {
						s.out.Thinking = s.latestThinking
						s.out.ThinkingPresent = s.thinkingPresent
						s.out.ToolParseFailure = s.toolParseFailure
						s.out.CompletedAt = time.Now().UTC()
						return s.out, err
					}
					s.out.Thinking = s.latestThinking
					s.out.ThinkingPresent = s.thinkingPresent
					s.out.ToolParseFailure = s.toolParseFailure
					s.out.CompletedAt = time.Now().UTC()
					return s.out, nil
				}

				// Downgrade execution-dependent modes when no SubAgentRunner is
				// available.  PromptOnly mode is always safe (just a system-prompt
				// hint) so we keep it; ToolGated and AutoExecute require the runner
				// and would otherwise fail with "subagent runner not configured".
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

				// Lock delegation mode once we enter forced or critical mode
				if trigger.Mode == DelegationModeToolGated || trigger.Mode == DelegationModeAutoExecute {
					s.delegationLocked = true
				}

				// AUTO-EXECUTE mode: bypass model entirely
				if trigger.Mode == DelegationModeAutoExecute && input.AutoDelegate {
					if err := r.executeDelegatedTasks(ctx, s, trigger.Subtasks, input); err != nil {
						s.out.Thinking = s.latestThinking
						s.out.ThinkingPresent = s.thinkingPresent
						s.out.ToolParseFailure = s.toolParseFailure
						s.out.CompletedAt = time.Now().UTC()
						return s.out, err
					}
					s.out.Thinking = s.latestThinking
					s.out.ThinkingPresent = s.thinkingPresent
					s.out.ToolParseFailure = s.toolParseFailure
					s.out.CompletedAt = time.Now().UTC()
					return s.out, nil
				}

				// TOOL_GATED mode: inject directive
				if trigger.Mode == DelegationModeToolGated {
					systemPrompt = appendPromptDirective(systemPrompt, buildForcedDelegationDirective(trigger))
				}

				// PROMPT_ONLY mode: soft hint
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
		// Update context tracking from model response
		if resp.PromptTokens > 0 {
			s.lastPromptTokens = resp.PromptTokens
		}
		if err != nil {
			s.out.Thinking = s.latestThinking
			s.out.ThinkingPresent = s.thinkingPresent
			s.out.ToolParseFailure = s.toolParseFailure
			if isContextCanceledModelError(ctx, err) {
				s.out.CompletedAt = time.Now().UTC()
				return s.out, context.Canceled
			}
			if len(s.toolResults) > 0 {
				if isTransientProviderModelError(err) {
					if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, input.SystemPromptExt, "# TRANSIENT_MODEL_ERROR_RECOVERY\n- Your previous model turn ended with a transient provider interruption after tool work.\n- Do not call tools in this recovery turn.\n- Use the latest tool results to answer the user directly.\n- If the tool results are incomplete, say what remains and mention that the stream was interrupted."); finalized != "" {
						s.out.FinalText = strings.TrimSpace(finalized)
						s.out.CompletedAt = time.Now().UTC()
						return s.out, nil
					}
				}
				s.out.FinalText = recoverFromModelError(err, s.toolResults, s.toolCap)
				s.out.CompletedAt = time.Now().UTC()
				return s.out, nil
			}
			if input.OnTextDelta != nil && (isTransientProviderModelError(err) || isProviderNoChoicesError(err)) {
				s.out.FinalText = recoverFromInterruptedStream(err)
				s.out.CompletedAt = time.Now().UTC()
				return s.out, nil
			}
			s.out.CompletedAt = time.Now().UTC()
			return s.out, err
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
				s.out.Thinking = s.latestThinking
				s.out.ThinkingPresent = s.thinkingPresent
				s.out.ToolParseFailure = s.toolParseFailure
				s.out.CompletedAt = time.Now().UTC()
				return s.out, nil
			}
			finalText := strings.TrimSpace(resp.FinalText)
			if finalText == "" {
				if len(s.toolResults) > 0 {
					if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, input.SystemPromptExt, "# EMPTY_FINAL_TEXT_RECOVERY\n- Your previous final response was empty.\n- Provide a concise user-facing final answer from the latest tool results."); finalized != "" {
						finalText = strings.TrimSpace(finalized)
					}
				}
				if finalText == "" {
					finalText = recoverFromEmptyFinal(s.toolResults)
				}
			}
			s.out.FinalText = finalText
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

		// === TOOL GATING IN FORCED MODE ===
		if (s.delegationMode == DelegationModeToolGated || s.delegationMode == DelegationModeAutoExecute) && len(resp.ToolCalls) > 0 {
			filteredCalls := make([]ToolCallRequest, 0, len(resp.ToolCalls))
			for _, call := range resp.ToolCalls {
				if s.isToolAllowedInDelegationMode(call.Name) {
					filteredCalls = append(filteredCalls, call)
				} else {
					s.emitDecision(ctx, DecisionRecordTypeToolDecision, map[string]any{
						"tool":             strings.TrimSpace(call.Name),
						"tool_call_id":     strings.TrimSpace(call.ID),
						"decision":         "denied",
						"policy_reference": "policy.delegation_mode",
						"reason":           fmt.Sprintf("tool %s is blocked while delegation mode is %s", strings.TrimSpace(call.Name), strings.TrimSpace(string(s.delegationMode))),
					}, fmt.Sprintf("Denied tool %s due to delegation mode constraints.", strings.TrimSpace(call.Name)))
					// Check rewrite budget to prevent infinite loops
					if s.toolRewriteCount >= maxToolRewriteBudget {
						// Execute pending subtasks directly if we have them
						if len(s.pendingSubtasks) > 0 {
							slog.Debug("delegation: executing pending subtasks (rewrite budget exceeded)", "pending_count", len(s.pendingSubtasks))
							if err := r.executeDelegatedTasks(ctx, s, s.pendingSubtasks, input); err != nil {
								s.out.FinalText = fmt.Sprintf("Delegation failed: %v", err)
								s.out.Thinking = s.latestThinking
								s.out.ThinkingPresent = s.thinkingPresent
								s.out.ToolParseFailure = s.toolParseFailure
								s.out.CompletedAt = time.Now().UTC()
								return s.out, nil
							}
							// Subtasks completed, unlock delegation and continue
							s.pendingSubtasks = nil
							s.delegationLocked = false
							s.delegationMode = ""
							slog.Debug("delegation: subtasks completed, unlocking delegation mode")
							continue
						}
						// Budget exceeded - fail fast with clear message
						s.out.FinalText = fmt.Sprintf("DELEGATION_REWRITE_BUDGET_EXCEEDED: Task requires delegation but exceeded maximum rewrite attempts (%d). The subagent keeps trying to use forbidden tools. Please break this task into smaller, independent subtasks manually.", maxToolRewriteBudget)
						s.out.Thinking = s.latestThinking
						s.out.ThinkingPresent = s.thinkingPresent
						s.out.ToolParseFailure = s.toolParseFailure
						s.out.CompletedAt = time.Now().UTC()
						return s.out, nil
					}
					// Execute pending subtasks directly instead of rewriting
					if len(s.pendingSubtasks) > 0 {
						slog.Debug("delegation: executing pending subtasks (forbidden tool attempted)", "pending_count", len(s.pendingSubtasks), "tool", call.Name)
						if err := r.executeDelegatedTasks(ctx, s, s.pendingSubtasks, input); err != nil {
							s.out.FinalText = fmt.Sprintf("Delegation failed: %v", err)
							s.out.Thinking = s.latestThinking
							s.out.ThinkingPresent = s.thinkingPresent
							s.out.ToolParseFailure = s.toolParseFailure
							s.out.CompletedAt = time.Now().UTC()
							return s.out, nil
						}
						// Subtasks completed, unlock delegation and continue
						s.pendingSubtasks = nil
						s.delegationLocked = false
						s.delegationMode = ""
						slog.Debug("delegation: subtasks completed, unlocking delegation mode")
						continue
					}
					// Rewrite to delegation call (fallback when no pending subtasks)
					rewritten := s.rewriteToDelegation(call)
					filteredCalls = append(filteredCalls, rewritten)
					s.toolRewriteCount++
				}
			}
			resp.ToolCalls = filteredCalls

			// If model output plain text with no tool calls in forced mode
			if len(resp.ToolCalls) == 0 && strings.TrimSpace(resp.FinalText) != "" {
				s.noProgressIterations++
				// Execute pending subtasks directly if we have them
				if s.noProgressIterations >= 1 && len(s.pendingSubtasks) > 0 {
					slog.Debug("delegation: executing pending subtasks (model produced no tool calls)", "pending_count", len(s.pendingSubtasks))
					if err := r.executeDelegatedTasks(ctx, s, s.pendingSubtasks, input); err != nil {
						s.out.FinalText = fmt.Sprintf("Delegation failed: %v", err)
						s.out.Thinking = s.latestThinking
						s.out.ThinkingPresent = s.thinkingPresent
						s.out.ToolParseFailure = s.toolParseFailure
						s.out.CompletedAt = time.Now().UTC()
						return s.out, nil
					}
					// Subtasks completed, unlock delegation and continue to let model process results
					s.pendingSubtasks = nil
					s.delegationLocked = false
					s.delegationMode = ""
					slog.Debug("delegation: subtasks completed, unlocking delegation mode")
					continue
				}
				// Re-prompt once with stronger directive
				if s.noProgressIterations <= 1 {
					s.setLastModelOutput(resp.FinalText)
					s.messages = append(s.messages, ChatMessage{Role: "assistant", Content: resp.FinalText})
					continue
				}
			}
		}

		// Update model output tracking
		s.setLastModelOutput(resp.FinalText)

		hadFreshExecution := s.executeTools(ctx, r, resp.ToolCalls, input)
		s.toolParseReprompts = 0
		s.updateLastIterationOutcome(len(resp.ToolCalls))

		// Hard-stop on structural blockers.  These are infrastructure
		// problems (missing directories, permission denials) that cannot
		// be resolved by retrying.  Escalate to the owner immediately.
		if blockerCat, blockerCount := s.structuralBlockerTriggered(); blockerCat != "" {
			s.out.FinalText = formatStructuralBlockerEscalation(blockerCat, blockerCount, s.toolResults)
			s.out.Thinking = s.latestThinking
			s.out.ThinkingPresent = s.thinkingPresent
			s.out.ToolParseFailure = s.toolParseFailure
			s.out.CompletedAt = time.Now().UTC()
			return s.out, nil
		}

		if hadFreshExecution {
			s.noProgressIterations = 0
			s.allBlockedIterations = 0
		} else {
			s.noProgressIterations++
			// Check if ALL results from this iteration were repetition-blocked
			// (not just cached). If so, finalize faster than the generic no-progress path.
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

		// Fast-finalize when all tool calls have been repetition-blocked for 2+ iterations
		if s.allBlockedIterations >= 2 && len(s.toolResults) > 0 {
			if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, input.SystemPromptExt, ""); finalized != "" {
				s.out.FinalText = finalized
			} else {
				s.out.FinalText = fallbackFromNoProgressToolResults(s.toolResults)
			}
			s.out.Thinking = s.latestThinking
			s.out.ThinkingPresent = s.thinkingPresent
			s.out.ToolParseFailure = s.toolParseFailure
			s.out.CompletedAt = time.Now().UTC()
			return s.out, nil
		}

		if s.failureRecoveryActive && s.failuresSinceRecovery >= failureGuidanceEscalation && len(s.out.ToolCalls) > 0 {
			s.out.FinalText = finalizeAfterFailureEscalation(input.Message, s.out.ToolCalls)
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
				s.out.FinalText = fallbackFromNoProgressToolResults(s.toolResults)
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

func (s *runState) updateLastIterationOutcome(callCount int) {
	s.lastIterationMixed = false
	s.lastIterationSucceeded = nil
	s.lastIterationFailed = nil
	if callCount <= 0 || len(s.out.ToolCalls) < callCount {
		return
	}
	start := len(s.out.ToolCalls) - callCount
	iterRecords := s.out.ToolCalls[start:]
	succeeded := make(map[string]struct{})
	failed := make(map[string]struct{})
	for _, rec := range iterRecords {
		name := strings.TrimSpace(rec.Request.Name)
		if name == "" {
			name = "unknown.tool"
		}
		if strings.TrimSpace(rec.Result.Error) == "" {
			succeeded[name] = struct{}{}
		} else {
			failed[name] = struct{}{}
		}
	}
	if len(succeeded) == 0 || len(failed) == 0 {
		return
	}
	s.lastIterationMixed = true
	s.lastIterationSucceeded = sortedToolNames(succeeded)
	s.lastIterationFailed = sortedToolNames(failed)
}

func sortedToolNames(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for name := range values {
		out = append(out, name)
	}
	sort.Strings(out)
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func shouldRetryToolParseFailure(finalText string, allowedTools []string) bool {
	if len(allowedTools) == 0 {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(finalText))
	if strings.Contains(text, "not enabled for this agent") ||
		strings.Contains(text, "please enable it and retry") ||
		strings.Contains(text, "network.enabled=true") ||
		strings.Contains(text, "shell.enable_exec=true") {
		return false
	}
	return true
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
	if strings.Contains(text, "context canceled") || strings.Contains(text, "context cancelled") {
		return false
	}
	return strings.Contains(text, "stream interrupted") ||
		strings.Contains(text, "context deadline exceeded") ||
		strings.Contains(text, "client.timeout") ||
		strings.Contains(text, "timeout while reading body") ||
		strings.Contains(text, "i/o timeout") ||
		strings.Contains(text, "connection reset") ||
		strings.Contains(text, "unexpected eof") ||
		strings.Contains(text, "provider returned no choices")
}

func isContextCanceledModelError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	if ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "context canceled") || strings.Contains(text, "context cancelled")
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
		taskID := normalizeTaskIDForRepetition(firstTrimmedStringFromMap(args, "task_id", "taskId"))
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
		taskID := normalizeTaskIDForRepetition(firstTrimmedStringFromMap(args, "task_id", "taskId"))
		if taskID != "" {
			return name + "|" + agentID + "|" + taskID, agentRunCap, true
		}
		message := firstTrimmedStringFromMap(args, "message", "content", "text", "body", "prompt")
		if message == "" {
			return "", 0, false
		}
		return name + "|" + agentID + "|" + firstN(strings.ToLower(message), 80), agentRunCap, true
	}
	if name == "memory.write" {
		key, ok := normalizedMemoryWriteKey(call.Arguments)
		if !ok {
			return "", 0, false
		}
		return "memory.write|" + key, memoryWriteRepetitionCap, true
	}

	// becomussy tools: write tools (create, propose, reinforce) get a tight
	// cap to prevent duplicate entries; read tools (search, list, get,
	// resume, current, history, pending) get a higher cap because they are
	// idempotent and commonly re-invoked during batch workflows.
	if strings.HasPrefix(name, "becomussy.") {
		cap := becomussyReadRepetitionCap
		if strings.HasSuffix(name, ".create") || strings.HasSuffix(name, ".propose") || strings.HasSuffix(name, ".reinforce") {
			cap = becomussyWriteRepetitionCap
		}
		return name + "|" + string(call.Arguments), cap, true
	}

	if name != "fs.write" && name != "fs.append" && name != "fs.read" && name != "shell.exec" {
		return "", 0, false
	}

	// shell.exec: prevent the same shell command from being run more than 3 times
	if name == "shell.exec" {
		args, ok := parseToolArgs(call.Arguments)
		if !ok {
			return "", 0, false
		}
		command := firstTrimmedStringFromMap(args, "command", "cmd")
		cmdArgs := ""
		if rawArgs, hasArgs := args["args"]; hasArgs {
			if encoded, err := json.Marshal(rawArgs); err == nil {
				cmdArgs = string(encoded)
			}
		}
		key := name + "|" + command + "|" + firstN(cmdArgs, 120)
		return key, shellExecRepetitionCap, true
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
		if toolName == "fs.append" {
			if isBuildLogPath {
				return buildLogFileAppendCap, true
			}
			if isJournalPath {
				return journalFileAppendCap, true
			}
			return defaultRepeatedFileAppendCap, true
		}
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

func toolCallCacheKey(call ToolCallRequest) string {
	name := strings.TrimSpace(call.Name)
	// fs.list and fs.read must never be cached — directory contents and file
	// contents change after writes.  Returning stale cached results causes the
	// agent to believe files are missing or have old content.
	if name == "fs.list" || name == "fs.read" {
		return "|"
	}
	// becomussy tools should not be cached — each call should either
	// execute fresh (on first use) or be caught by repetition detection
	// (on subsequent identical calls) so the model gets the explicit
	// "repetition detected" signal to stop and produce a text response.
	if strings.HasPrefix(name, "becomussy.") {
		return "|"
	}
	if name == "http.request" || name == "net.fetch" {
		if key, ok := normalizedHTTPRequestCacheKey(call.Arguments); ok {
			return "http.request|" + key
		}
	}
	if name == "memory.write" {
		if key, ok := normalizedMemoryWriteKey(call.Arguments); ok {
			return "memory.write|" + key
		}
	}
	return call.Name + "|" + string(call.Arguments)
}

func normalizedHTTPRequestCacheKey(args []byte) (string, bool) {
	payload, ok := parseToolArgs(args)
	if !ok {
		return "", false
	}
	rawURL := firstTrimmedStringFromMap(payload, "url", "uri", "endpoint")
	if rawURL == "" {
		return "", false
	}
	method := strings.ToUpper(firstTrimmedStringFromMap(payload, "method"))
	if method == "" {
		method = "GET"
	}
	body := firstTrimmedStringFromMap(payload, "body", "data", "payload")
	return method + "|" + normalizeRequestURL(rawURL) + "|" + firstN(body, 200), true
}

func normalizedMemoryWriteKey(args []byte) (string, bool) {
	payload, ok := parseToolArgs(args)
	if !ok {
		return "", false
	}
	title := firstTrimmedStringFromMap(payload, "title", "name", "key")
	content := firstTrimmedStringFromMap(payload, "content", "summary", "text", "value")
	kind := firstTrimmedStringFromMap(payload, "kind", "type")
	if title == "" && content == "" {
		return "", false
	}
	return strings.ToLower(kind) + "|" + strings.ToLower(title) + "|" + firstN(strings.ToLower(content), 160), true
}

func normalizeRequestURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return strings.ToLower(trimmed)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if strings.HasSuffix(parsed.Host, ":80") && parsed.Scheme == "http" {
		parsed.Host = strings.TrimSuffix(parsed.Host, ":80")
	}
	if strings.HasSuffix(parsed.Host, ":443") && parsed.Scheme == "https" {
		parsed.Host = strings.TrimSuffix(parsed.Host, ":443")
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	parsed.Fragment = ""
	return parsed.String()
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

// repetitionSuffixPattern matches common retry/version/continuation suffixes at the end of task IDs.
var repetitionSuffixPattern = regexp.MustCompile(`(?i)-(v\d+|retry|continue|attempt\d*|redo|fix)$`)

func normalizeTaskIDForRepetition(taskID string) string {
	value := strings.ToLower(strings.TrimSpace(taskID))
	if value == "" {
		return ""
	}
	cut := len(value)
	for _, sep := range []string{"-", "_", ".", " "} {
		if idx := strings.Index(value, sep); idx >= 0 && idx < cut {
			cut = idx
		}
	}
	if cut > 0 {
		base := strings.TrimSpace(value[:cut])
		if looksLaneTaskID(base) {
			return base
		}
	}
	if looksLaneTaskID(value) {
		return value
	}
	// Strip common retry/version/continuation suffixes so that
	// "task-name-v2", "task-name-retry", "task-name-continue", etc.
	// all normalize to "task-name".
	value = repetitionSuffixPattern.ReplaceAllString(value, "")
	return value
}

func looksLaneTaskID(value string) bool {
	if value == "" {
		return false
	}
	seenLetter := false
	seenDigit := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' {
			if seenDigit {
				return false
			}
			seenLetter = true
			continue
		}
		if r >= '0' && r <= '9' {
			if !seenLetter {
				return false
			}
			seenDigit = true
			continue
		}
		return false
	}
	return seenLetter && seenDigit
}

func extractPathFromToolArgs(args []byte) string {
	payload, ok := parseToolArgs(args)
	if !ok {
		return ""
	}
	return firstTrimmedStringFromMap(payload, "path", "file", "target", "filename")
}

// Delegation state methods

func (s *runState) computeDelegationTrigger(promptTokens, contextWindow int, snapshot StateSnapshot) *DelegationTrigger {
	if s.delegationCooldown > 0 {
		s.delegationCooldown--
		return nil
	}

	score := ComputeComplexity(s, promptTokens, contextWindow)
	trigger := ShouldTriggerDelegation(score, s, snapshot)

	if trigger != nil {
		slog.Debug("delegation: trigger fired", "level", score.Level, "mode", trigger.Mode, "triggers", score.Triggers, "score", score)
		trigger.Subtasks = DecomposeTask(s.out.Prompt, score, snapshot)
		slog.Debug("delegation: task decomposed", "subtask_count", len(trigger.Subtasks))
	}

	return trigger
}

func (s *runState) isToolAllowedInDelegationMode(toolName string) bool {
	if s.delegationMode == "" || s.delegationMode == DelegationModePromptOnly {
		return true
	}

	allowed := map[string]bool{
		"agent.list": true,
		"agent.run":  true,
	}
	return allowed[toolName]
}

func (s *runState) rewriteToDelegation(call ToolCallRequest) ToolCallRequest {
	slog.Debug("delegation: rewriting tool call to agent.run", "from_tool", call.Name, "rewrite_count", s.toolRewriteCount+1, "max_rewrites", maxToolRewriteBudget)
	message := fmt.Sprintf("Delegation rewrite: the parent run attempted `%s` with args %s but delegation mode only allows agent.run. Complete the underlying user-facing objective using the best available approach. Do not repeat the same blocked tool path unchanged. Report what you did, what you verified, and any remaining blocker.", call.Name, string(call.Arguments))
	newArgs, _ := json.Marshal(map[string]any{
		"agent_id":      "default",
		"task_id":       "auto-delegated-" + call.ID,
		"message":       message,
		"thinking_mode": "never",
	})
	return ToolCallRequest{
		ID:        call.ID,
		Name:      "agent.run",
		Arguments: newArgs,
	}
}

func (s *runState) getLastToolName() string {
	if len(s.out.ToolCalls) == 0 {
		return ""
	}
	return s.out.ToolCalls[len(s.out.ToolCalls)-1].Request.Name
}

func (s *runState) getRecentErrorTypes() []string {
	errors := make([]string, 0)
	for i := len(s.toolResults) - 1; i >= 0 && len(errors) < 3; i-- {
		if strings.TrimSpace(s.toolResults[i].Error) != "" {
			errors = append(errors, s.toolResults[i].Error)
		}
	}
	return errors
}

func (s *runState) getLastModelOutput() string {
	return s.lastModelOutput
}

func (s *runState) setLastModelOutput(output string) {
	s.lastModelOutput = output
	s.recentIntents = append(s.recentIntents, output)
	if len(s.recentIntents) > 3 {
		s.recentIntents = s.recentIntents[1:]
	}
}

func (s *runState) appendPlannedDelegationEvents(plan DecompositionPlan) {
	for _, task := range plan.Tasks {
		s.appendDelegationEvent(DelegationEvent{
			Timestamp:      time.Now().UTC(),
			TaskID:         task.TaskID,
			TriggerReason:  strings.TrimSpace(plan.TriggerReason),
			SelectedRole:   strings.TrimSpace(task.AssignedRole),
			Confidence:     task.Confidence,
			TaskAssignment: strings.TrimSpace(task.Description),
			Rationale:      strings.TrimSpace(task.Rationale),
			Outcome:        "planned",
		})
	}
}

func (s *runState) appendDelegationEvent(event DelegationEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	event.TriggerReason = strings.TrimSpace(event.TriggerReason)
	event.SelectedRole = strings.TrimSpace(event.SelectedRole)
	event.TaskAssignment = strings.TrimSpace(event.TaskAssignment)
	event.Rationale = strings.TrimSpace(event.Rationale)
	event.Outcome = strings.TrimSpace(event.Outcome)
	s.out.DelegationEvents = append(s.out.DelegationEvents, event)
}
