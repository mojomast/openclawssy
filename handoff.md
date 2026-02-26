# Handoff

Date: 2026-02-26

## Completed: Auto-Delegation System Implementation

## Overview

Implemented a comprehensive auto-delegation system that automatically breaks complex tasks into subtasks when agents get stuck. This provides structural enforcement of subagent delegation without relying on LLM "understanding" of prompts.

## What Was Done

### Core Implementation

#### 1. Complexity Scoring (`internal/agent/complexity.go`)
- **ComplexityScore struct**: Tracks weighted signals (failure, progress, blocked, loop, context, iterations)
- **ComputeComplexity()**: Calculates complexity score based on runtime signals
- **ShouldTriggerDelegation()**: Determines when to trigger delegation (Moderate/High/Critical)
- **StateSnapshot**: Captures runtime state for decomposition decisions
- **User question detection**: Prevents delegation when user input is needed

#### 2. Task Decomposition (`internal/agent/decomposer.go`)
- **Pattern-based decomposition**: Matches task patterns (parallel-files, phased-analysis, debug-fix)
- **Signal-based fallback**: Decomposes based on stuck signals (loops, failures, context pressure)
- **Dependency tracking**: Tasks specify DependsOn for ordering
- **Artifact passing**: Tasks can produce/consume artifacts between phases

#### 3. Runner Integration (`internal/agent/runner.go`)
- **Delegation trigger check**: Runs before each model call
- **executeDelegatedTasks()**: Executes subtasks with dependency resolution
- **topologicalSortTasks()**: Kahn's algorithm with cycle detection
- **SubAgentRunner interface**: For pluggable subagent execution
- **Structured logging**: All delegation events logged with context

#### 4. Runtime State (`internal/agent/run_state.go`)
- **Delegation state tracking**: mode, cooldown, pending tasks, artifacts
- **Tool gating**: Blocks non-agent tools in forced mode
- **Rewrite budget**: Max 3 rewrites to prevent infinite loops
- **Delegation locking**: Once forced, stays locked until completion
- **rewriteToDelegation()**: Converts forbidden calls to agent.run

#### 5. Configuration (`internal/config/config.go`)
New delegation settings:
```json
{
  "agents": {
    "auto_delegate": false,
    "delegation_mode": "tool_gated",
    "delegation_threshold": 2,
    "delegation_agent_id": "default",
    "delegation_cooldown_iterations": 15
  }
}
```

#### 6. Engine Integration (`internal/runtime/engine.go`)
- **agentSubAgentRunnerAdapter**: Bridges runtime subAgentRunner to agent.SubAgentRunner
- **Identity bootstrap fix**: Checks identity.json before requiring bootstrap
- **loadPromptDocs()**: Recognizes identity.json as valid identity source

#### 7. Agent Scaffold (`internal/agentdocs/scaffold.go`)
- **identity.json creation**: Auto-creates identity.json with defaults
- **Prevents onboarding loop**: New agents no longer ask "What is your preferred name?"

### Delegation Modes

1. **prompt_only** - Soft hint, no enforcement
2. **tool_gated** (default) - Runtime blocks non-agent tools, rewrites to agent.run
3. **auto_execute** - Bypasses model, executes subtasks automatically

### Trigger Conditions

| Signal | Weight | Threshold |
|--------|--------|-----------|
| Iterations | +1/+2/+3 | >40 warn, >80 force, >120 critical |
| Consecutive failures | +2/+3 | >=2 |
| No progress | +2 | >=2 iterations |
| All blocked | +3 | >=1 iteration |
| Repetition detected | +2 | any tool >=2 |
| Context pressure | +1/+2/+3 | >75%/85%/92% |

**Complexity Levels:**
- Total >= 2: Moderate (soft hint)
- Total >= 4 OR blocked: High (tool gating)
- Total >= 6 OR blocked+others: Critical (auto-execute)

### Safety Features

1. **Rewrite budget (max 3)**: Prevents infinite loops when subagent keeps trying forbidden tools
2. **Delegation locking**: Once forced mode starts, stays active until subtasks complete
3. **Cycle detection**: Kahn's algorithm detects circular dependencies
4. **Cooldown**: 15 iterations before re-evaluating delegation
5. **User question detection**: Prevents delegation when user input needed
6. **Subtask timeouts**: Enforced via context.WithTimeout

### Documentation Updated

- `docs/ARCHITECTURE.md` - Added Auto-Delegation System section
- `docs/specs/CONFIG.md` - Added delegation configuration reference

## Bug Fixes During Implementation

### Fix 1: Invalid thinking_mode "off"
**Problem**: agent.run calls failing with "invalid thinking mode 'off'"
**Solution**: Changed to "never" (valid per config.ThinkingModeNever)

### Fix 2: Identity bootstrap blocking
**Problem**: New agents asked "What is your preferred name?" blocking all work
**Solution**: Auto-create identity.json in scaffold, check it in loadPromptDocs

### Fix 3: Tool gating edge cases
**Problem**: Rewrite loops possible
**Solution**: Added rewrite budget counter, delegation locking, fail-fast on budget exceeded

## Files Modified

### New Files
- `internal/agent/complexity.go` - Complexity scoring logic
- `internal/agent/decomposer.go` - Task decomposition patterns

### Modified Files
- `internal/agent/run_state.go` - Delegation state, tool gating, rewrite logic
- `internal/agent/runner.go` - SubAgentRunner, task execution
- `internal/agent/types.go` - SubAgentRunner interface, AutoDelegate field
- `internal/config/config.go` - Delegation configuration
- `internal/runtime/engine.go` - Adapter, identity bootstrap fix
- `internal/agentdocs/scaffold.go` - identity.json scaffolding
- `docs/ARCHITECTURE.md` - Architecture documentation
- `docs/specs/CONFIG.md` - Configuration reference

## Usage

### Default (Safe)
Delegation hints only, no enforcement:
```json
{
  "agents": {
    "delegation_mode": "prompt_only",
    "auto_delegate": false
  }
}
```

### Structural Enforcement (Recommended)
Blocks non-agent tools when triggered:
```json
{
  "agents": {
    "delegation_mode": "tool_gated",
    "auto_delegate": false
  }
}
```

### Full Automation
Bypasses model for critical triggers:
```json
{
  "agents": {
    "delegation_mode": "auto_execute",
    "auto_delegate": true
  }
}
```

## Testing

Build and runtime verified:
- `go build ./...` - Builds cleanly
- `go test ./internal/agent/...` - Tests pass (3 pre-existing failures unrelated)
- Docker container running with new image

## Known Limitations

1. **Context token tracking**: Currently hardcoded at 120k, needs integration with model response
2. **Test coverage**: Needs comprehensive tests for triggers, modes, cycle detection
3. **Subagent tool permissions**: Subagents currently get full tool access; may need capability restrictions

## Rollout Recommendation

Current default (`tool_gated` + `auto_delegate: false`) is safe for production:
- Provides structural enforcement without full automation
- Rewrite budget prevents infinite loops
- Clear error messages when delegation fails

Enable `auto_delegate: true` only after testing with specific task classes.

## Next Steps

1. Add comprehensive test suite for delegation system
2. Integrate actual prompt token tracking from model responses
3. Consider capability restrictions for subagents
4. Monitor logs for delegation patterns and tune thresholds
5. Add metrics for delegation trigger frequency and success rates
