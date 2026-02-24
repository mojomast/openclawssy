# Fix Summary: Bot Repetition and JSON Streaming

## Issues Fixed

### 1. Bot Repetition Issue
**Problem**: The model was repeating the same tool calls (e.g., creating "orchestrator" agent multiple times) because:
- Context window compaction removed critical "already done" context
- No mechanism to detect repeated agent creation attempts
- Streaming retries could duplicate content

**Solution Applied**:
- Added `repetitionPrevention` tracking in `run_state.go` to count repeated agent.create calls
- After 3 attempts to create the same agent, the system returns an error:
  ```
  "repetition detected: agent 'orchestrator' creation was already attempted 3 times"
  ```
- This breaks the loop and forces the model to check previous results

### 2. JSON Streaming Display
**Problem**: Tool arguments weren't visible during streaming - only summary/error/duration were sent

**Solution Applied**:
- Added `"arguments"` and `"output"` fields to the `tool_end` SSE event in `engine.go`
- Frontend already has logic to display these (`chat.js:892`)
- Now JSON tool calls are fully visible during streaming

## Using Subagents to Keep Context Fresh

The codebase already has excellent subagent infrastructure. Here's how to use it:

### When to Delegate to Subagents

1. **Tool iteration limit approaching** (120 iterations)
2. **Repeated no-progress loops** (6+ iterations)
3. **Context window approaching limit**
4. **Cross-domain tasks**
5. **Long-running analysis**

### Example: Using agent.run Tool

```go
// Delegate a file search task to a specialized subagent
input := tools.AgentRunInput{
    CallerAgentID: "orchestrator",
    TargetAgentID: "file-searcher",  // Use a specialized agent profile
    Message: `Search for all files matching pattern "config*.go".
              Return only the file paths, one per line.
              Do not perform any other actions.`,
    TaskID:        "search-phase-1",
    Source:        "subagent/orchestrator",
    ThinkingMode:  "off",  // Reduce token usage
}

result, err := subAgentRunner.ExecuteSubAgent(ctx, input)
// result.FinalText contains the structured output
```

### Best Practices

1. **Use Task IDs for Traceability**
   ```go
   TaskID: "phase-1-analysis"
   ```

2. **Configure Agent Profiles**
   ```json
   {
     "agents": {
       "profiles": {
         "file-searcher": {
           "enabled": true,
           "model": {"temperature": 0.1, "max_tokens": 2000}
         }
       }
     }
   }
   ```

3. **Pass Minimal Context**
   - Subagents get fresh context windows
   - Only pass task description + minimal required context
   - Use memory system for cross-agent state

4. **Source Tagging**
   ```go
   Source: "subagent/orchestrator"
   ```
   Creates clear audit trails in run artifacts

### Context Overflow Prevention Strategies

**Strategy A: Compaction Before Delegation**
```go
// Summarize accumulated tool results
// Extract key findings
// Pass only summary to subagent
```

**Strategy B: Parallel Subagents (Map-Reduce)**
```go
// Spawn multiple subagents with independent tasks
// Each gets fresh context window
// Results aggregated by parent
```

**Strategy C: Sequential Pipeline**
```go
// Subagent 1: Phase 1 → writes results to file/memory
// Subagent 2: Phase 2 → reads checkpoint, continues
// Each phase starts with fresh context
```

## Key Files Modified

- `internal/agent/run_state.go`: Added repetition detection
- `internal/runtime/engine.go`: Added arguments/output to tool_end events

## Key Infrastructure Files

- `internal/tools/agent_tools.go`: Subagent tool implementations
- `internal/runtime/engine.go`: Subagent runner, context management
- `docs/MEMORY_SYSTEM.md`: Memory system for context persistence
