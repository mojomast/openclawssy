package agent

import "strings"

const (
	ComplexityLow      = 0
	ComplexityModerate = 1 // warn
	ComplexityHigh     = 2 // force
	ComplexityCritical = 3 // auto-execute, bypass model
)

const (
	// Thresholds
	warnIterationThreshold       = 40
	forceIterationThreshold      = 80
	forceNoProgressThreshold     = 2
	forceFailureThreshold        = 2
	forceBlockedThreshold        = 1
	forceRepetitionThreshold     = 2
	warnContextPressureRatio     = 0.75
	forceContextPressureRatio    = 0.85
	criticalContextPressureRatio = 0.92
)

type ComplexityScore struct {
	Level          int
	TotalScore     int
	IterationScore int
	ProgressScore  int
	FailureScore   int
	BlockedScore   int
	LoopScore      int
	ContextScore   int
	Triggers       []string
}

type DelegationMode string

const (
	DelegationModePromptOnly  DelegationMode = "prompt_only"
	DelegationModeToolGated   DelegationMode = "tool_gated"
	DelegationModeAutoExecute DelegationMode = "auto_execute"
	DelegationModeSuggestOnly DelegationMode = "suggest_only"
	DelegationModeApprovePlan DelegationMode = "approve_plan"
	DelegationModeAutoTrusted DelegationMode = "auto_trusted"
	DelegationModeFullAuto    DelegationMode = "full_autonomous"
)

type DelegationTrigger struct {
	Mode         DelegationMode
	Reason       string
	Subtasks     []DecomposedTask
	Directive    string
	AllowedTools []string
	CooldownFor  int
}

type DecomposedTask struct {
	TaskID                string   `json:"task_id"`
	AgentID               string   `json:"agent_id"`
	Message               string   `json:"message"`
	AcceptanceCrit        []string `json:"acceptance_criteria,omitempty"`
	DependsOn             []string `json:"depends_on,omitempty"`
	Produces              []string `json:"produces,omitempty"`
	Priority              int      `json:"priority"`
	TimeoutMS             int      `json:"timeout_ms,omitempty"`
	ThinkingMode          string   `json:"thinking_mode,omitempty"`
	AssignedRole          string   `json:"assigned_role,omitempty"`
	RoutingConfidence     float64  `json:"routing_confidence,omitempty"`
	RoutingRationale      string   `json:"routing_rationale,omitempty"`
	RoleAllowedTools      []string `json:"role_allowed_tools,omitempty"`
	RoleMaxToolIterations int      `json:"role_max_tool_iterations,omitempty"`
	RoleTimeoutMS         int      `json:"role_timeout_ms,omitempty"`
}

type StateSnapshot struct {
	LastToolAttempted string
	LastErrorTypes    []string
	RecentIntents     []string
	LastModelOutput   string
	AskedUserQuestion bool
}

func ComputeComplexity(state *runState, promptTokens, contextWindow int) ComplexityScore {
	score := ComplexityScore{Triggers: make([]string, 0, 6)}

	// FailureScore: stuck signals weighted heavily
	if state.consecutiveToolFailures >= 3 {
		score.FailureScore = 3
		score.Triggers = append(score.Triggers, "failures>=3")
	} else if state.consecutiveToolFailures >= 2 {
		score.FailureScore = 2
		score.Triggers = append(score.Triggers, "failures>=2")
	}

	// ProgressScore
	if state.noProgressIterations >= 2 {
		score.ProgressScore = 2
		score.Triggers = append(score.Triggers, "no_progress>=2")
	}

	// BlockedScore (highest weight)
	if state.allBlockedIterations >= 1 {
		score.BlockedScore = 3
		score.Triggers = append(score.Triggers, "all_blocked>=1")
	}

	// LoopScore
	for key, count := range state.repetitionPrevention {
		if count >= 2 {
			score.LoopScore = 2
			score.Triggers = append(score.Triggers, "repetition:"+key)
			break
		}
	}

	// ContextScore
	if contextWindow > 0 {
		ratio := float64(promptTokens) / float64(contextWindow)
		if ratio >= 0.92 {
			score.ContextScore = 3
			score.Triggers = append(score.Triggers, "context>92%")
		} else if ratio >= 0.85 {
			score.ContextScore = 2
			score.Triggers = append(score.Triggers, "context>85%")
		} else if ratio >= 0.75 {
			score.ContextScore = 1
			score.Triggers = append(score.Triggers, "context>75%")
		}
	}

	// IterationScore (secondary signal)
	if state.toolIterations >= 120 {
		score.IterationScore = 3
		score.Triggers = append(score.Triggers, "iterations>=120")
	} else if state.toolIterations >= 80 {
		score.IterationScore = 2
		score.Triggers = append(score.Triggers, "iterations>=80")
	} else if state.toolIterations >= 40 {
		score.IterationScore = 1
		score.Triggers = append(score.Triggers, "iterations>=40")
	}

	score.TotalScore = score.FailureScore + score.ProgressScore +
		score.BlockedScore + score.LoopScore + score.ContextScore + score.IterationScore

	// Determine level with hysteresis
	if score.BlockedScore >= 3 && score.TotalScore >= 4 {
		score.Level = ComplexityCritical
	} else if score.TotalScore >= 6 {
		score.Level = ComplexityCritical
	} else if score.TotalScore >= 4 || score.BlockedScore >= 3 {
		score.Level = ComplexityHigh
	} else if score.TotalScore >= 2 {
		score.Level = ComplexityModerate
	}

	return score
}

func ShouldTriggerDelegation(score ComplexityScore, state *runState, snapshot StateSnapshot) *DelegationTrigger {
	// Safety valve: if model asked user a question, don't auto-delegate
	if snapshot.AskedUserQuestion && score.Level < ComplexityCritical {
		return nil
	}

	switch score.Level {
	case ComplexityCritical:
		return &DelegationTrigger{
			Mode:         DelegationModeAutoExecute,
			Reason:       strings.Join(score.Triggers, ", "),
			AllowedTools: []string{"agent.list", "agent.run"},
			CooldownFor:  20,
		}
	case ComplexityHigh:
		return &DelegationTrigger{
			Mode:         DelegationModeToolGated,
			Reason:       strings.Join(score.Triggers, ", "),
			AllowedTools: []string{"agent.list", "agent.run"},
			CooldownFor:  15,
		}
	case ComplexityModerate:
		return &DelegationTrigger{
			Mode:         DelegationModePromptOnly,
			Reason:       strings.Join(score.Triggers, ", "),
			AllowedTools: nil, // no restriction
			CooldownFor:  10,
		}
	default:
		return nil
	}
}

func DetectUserQuestion(output string) bool {
	lower := strings.ToLower(output)
	questionIndicators := []string{
		"which option", "do you want", "should i", "would you prefer",
		"please confirm", "please provide", "what is your", "can you clarify",
		"i need more information", "unable to proceed without",
	}
	for _, indicator := range questionIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}
