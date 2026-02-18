package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
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

func (s *runState) prepareSystemPrompt() string {
	systemPrompt := s.out.Prompt
	if s.failureRecoveryActive {
		systemPrompt = appendPromptDirective(systemPrompt, "# ERROR_RECOVERY_MODE\n- Recent tool calls failed. Analyze the latest errors and outputs before choosing the next action.\n- Try a materially different approach to resolve the error.\n- Do not repeat the same failing command/arguments unless you explain why it should now work.")
	}
	if s.followThroughReprompts > 0 {
		systemPrompt = appendPromptDirective(systemPrompt, "# ACTION_EXECUTION_MODE\n- You previously replied with intent to act but did not execute.\n- In this turn, either call required tools now or provide a concrete final answer from existing evidence.\n- Do not defer with phrases like 'let me check' or promise future action without execution.")
	}
	return systemPrompt
}

func (s *runState) executeTools(ctx context.Context, r Runner, toolCalls []ToolCallRequest, input RunInput) bool {
	hadFreshExecution := false
	for _, incoming := range toolCalls {
		s.toolCallOrdinal++
		call := incoming
		call.ID = uniqueToolCallID(call.ID, s.toolCallOrdinal, s.usedToolCallIDs)

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
		systemPrompt := s.prepareSystemPrompt()

		resp, err := r.Model.Generate(ctx, ModelRequest{
			AgentID:       input.AgentID,
			RunID:         input.RunID,
			SystemPrompt:  systemPrompt,
			Messages:      append([]ChatMessage(nil), s.messages...),
			AllowedTools:  append([]string(nil), input.AllowedTools...),
			ToolTimeoutMS: input.ToolTimeoutMS,
			Prompt:        systemPrompt,
			Message:       input.Message,
			ToolResults:   append([]ToolCallResult(nil), s.toolResults...),
		})
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
				if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, ""); finalized != "" {
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
			if finalized := finalizeFromToolResults(ctx, r.Model, input.AgentID, input.RunID, s.out.Prompt, s.messages, input.Message, input.ToolTimeoutMS, s.toolResults, ""); finalized != "" {
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
