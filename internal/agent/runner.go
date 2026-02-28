package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	repeatedNoProgressLoopCapTrigger = 3
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
	SubAgentRunner    SubAgentRunner
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

	completionClaims := []string{
		"i created",
		"created the file",
		"created `",
		"created ",
		"implemented",
		"implementation complete",
		"i updated",
		"i wrote",
		"saved to",
	}
	for _, phrase := range completionClaims {
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
		AllowedTools:  []string{},
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

func fallbackFromNoProgressToolResults(results []ToolCallResult) string {
	if len(results) == 0 {
		return "I stopped repeated tool calls because they were no longer making progress."
	}

	var b strings.Builder
	b.WriteString("I stopped repeated tool calls because they were no longer making progress. Here are the latest tool results:\n")
	b.WriteString(formatLatestToolResults(results))
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

func recoverFromEmptyFinal(toolResults []ToolCallResult) string {
	if len(toolResults) == 0 {
		return "I completed the run, but the model returned an empty final response. Please retry."
	}
	return "I completed tool execution, but the model returned an empty final response. Here are the latest tool results:\n" + formatLatestToolResults(toolResults)
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

func finalizeAfterFailureEscalation(userMessage string, records []ToolCallRecord) string {
	if prompt, ok := networkAllowlistPermissionPrompt(userMessage, records); ok {
		return prompt
	}

	var b strings.Builder
	b.WriteString("I stopped repeated failing tool attempts to avoid looping and finalized with the best available result.\n")
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
		b.WriteString("No concrete failing tool payloads were captured in the final attempts.")
		return b.String()
	}

	b.WriteString("Recent failing attempts:\n")
	for i := len(failing) - 1; i >= 0; i-- {
		rec := failing[i]
		attempt := rec.Request.Name
		args := truncateGuidanceText(strings.TrimSpace(string(rec.Request.Arguments)), 120)
		if args != "" {
			attempt += " " + args
		}
		b.WriteString(fmt.Sprintf("- %s\n", attempt))
	}
	b.WriteString("\nNext best step: adjust failing arguments/permissions, then rerun the task.")
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

// Delegation helper functions

func (r *Runner) executeDelegatedTasks(ctx context.Context, s *runState, tasks []DecomposedTask, input RunInput) error {
	if r.SubAgentRunner == nil {
		return errors.New("subagent runner not configured for auto-delegation")
	}

	slog.Debug("delegation: starting subtask execution", "task_count", len(tasks))

	ordered, err := topologicalSortTasks(tasks)
	if err != nil {
		slog.Debug("delegation: topological sort failed", "error", err)
		return err
	}

	for _, task := range ordered {
		slog.Debug("delegation: starting subtask", "task_id", task.TaskID, "agent_id", task.AgentID, "priority", task.Priority)
		// Check dependencies
		for _, dep := range task.DependsOn {
			if _, ok := s.completedSubtasks[dep]; !ok {
				return fmt.Errorf("dependency not satisfied: %s", dep)
			}
		}

		// Inject artifact context from dependencies
		message := task.Message
		if len(task.DependsOn) > 0 {
			message = injectArtifacts(message, task.DependsOn, s.delegationArtifacts)
		}

		// Execute subtask with the updated message
		modifiedTask := task
		modifiedTask.Message = message

		// Apply subtask timeout if specified
		taskCtx := ctx
		if task.TimeoutMS > 0 {
			var cancel context.CancelFunc
			taskCtx, cancel = context.WithTimeout(ctx, time.Duration(task.TimeoutMS)*time.Millisecond)
			defer cancel()
		}

		result, err := r.SubAgentRunner.ExecuteSubAgent(taskCtx, modifiedTask)

		if err != nil {
			slog.Debug("delegation: subtask failed", "task_id", task.TaskID, "error", err)
			s.completedSubtasks[task.TaskID] = "FAILED: " + err.Error()
			continue
		}

		slog.Debug("delegation: subtask completed", "task_id", task.TaskID, "success", result.Success, "output_len", len(result.FinalText))
		s.completedSubtasks[task.TaskID] = result.FinalText

		// Store produced artifacts
		for _, artifact := range task.Produces {
			s.delegationArtifacts[artifact] = summarizeForArtifact(result.FinalText)
			slog.Debug("delegation: artifact stored", "key", artifact, "task_id", task.TaskID)
		}
	}

	// Aggregate results into final output
	s.out.FinalText = aggregateSubtaskResults(s.completedSubtasks)
	slog.Debug("delegation: all subtasks completed", "output_len", len(s.out.FinalText))
	return nil
}

func topologicalSortTasks(tasks []DecomposedTask) ([]DecomposedTask, error) {
	// Build task map for O(1) lookup
	taskMap := make(map[string]DecomposedTask)
	for _, task := range tasks {
		taskMap[task.TaskID] = task
	}

	// Build adjacency list (task -> tasks that depend on it)
	dependents := make(map[string][]string)
	for _, task := range tasks {
		for _, dep := range task.DependsOn {
			dependents[dep] = append(dependents[dep], task.TaskID)
		}
	}

	// Kahn's algorithm for topological sort with cycle detection
	inDegree := make(map[string]int)
	for _, task := range tasks {
		inDegree[task.TaskID] = len(task.DependsOn)
	}

	// Start with tasks that have no dependencies
	queue := make([]string, 0)
	for _, task := range tasks {
		if inDegree[task.TaskID] == 0 {
			queue = append(queue, task.TaskID)
		}
	}

	// Sort queue by priority (lower priority first)
	sortByPriority := func(ids []string) {
		for i := 0; i < len(ids)-1; i++ {
			for j := i + 1; j < len(ids); j++ {
				if taskMap[ids[j]].Priority < taskMap[ids[i]].Priority {
					ids[i], ids[j] = ids[j], ids[i]
				}
			}
		}
	}
	sortByPriority(queue)

	result := make([]DecomposedTask, 0, len(tasks))
	visited := make(map[string]bool)

	for len(queue) > 0 {
		// Pop from front
		id := queue[0]
		queue = queue[1:]

		if visited[id] {
			continue
		}
		visited[id] = true

		task := taskMap[id]
		result = append(result, task)

		// Reduce in-degree for dependents
		for _, dependentID := range dependents[id] {
			inDegree[dependentID]--
			if inDegree[dependentID] == 0 {
				queue = append(queue, dependentID)
			}
		}

		// Re-sort queue by priority
		sortByPriority(queue)
	}

	// Check for cycles
	if len(result) != len(tasks) {
		// Find tasks that weren't visited (part of a cycle)
		unvisited := make([]string, 0)
		for _, task := range tasks {
			if !visited[task.TaskID] {
				unvisited = append(unvisited, task.TaskID)
			}
		}
		return nil, fmt.Errorf("dependency cycle detected involving tasks: %v", unvisited)
	}

	return result, nil
}

func injectArtifacts(message string, deps []string, artifacts map[string]string) string {
	var b strings.Builder
	b.WriteString(message)
	b.WriteString("\n\nContext from previous steps:\n")
	for _, dep := range deps {
		if artifact, ok := artifacts[dep]; ok {
			b.WriteString(fmt.Sprintf("- %s: %s\n", dep, artifact))
		}
	}
	return b.String()
}

func summarizeForArtifact(fullText string) string {
	lines := strings.Split(fullText, "\n")
	if len(lines) == 0 {
		return ""
	}
	if len(lines) <= 3 {
		return fullText
	}
	return strings.Join(lines[:3], "\n") + "..."
}

func aggregateSubtaskResults(results map[string]string) string {
	var b strings.Builder
	b.WriteString("## Delegated Task Results\n\n")
	for id, result := range results {
		b.WriteString(fmt.Sprintf("### %s\n", id))
		b.WriteString(result)
		b.WriteString("\n\n")
	}
	return b.String()
}

func buildForcedDelegationDirective(trigger *DelegationTrigger) string {
	subtaskJSON, _ := json.MarshalIndent(trigger.Subtasks, "", "  ")
	return fmt.Sprintf(`# FORCED_DELEGATION_MODE

CRITICAL: Task complexity has exceeded safe execution thresholds.
You are in FORCED mode. Only these tools are allowed: %s

TRIGGER REASON: %s

REQUIRED ACTIONS:
1. Use agent.list to discover available agents
2. Execute each subtask below using agent.run

SUBTASKS:
%s

Your output MUST be tool calls only. Plain text responses are not accepted.
Each agent.run call must include the task_id from the subtask definition.`,
		strings.Join(trigger.AllowedTools, ", "),
		trigger.Reason,
		string(subtaskJSON),
	)
}

func buildSoftDelegationHint(trigger *DelegationTrigger) string {
	return fmt.Sprintf(`# DELEGATION_RECOMMENDED

Context signals suggest this task would benefit from delegation:
Reason: %s

Consider using agent.run to delegate subtasks to specialized agents.
Each subagent gets a fresh context window, which can help with complex multi-step tasks.`,
		trigger.Reason,
	)
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
