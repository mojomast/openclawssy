package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/artifacts"
	"openclawssy/internal/audit"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/policy"
	"openclawssy/internal/sandbox"
	"openclawssy/internal/secrets"
	"openclawssy/internal/tools"
)

var promptDocOrder = []string{"SOUL.md", "RULES.md", "TOOLS.md", "SPECPLAN.md", "DEVPLAN.md", "HANDOFF.md"}

type Engine struct {
	rootDir      string
	workspaceDir string
	agentsDir    string
	runTracker   *RunTracker
	chatStore    *chatstore.Store

	runLimitMu  sync.Mutex
	runLimitCap int
	runSlots    chan struct{}
}

type RunResult struct {
	RunID            string
	FinalText        string
	ArtifactPath     string
	DurationMS       int64
	ToolCalls        int
	Provider         string
	Model            string
	Trace            map[string]any
	ParseDiagnostics *ParseDiagnostics
}

type ParseDiagnostics struct {
	Rejected []ParseDiagnosticEntry `json:"rejected,omitempty"`
}

type ParseDiagnosticEntry struct {
	Snippet string `json:"snippet,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type ExecuteInput struct {
	AgentID      string
	Message      string
	Source       string
	SessionID    string
	ThinkingMode string
}

const (
	maxSessionContextChars         = 12000
	maxSessionMessageChars         = 1400
	maxSessionToolSummaryChars     = 220
	maxSessionToolOutputChars      = 1000
	maxSessionToolErrorChars       = 320
	maxSessionContextMessageCap    = 200
	maxParseDiagnosticSnippetChars = 260
	maxParseDiagnosticReasonChars  = 180
	defaultRunTimeout              = 20 * time.Minute
	maxRunErrorUserMessageChars    = 320
)

type RunLimitError struct {
	Limit int
}

func (e *RunLimitError) Error() string {
	if e == nil || e.Limit <= 0 {
		return "engine.max_concurrent_runs exceeded"
	}
	return fmt.Sprintf("engine.max_concurrent_runs exceeded (%d)", e.Limit)
}

func NewEngine(rootDir string) (*Engine, error) {
	if rootDir == "" {
		return nil, errors.New("runtime: root dir is required")
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("runtime: resolve root: %w", err)
	}

	agentsDir := filepath.Join(absRoot, ".openclawssy", "agents")
	// Ensure agentsDir exists so NewStore succeeds even on fresh init
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return nil, fmt.Errorf("runtime: create agents dir: %w", err)
	}

	chatStore, err := chatstore.NewStore(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("runtime: init chat store: %w", err)
	}

	return &Engine{
		rootDir:      absRoot,
		workspaceDir: filepath.Join(absRoot, "workspace"),
		agentsDir:    agentsDir,
		runTracker:   NewRunTracker(),
		chatStore:    chatStore,
	}, nil
}

func (e *Engine) Init(agentID string, force bool) error {
	if err := os.MkdirAll(e.workspaceDir, 0o755); err != nil {
		return fmt.Errorf("runtime: create workspace: %w", err)
	}

	agentRoot := filepath.Join(e.agentsDir, agentID)
	if err := os.MkdirAll(filepath.Join(agentRoot, "memory"), 0o755); err != nil {
		return fmt.Errorf("runtime: create memory dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(agentRoot, "audit"), 0o755); err != nil {
		return fmt.Errorf("runtime: create audit dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(agentRoot, "runs"), 0o755); err != nil {
		return fmt.Errorf("runtime: create runs dir: %w", err)
	}

	files := map[string]string{
		"SOUL.md":     "# SOUL\n\nMission and behavior contract for this agent.\n",
		"RULES.md":    "# RULES\n\n- Follow workspace-only write policy.\n- Respect tool capabilities.\n",
		"TOOLS.md":    "# TOOLS\n\nEnabled core tools: fs.read, fs.list, fs.write, fs.append, fs.delete, fs.move, fs.edit, code.search, config.get, config.set, secrets.get, secrets.set, secrets.list, skill.list, skill.read, scheduler.list, scheduler.add, scheduler.remove, scheduler.pause, scheduler.resume, session.list, session.close, agent.list, agent.create, agent.switch, policy.list, policy.grant, policy.revoke, run.list, run.get, run.cancel, metrics.get, http.request, time.now.\n",
		"SPECPLAN.md": "# SPECPLAN\n\nDescribe specs and acceptance requirements before coding.\n",
		"DEVPLAN.md":  "# DEVPLAN\n\n- [ ] Implement task\n- [ ] Add tests\n- [ ] Update handoff\n",
		"HANDOFF.md":  "# HANDOFF\n\nStatus: initialized\n\nNext:\n- Define first run objective.\n",
	}
	for name, body := range files {
		path := filepath.Join(agentRoot, name)
		if !force {
			if _, err := os.Stat(path); err == nil {
				continue
			}
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return fmt.Errorf("runtime: write %s: %w", name, err)
		}
	}

	cfgPath := filepath.Join(e.rootDir, ".openclawssy", "config.json")
	if force || !fileExists(cfgPath) {
		cfg := config.Default()
		cfg.Workspace.Root = e.workspaceDir
		if err := config.Save(cfgPath, cfg); err != nil {
			return fmt.Errorf("runtime: write config: %w", err)
		}
	}

	return nil
}

func (e *Engine) Execute(ctx context.Context, agentID, message string) (RunResult, error) {
	return e.ExecuteWithInput(ctx, ExecuteInput{AgentID: agentID, Message: message})
}

func (e *Engine) ExecuteWithInput(ctx context.Context, in ExecuteInput) (RunResult, error) {
	agentID := strings.TrimSpace(in.AgentID)
	message := strings.TrimSpace(in.Message)
	source := strings.TrimSpace(in.Source)
	sessionID := strings.TrimSpace(in.SessionID)

	if agentID == "" {
		return RunResult{}, errors.New("runtime: agent id is required")
	}
	if message == "" {
		return RunResult{}, errors.New("runtime: message is required")
	}

	if err := os.MkdirAll(e.workspaceDir, 0o755); err != nil {
		return RunResult{}, fmt.Errorf("runtime: create workspace dir: %w", err)
	}

	cfg, err := config.LoadOrDefault(filepath.Join(e.rootDir, ".openclawssy", "config.json"))
	if err != nil {
		return RunResult{}, fmt.Errorf("runtime: load config: %w", err)
	}
	releaseSlot, err := e.acquireRunSlot(cfg.Engine.MaxConcurrentRuns)
	if err != nil {
		return RunResult{}, err
	}
	defer releaseSlot()
	thinkingMode := config.NormalizeThinkingMode(cfg.Output.ThinkingMode)
	if strings.TrimSpace(in.ThinkingMode) != "" {
		if !config.IsValidThinkingMode(in.ThinkingMode) {
			return RunResult{}, fmt.Errorf("runtime: invalid thinking mode %q", in.ThinkingMode)
		}
		thinkingMode = config.NormalizeThinkingMode(in.ThinkingMode)
	}

	runID := fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())

	// Create cancellable context for this run and track it
	runCtxBase, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	runCtx := runCtxBase
	runTimeout := resolveRunTimeout(cfg.Engine)
	cancelTimeout := func() {}
	if runTimeout > 0 {
		runCtx, cancelTimeout = context.WithTimeout(runCtxBase, runTimeout)
	}
	defer cancelTimeout()
	if e.runTracker != nil {
		e.runTracker.Track(runID, cancelRun)
		defer e.runTracker.Remove(runID)
	}

	docs, err := e.loadPromptDocs(agentID)
	if err != nil {
		return RunResult{}, err
	}

	auditPath := filepath.Join(e.agentsDir, agentID, "audit", "events.jsonl")
	aud, err := audit.NewLogger(auditPath, policy.RedactValue)
	if err != nil {
		return RunResult{}, fmt.Errorf("runtime: init audit logger: %w", err)
	}
	defer func() {
		_ = aud.Close()
	}()

	startEvent := map[string]any{"run_id": runID, "agent_id": agentID, "message": message}
	if source != "" {
		startEvent["source"] = source
	}
	if sessionID != "" {
		startEvent["session_id"] = sessionID
	}
	_ = aud.LogEvent(runCtx, audit.EventRunStart, startEvent)

	allowedTools := e.allowedTools(cfg)
	isSchedulerRun := strings.HasPrefix(source, "scheduler")
	runMessage := message
	maxToolIterations := agent.DefaultToolIterationCap
	if isSchedulerRun && !strings.HasPrefix(strings.TrimSpace(message), "/tool ") {
		allowedTools = []string{}
		maxToolIterations = 1
		runMessage = "Scheduled proactive delivery. Respond with exactly one concise assistant message that delivers this content to the user. Do not call tools. Do not ask follow-up questions. Content: " + message
	}
	effectiveCaps := e.effectiveCapabilities(agentID, allowedTools)
	traceCollector := newRunTraceCollector(runID, sessionID, source, message)
	runCtx = withRunTraceCollector(runCtx, traceCollector)
	enforcer := policy.NewEnforcer(e.workspaceDir, map[string][]string{agentID: effectiveCaps})
	registry := tools.NewRegistry(enforcer, aud)
	if err := tools.RegisterCoreWithOptions(registry, tools.CoreOptions{
		EnableShellExec: cfg.Shell.EnableExec && cfg.Sandbox.Active && strings.ToLower(cfg.Sandbox.Provider) != "none",
		ConfigPath:      filepath.Join(e.rootDir, ".openclawssy", "config.json"),
		AgentsPath:      e.agentsDir,
		SchedulerPath:   filepath.Join(e.rootDir, ".openclawssy", "scheduler", "jobs.json"),
		ChatstorePath:   e.agentsDir,
		PolicyPath:      filepath.Join(e.rootDir, ".openclawssy", "policy", "capabilities.json"),
		DefaultGrants:   allowedTools,
		RunsPath:        filepath.Join(e.rootDir, ".openclawssy", "runs.json"),
		RunTracker:      e.runTracker,
	}); err != nil {
		return RunResult{}, fmt.Errorf("runtime: register core tools: %w", err)
	}
	registry.SetShellAllowedCommands(cfg.Shell.AllowedCommands)

	var provider sandbox.Provider
	if cfg.Sandbox.Active {
		provider, err = sandbox.NewProvider(cfg.Sandbox.Provider, e.workspaceDir)
		if err != nil {
			return RunResult{}, fmt.Errorf("runtime: create sandbox provider: %w", err)
		}
		if err := provider.Start(runCtx); err != nil {
			return RunResult{}, fmt.Errorf("runtime: start sandbox provider: %w", err)
		}
		defer provider.Stop()
		if cfg.Shell.EnableExec && sandbox.ShellExecAllowed(provider) {
			registry.SetShellExecutor(&sandboxShellExecutor{provider: provider})
		}
	}

	secretStore, _ := secrets.NewStore(cfg)
	lookup := func(name string) (string, bool, error) {
		if secretStore == nil {
			return "", false, nil
		}
		return secretStore.Get(name)
	}

	model, err := NewProviderModel(cfg, lookup)
	if err != nil {
		return RunResult{}, err
	}

	runner := agent.Runner{
		Model:             model,
		ToolExecutor:      &RegistryExecutor{Registry: registry, AgentID: agentID, Workspace: e.workspaceDir},
		MaxToolIterations: agent.DefaultToolIterationCap,
	}

	modelMessages := []agent.ChatMessage{{Role: "user", Content: runMessage}}
	var conversationStore *chatstore.Store
	if sessionID != "" {
		conversationStore = e.chatStore
		sessionMeta, sessionErr := conversationStore.GetSession(sessionID)
		if sessionErr != nil {
			return RunResult{}, fmt.Errorf("runtime: load session: %w", sessionErr)
		}
		if sessionMeta.IsClosed() {
			return RunResult{}, fmt.Errorf("runtime: session is closed: %s", sessionID)
		}
		history, historyErr := e.loadSessionMessages(sessionID, 200)
		if historyErr != nil {
			return RunResult{}, historyErr
		}
		if len(history) > 0 {
			modelMessages = history
		}
	}
	if len(modelMessages) == 0 || !hasTrailingUserMessage(modelMessages, runMessage) {
		modelMessages = append(modelMessages, agent.ChatMessage{Role: "user", Content: runMessage})
	}

	appendToolsAfterRun := true
	onToolCall := func(rec agent.ToolCallRecord) error { return nil }
	if conversationStore != nil {
		appendToolsAfterRun = false
		onToolCall = func(rec agent.ToolCallRecord) error {
			return appendToolCallMessage(conversationStore, sessionID, runID, rec)
		}
	}

	start := time.Now().UTC()
	out, runErr := runner.Run(runCtx, agent.RunInput{
		AgentID:           agentID,
		RunID:             runID,
		Message:           runMessage,
		Messages:          modelMessages,
		ArtifactDocs:      docs,
		PerFileByteLimit:  16 * 1024,
		MaxToolIterations: maxToolIterations,
		ToolTimeoutMS:     int(agent.DefaultToolTimeout / time.Millisecond),
		AllowedTools:      allowedTools,
		OnToolCall:        onToolCall,
	})

	artifactPath := ""
	persistedThinking, thinkingPresent := sanitizedPersistedThinking(out.Thinking, out.ThinkingPresent, cfg.Output.MaxThinkingChars)
	includeThinking := shouldIncludeThinking(thinkingMode, runErr != nil, out.ToolParseFailure, thinkingPresent)
	if runErr == nil {
		durationMS := time.Since(start).Milliseconds()
		toolCount := len(out.ToolCalls)
		toolLines := make([]string, 0, len(out.ToolCalls))
		for _, rec := range out.ToolCalls {
			b, mErr := json.Marshal(rec)
			if mErr != nil {
				runErr = mErr
				break
			}
			toolLines = append(toolLines, string(b))
		}
		if runErr == nil {
			finalOutput := policy.RedactString(out.FinalText)
			if includeThinking {
				finalOutput = formatFinalOutputWithThinking(finalOutput, persistedThinking)
			}
			artifactPath, err = artifacts.WriteRunBundleV1(e.rootDir, agentID, runID, artifacts.BundleV1Input{
				Input:     map[string]any{"agent_id": agentID, "message": message},
				PromptMD:  out.Prompt,
				ToolCalls: toolLines,
				OutputMD:  finalOutput,
				Meta: map[string]any{
					"started_at":       out.StartedAt,
					"completed_at":     out.CompletedAt,
					"duration_ms":      durationMS,
					"tool_call_count":  toolCount,
					"provider":         model.ProviderName(),
					"model":            model.ModelName(),
					"thinking":         persistedThinking,
					"thinking_present": thinkingPresent,
				},
				MirrorJSON: true,
			})
			out.FinalText = finalOutput
		}
		if err != nil {
			runErr = err
		}
		if runErr == nil && sessionID != "" {
			if err := e.appendRunConversation(sessionID, runID, out, appendToolsAfterRun); err != nil {
				runErr = err
			}
		}
	}
	logToolCallbackFailures(runCtx, aud, runID, agentID, out.ToolCalls, source, sessionID)
	if runErr != nil && sessionID != "" {
		if err := e.appendRunFailureConversation(sessionID, runID, out, appendToolsAfterRun, runErr); err != nil {
			log.Printf("runtime: failed to append run failure conversation (run=%s session=%s): %v", runID, sessionID, err)
		}
	}

	fields := map[string]any{"run_id": runID, "agent_id": agentID}
	if source != "" {
		fields["source"] = source
	}
	if sessionID != "" {
		fields["session_id"] = sessionID
	}
	if runErr != nil {
		fields["error"] = runErr.Error()
	} else {
		fields["artifact_path"] = artifactPath
	}
	fields["thinking"] = persistedThinking
	fields["thinking_present"] = thinkingPresent
	_ = aud.LogEvent(runCtx, audit.EventRunEnd, fields)

	traceCollector.RecordThinking(persistedThinking, thinkingPresent)
	traceCollector.RecordToolExecution(out.ToolCalls)
	traceSnapshot := traceCollector.Snapshot()
	parseDiagnostics := buildRunParseDiagnostics(traceSnapshot, thinkingMode == config.ThinkingModeAlways || out.ToolParseFailure)

	if runErr != nil {
		if includeThinking {
			runErr = fmt.Errorf("%w\n\nThinking:\n%s", runErr, persistedThinking)
		}
		return RunResult{
			RunID:            runID,
			ArtifactPath:     artifactPath,
			DurationMS:       time.Since(start).Milliseconds(),
			ToolCalls:        len(out.ToolCalls),
			Provider:         model.ProviderName(),
			Model:            model.ModelName(),
			Trace:            traceSnapshot,
			ParseDiagnostics: parseDiagnostics,
		}, runErr
	}

	return RunResult{
		RunID:            runID,
		FinalText:        out.FinalText,
		ArtifactPath:     artifactPath,
		DurationMS:       time.Since(start).Milliseconds(),
		ToolCalls:        len(out.ToolCalls),
		Provider:         model.ProviderName(),
		Model:            model.ModelName(),
		Trace:            traceSnapshot,
		ParseDiagnostics: parseDiagnostics,
	}, nil
}

func shouldIncludeThinking(mode string, runError bool, parseFailure bool, thinkingPresent bool) bool {
	if !thinkingPresent {
		return false
	}
	switch config.NormalizeThinkingMode(mode) {
	case config.ThinkingModeAlways:
		return true
	case config.ThinkingModeOnError:
		return runError || parseFailure
	default:
		return false
	}
}

func sanitizedPersistedThinking(thinking string, present bool, maxChars int) (string, bool) {
	present = present || strings.TrimSpace(thinking) != ""
	if !present {
		return "", false
	}
	redacted := policy.RedactString(strings.TrimSpace(thinking))
	if maxChars <= 0 {
		maxChars = 4000
	}
	if len([]rune(redacted)) > maxChars {
		redacted = strings.TrimSpace(truncateRunes(redacted, maxChars)) + "..."
	}
	return redacted, true
}

func resolveRunTimeout(engineCfg config.EngineConfig) time.Duration {
	defaultMS := engineCfg.DefaultRunTimeoutMS
	maxMS := engineCfg.MaxRunTimeoutMS

	if maxMS > 0 && defaultMS > maxMS {
		defaultMS = maxMS
	}
	if defaultMS <= 0 {
		if maxMS > 0 {
			defaultMS = maxMS
		} else {
			defaultMS = int(defaultRunTimeout / time.Millisecond)
		}
	}
	if maxMS > 0 && defaultMS > maxMS {
		defaultMS = maxMS
	}
	if defaultMS <= 0 {
		return 0
	}

	return time.Duration(defaultMS) * time.Millisecond
}

func (e *Engine) appendRunFailureConversation(sessionID, runID string, out agent.RunOutput, includeToolMessages bool, runErr error) error {
	failureMessage := policy.RedactString(strings.TrimSpace(out.FinalText))
	if failureMessage == "" {
		failureMessage = formatRunFailureUserMessage(runErr)
	} else {
		errSummary := summarizeRunErrorForUser(runErr)
		if errSummary != "" {
			failureMessage += "\n\nI also hit an error and need your attention:\n" + errSummary
		}
	}
	if strings.TrimSpace(failureMessage) == "" {
		return nil
	}

	out.FinalText = failureMessage
	return e.appendRunConversation(sessionID, runID, out, includeToolMessages)
}

func summarizeRunErrorForUser(err error) string {
	if err == nil {
		return ""
	}
	text := policy.RedactString(strings.TrimSpace(err.Error()))
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > maxRunErrorUserMessageChars {
		if maxRunErrorUserMessageChars <= 3 {
			return text[:maxRunErrorUserMessageChars]
		}
		return strings.TrimSpace(text[:maxRunErrorUserMessageChars-3]) + "..."
	}
	return text
}

func formatRunFailureUserMessage(err error) string {
	summary := summarizeRunErrorForUser(err)
	lower := strings.ToLower(summary)
	isTimeout := errors.Is(err, context.DeadlineExceeded) || strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded")

	if isTimeout {
		if summary == "" {
			summary = "run timed out"
		}
		return "I ran into a timeout while working on that and need your attention.\n\nPlease reply with one of:\n- \"retry\" to try again\n- a narrower request\n- config/secrets changes if needed\n\nDetails: " + summary
	}

	if summary == "" {
		summary = "run failed"
	}
	return "I hit an error and need your attention before I can continue.\n\nDetails: " + summary + "\n\nReply \"retry\" if you want me to try again."
}

func (e *Engine) acquireRunSlot(limit int) (func(), error) {
	if limit <= 0 {
		limit = 1
	}
	e.runLimitMu.Lock()
	if e.runSlots == nil {
		e.runSlots = make(chan struct{}, limit)
		e.runLimitCap = limit
	} else if e.runLimitCap != limit && len(e.runSlots) == 0 {
		e.runSlots = make(chan struct{}, limit)
		e.runLimitCap = limit
	}
	slots := e.runSlots
	activeCap := e.runLimitCap
	e.runLimitMu.Unlock()

	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	default:
		log.Printf("runtime: rejected run due to max concurrent runs limit (%d)", activeCap)
		return nil, &RunLimitError{Limit: activeCap}
	}
}

func buildRunParseDiagnostics(trace map[string]any, include bool) *ParseDiagnostics {
	if !include || len(trace) == 0 {
		return nil
	}
	raw, ok := trace["extracted_tool_calls"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	rejected := make([]ParseDiagnosticEntry, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		accepted, _ := entry["accepted"].(bool)
		if accepted {
			continue
		}
		snippet := policy.RedactString(strings.TrimSpace(fmt.Sprintf("%v", entry["raw_snippet"])))
		if snippet == "<nil>" {
			snippet = ""
		}
		reason := policy.RedactString(strings.TrimSpace(fmt.Sprintf("%v", entry["reason"])))
		if reason == "<nil>" {
			reason = ""
		}
		snippet = truncateRunes(snippet, maxParseDiagnosticSnippetChars)
		reason = truncateRunes(reason, maxParseDiagnosticReasonChars)
		if snippet == "" && reason == "" {
			continue
		}
		rejected = append(rejected, ParseDiagnosticEntry{Snippet: snippet, Reason: reason})
	}
	if len(rejected) == 0 {
		return nil
	}
	return &ParseDiagnostics{Rejected: rejected}
}

func formatFinalOutputWithThinking(finalText, thinking string) string {
	if strings.TrimSpace(thinking) == "" {
		return strings.TrimSpace(finalText)
	}
	visible := strings.TrimSpace(finalText)
	if visible == "" {
		return "Thinking:\n" + thinking
	}
	return visible + "\n\nThinking:\n" + thinking
}

func logToolCallbackFailures(ctx context.Context, aud *audit.Logger, runID, agentID string, records []agent.ToolCallRecord, source, sessionID string) {
	if aud == nil || len(records) == 0 {
		return
	}
	for _, rec := range records {
		callbackErr := strings.TrimSpace(rec.CallbackErr)
		if callbackErr == "" {
			continue
		}
		fields := map[string]any{
			"run_id":    runID,
			"agent_id":  agentID,
			"tool":      strings.TrimSpace(rec.Request.Name),
			"tool_call": strings.TrimSpace(rec.Request.ID),
			"error":     callbackErr,
		}
		if source != "" {
			fields["source"] = source
		}
		if sessionID != "" {
			fields["session_id"] = sessionID
		}
		_ = aud.LogEvent(ctx, audit.EventToolCallbackError, fields)
	}
}

func (e *Engine) loadSessionMessages(sessionID string, limit int) ([]agent.ChatMessage, error) {
	if e.chatStore == nil {
		return nil, errors.New("runtime: chat store not initialized")
	}
	history, err := e.chatStore.ReadRecentMessages(sessionID, chatstore.ClampHistoryCount(limit, maxSessionContextMessageCap))
	if err != nil {
		return nil, fmt.Errorf("runtime: read chat history: %w", err)
	}
	out := make([]agent.ChatMessage, 0, len(history))
	for _, msg := range history {
		role := normalizeSessionRole(msg.Role)
		content := buildSessionMessageContent(role, msg)
		if content == "" {
			continue
		}
		out = append(out, agent.ChatMessage{
			Role:       role,
			Content:    content,
			Name:       strings.TrimSpace(msg.ToolName),
			ToolCallID: strings.TrimSpace(msg.ToolCallID),
			TS:         msg.TS,
		})
	}
	return clampSessionContext(out, chatstore.ClampHistoryCount(limit, maxSessionContextMessageCap), maxSessionContextChars), nil
}

func normalizeSessionRole(role string) string {
	clean := strings.ToLower(strings.TrimSpace(role))
	switch clean {
	case "system", "assistant", "tool", "user":
		return clean
	default:
		return "user"
	}
}

func buildSessionMessageContent(role string, msg chatstore.Message) string {
	if role == "tool" {
		return buildToolContextMessage(msg)
	}
	return truncateRunes(strings.TrimSpace(msg.Content), maxSessionMessageChars)
}

func buildToolContextMessage(msg chatstore.Message) string {
	toolName := strings.TrimSpace(msg.ToolName)
	toolCallID := strings.TrimSpace(msg.ToolCallID)
	summary := ""
	output := ""
	errText := ""

	payload := map[string]any{}
	if err := json.Unmarshal([]byte(msg.Content), &payload); err == nil {
		if v := fieldString(payload, "tool"); v != "" {
			toolName = v
		}
		if v := fieldString(payload, "id"); v != "" {
			toolCallID = v
		}
		summary = fieldString(payload, "summary")
		output = fieldString(payload, "output")
		errText = fieldString(payload, "error")
	} else {
		output = strings.TrimSpace(msg.Content)
	}

	summary = truncateRunes(summary, maxSessionToolSummaryChars)
	errText = truncateRunes(errText, maxSessionToolErrorChars)
	output = truncateRunes(output, maxSessionToolOutputChars)

	if summary == "" {
		summary = truncateRunes(summarizeToolExecution(toolName, output, errText), maxSessionToolSummaryChars)
	}

	header := "tool result"
	if toolName != "" {
		header = "tool " + toolName + " result"
	}
	if toolCallID != "" {
		header += " (" + toolCallID + ")"
	}

	lines := []string{header}
	if summary != "" {
		lines = append(lines, "summary: "+summary)
	}
	if errText != "" {
		lines = append(lines, "error: "+errText)
	}
	if output != "" {
		lines = append(lines, "output: "+output)
	}

	return truncateRunes(strings.Join(lines, "\n"), maxSessionMessageChars)
}

func fieldString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func clampSessionContext(messages []agent.ChatMessage, maxMessages, maxChars int) []agent.ChatMessage {
	if len(messages) == 0 {
		return nil
	}
	if maxMessages <= 0 {
		maxMessages = maxSessionContextMessageCap
	}
	out := append([]agent.ChatMessage(nil), messages...)
	if len(out) > maxMessages {
		out = out[len(out)-maxMessages:]
	}
	if maxChars <= 0 {
		return out
	}
	for len(out) > 1 && totalSessionContextChars(out) > maxChars {
		out = out[1:]
	}
	if len(out) == 1 && totalSessionContextChars(out) > maxChars {
		out[0].Content = truncateRunes(out[0].Content, maxChars)
	}
	return out
}

func totalSessionContextChars(messages []agent.ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += len([]rune(strings.TrimSpace(msg.Content)))
	}
	return total
}

func splitToolError(errText string) (code string, message string) {
	raw := strings.TrimSpace(errText)
	if raw == "" {
		return "", ""
	}
	message = raw
	head, tail, ok := strings.Cut(raw, ":")
	if !ok {
		return "", message
	}
	candidate := strings.TrimSpace(head)
	if idx := strings.Index(candidate, " "); idx >= 0 {
		candidate = strings.TrimSpace(candidate[:idx])
	}
	if candidate == "" {
		return "", message
	}
	for _, r := range candidate {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return "", message
	}
	trimmedTail := strings.TrimSpace(tail)
	if trimmedTail != "" {
		message = trimmedTail
	}
	return candidate, message
}

func hasTrailingUserMessage(messages []agent.ChatMessage, currentMessage string) bool {
	if len(messages) == 0 {
		return false
	}
	last := messages[len(messages)-1]
	role := strings.ToLower(strings.TrimSpace(last.Role))
	if role != "" && role != "user" {
		return false
	}
	return strings.TrimSpace(last.Content) == strings.TrimSpace(currentMessage)
}

func (e *Engine) appendRunConversation(sessionID, runID string, out agent.RunOutput, includeToolMessages bool) error {
	store := e.chatStore
	if store == nil {
		return errors.New("runtime: chat store not initialized")
	}
	if includeToolMessages {
		for _, rec := range out.ToolCalls {
			if err := appendToolCallMessage(store, sessionID, runID, rec); err != nil {
				return err
			}
		}
	}
	if strings.TrimSpace(out.FinalText) != "" {
		ts := out.CompletedAt
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		if err := store.AppendMessage(sessionID, chatstore.Message{Role: "assistant", Content: out.FinalText, TS: ts, RunID: runID}); err != nil {
			return fmt.Errorf("runtime: append assistant message: %w", err)
		}
	}
	return nil
}

func appendToolCallMessage(store *chatstore.Store, sessionID, runID string, rec agent.ToolCallRecord) error {
	if store == nil {
		return fmt.Errorf("runtime: chat store is not configured")
	}
	summary := summarizeToolExecution(rec.Request.Name, rec.Result.Output, rec.Result.Error)
	errCode, errMessage := splitToolError(rec.Result.Error)
	payload, marshalErr := json.Marshal(map[string]any{
		"tool":          rec.Request.Name,
		"id":            rec.Request.ID,
		"summary":       summary,
		"output":        rec.Result.Output,
		"error":         rec.Result.Error,
		"error_code":    errCode,
		"error_message": errMessage,
	})
	if marshalErr != nil {
		return fmt.Errorf("runtime: marshal tool result: %w", marshalErr)
	}
	ts := rec.CompletedAt
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	if err := store.AppendMessage(sessionID, chatstore.Message{
		Role:       "tool",
		Content:    string(payload),
		TS:         ts,
		RunID:      runID,
		ToolCallID: rec.Request.ID,
		ToolName:   rec.Request.Name,
	}); err != nil {
		return fmt.Errorf("runtime: append tool message: %w", err)
	}
	return nil
}

func (e *Engine) loadPromptDocs(agentID string) ([]agent.ArtifactDoc, error) {
	agentRoot := filepath.Join(e.agentsDir, agentID)
	docs := make([]agent.ArtifactDoc, 0, len(promptDocOrder)+2)
	for _, name := range promptDocOrder {
		path := filepath.Join(agentRoot, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("runtime: read %s: %w", name, err)
		}
		docs = append(docs, agent.ArtifactDoc{Name: name, Content: string(data)})
	}
	docs = append(docs, agent.ArtifactDoc{Name: "RUNTIME_CONTEXT.md", Content: runtimeContextDoc(e.workspaceDir)})
	docs = append(docs, agent.ArtifactDoc{Name: "TOOL_CALLING_BEST_PRACTICES.md", Content: toolCallingBestPracticesDocWithAgentTools()})
	return docs, nil
}

func runtimeContextDoc(workspaceDir string) string {
	doc := fmt.Sprintf(
		"# RUNTIME_CONTEXT\n\n- Workspace root: %s\n- File tools (fs.read/fs.list/fs.write/fs.append/fs.delete/fs.move/fs.edit/code.search) can only access paths inside workspace root.\n- Config tools (config.get/config.set) can only read redacted config and mutate an allowlisted safe field subset.\n- Secret tools (secrets.get/secrets.set/secrets.list) use encrypted secret storage; secret values are never written to audit fields in plaintext.\n- Scheduler tools (scheduler.list/add/remove/pause/resume) persist jobs in .openclawssy/scheduler/jobs.json and enforce scheduler validation rules.\n- Session tools (session.list/session.close) manage chat sessions persisted under .openclawssy/agents/*/memory/chats and closed sessions are not reused for routing.\n- Run tools (run.list/run.get) retrieve run traces and summaries from the run store; use filtering and pagination for large result sets.\n- Network tool (http.request/net.fetch) is enabled only when network.enabled=true, enforces scheme checks, allowlisted domains, redirect rechecks, and localhost policy.\n- Paths outside workspace (for example /home, ~, ..) are blocked by policy.\n- If shell.exec is enabled by policy, run shell commands through `bash -lc` in shell.exec args.\n- If `bash` is unavailable in PATH, runtime retries `/bin/bash`, then `/usr/bin/bash`, then `sh`.\n- Shell commands can use environment tools available in the runtime image (for example: python3/pip, node/npm, git, curl/wget, jq, nmap, dig/nslookup, ip/ss/netstat, traceroute, tcpdump).\n- Some network commands may require container capabilities or host mounts (for example docker socket, NET_RAW, NET_ADMIN). If unavailable, report the exact error and continue with the best available diagnostic command.\n- Paths outside workspace (for example /home, ~, ..) are blocked by policy even when using shell.exec.\n- If the user asks about files in home directory, explain this limitation and offer to list the workspace instead.\n- Keep responses task-focused; do not mention HANDOFF/SPECPLAN/DEVPLAN unless the user explicitly asks about them.\n",
		workspaceDir,
	)
	doc = strings.Replace(doc,
		"- Run tools (run.list/run.get) retrieve run traces and summaries from the run store; use filtering and pagination for large result sets.",
		"- Agent tools (agent.list/agent.create/agent.switch) manage per-agent control-plane directories and update default chat/discord routing in config.\n- Run tools (run.list/run.get/run.cancel) retrieve run traces and summaries from the run store and can cancel tracked in-flight runs.",
		1,
	)
	doc = strings.Replace(doc,
		"- Run tools (run.list/run.get/run.cancel) retrieve run traces and summaries from the run store and can cancel tracked in-flight runs.",
		"- Run tools (run.list/run.get/run.cancel) retrieve run traces and summaries from the run store and can cancel tracked in-flight runs.\n- Policy tools (policy.list/policy.grant/policy.revoke) manage per-agent capability grants and require policy.admin capability.\n- Metrics tool (metrics.get) aggregates run and per-tool duration/error metrics from run traces.",
		1,
	)
	doc = strings.Replace(doc,
		"- Secret tools (secrets.get/secrets.set/secrets.list) use encrypted secret storage; secret values are never written to audit fields in plaintext.",
		"- Secret tools (secrets.get/secrets.set/secrets.list) use encrypted secret storage; secret values are never written to audit fields in plaintext.\n- Skill tools (skill.list/skill.read) discover workspace skills under skills/ and report required secret keys with missing-secret diagnostics.",
		1,
	)
	return doc
}

func toolCallingBestPracticesDoc() string {
	return "# TOOL_CALLING_BEST_PRACTICES\n\n- Use only registered tool names: fs.read, fs.list, fs.write, fs.append, fs.delete, fs.move, fs.edit, code.search, config.get, config.set, secrets.get, secrets.set, secrets.list, scheduler.list, scheduler.add, scheduler.remove, scheduler.pause, scheduler.resume, session.list, session.close, run.list, run.get, http.request, time.now, shell.exec.\n- Preferred format for tool calls is a fenced JSON object with tool_name and arguments.\n- Example:\n```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```\n- For shell commands use shell.exec with command=`bash` and args=[\"-lc\", \"<script>\"].\n- Runtime retries `/bin/bash` and `/usr/bin/bash` before fallback to `sh`; keep scripts POSIX-compatible when possible.\n- Common runtime shell tools include: python3/pip, node/npm, git, curl, wget, jq, nmap, dig/nslookup, ip, ss, netstat, traceroute, tcpdump.\n- For connectivity checks, prefer read-only diagnostics first (for example `ip addr`, `ss -tulpen`, `dig`, `curl -I`, `nmap -sT`).\n- For multi-step shell tasks, prefer one well-structured script in a single `shell.exec` call over many tiny probe commands.\n- Use fs.append to add content to existing files without replacing prior content.\n- Use fs.delete for removals; pass recursive=true only when deleting directories intentionally.\n- Use fs.move for renames/moves. Pass overwrite=true only when destination replacement is intentional.\n- Use config.set only for safe runtime knobs; do not use it for provider API keys or secret values.\n- Use secrets.set for secret writes and secrets.get for reads; never echo secret values in plain text summaries.\n- Use scheduler.add/list/remove/pause/resume for job lifecycle; keep schedules valid (`@every` or RFC3339 one-shot).\n- Use session.list to inspect recent sessions and session.close to retire a session so future chat routing creates a new one.\n- Use run.list to enumerate runs with filtering (agent_id, status) and pagination (limit, offset); use run.get to retrieve a specific run by ID.\n- Use http.request only for http/https targets allowed by network config; keep timeout and response size bounded.\n- If the user asks you to do work, continue executing the plan directly; do not ask permission-style follow-up questions.\n- If a command fails due to permissions/capabilities, surface the exact stderr and try a safer fallback command when possible.\n- If you already have enough evidence from tool results, stop calling tools and provide the final answer.\n- Avoid running the exact same failing command repeatedly; adjust flags or explain the failure instead.\n- Do not invent tool names (for example time.sleep is invalid).\n- Do not claim file edits or command results until a matching tool.result is observed.\n- For multi-step requests, chain tool calls until the task is complete instead of stopping after the first step.\n- After each tool result, send a short progress update before issuing the next tool call when possible.\n"
}

func toolCallingBestPracticesDocWithAgentTools() string {
	doc := toolCallingBestPracticesDoc()
	doc = strings.Replace(doc,
		"secrets.get, secrets.set, secrets.list, scheduler.list, scheduler.add, scheduler.remove, scheduler.pause, scheduler.resume, session.list, session.close, run.list, run.get, http.request, time.now, shell.exec.",
		"secrets.get, secrets.set, secrets.list, skill.list, skill.read, scheduler.list, scheduler.add, scheduler.remove, scheduler.pause, scheduler.resume, session.list, session.close, run.list, run.get, http.request, time.now, shell.exec.",
		1,
	)
	doc = strings.Replace(doc,
		"- Use secrets.set for secret writes and secrets.get for reads; never echo secret values in plain text summaries.",
		"- Use secrets.set for secret writes and secrets.get for reads; never echo secret values in plain text summaries.\n- Use skill.list and skill.read to discover workspace skills under skills/ and validate required secret keys before execution.",
		1,
	)
	doc = strings.Replace(doc,
		"session.list, session.close, run.list, run.get, http.request, time.now, shell.exec.",
		"session.list, session.close, agent.list, agent.create, agent.switch, run.list, run.get, run.cancel, http.request, time.now, shell.exec.",
		1,
	)
	doc = strings.Replace(doc,
		"session.list, session.close, agent.list, agent.create, agent.switch, run.list, run.get, run.cancel, http.request, time.now, shell.exec.",
		"session.list, session.close, agent.list, agent.create, agent.switch, policy.list, policy.grant, policy.revoke, run.list, run.get, run.cancel, metrics.get, http.request, time.now, shell.exec.",
		1,
	)
	doc = strings.Replace(doc,
		"- Use run.list to enumerate runs with filtering (agent_id, status) and paginatio...",
		"- Use agent.list to inspect available agents, agent.create to scaffold missing agents, and agent.switch to update chat/discord defaults safely.\n- Use policy.list/policy.grant/policy.revoke for capability governance; only agents with policy.admin should mutate grants.\n- Use run.list to enumerate runs with filtering (agent_id, status), use run.get for details, and use run.cancel to stop a running task when needed.\n- Use metrics.get to inspect run-level and per-tool duration/error trends.",
		1,
	)
	return doc
}

type RegistryExecutor struct {
	Registry  *tools.Registry
	AgentID   string
	Workspace string
}

func (r *RegistryExecutor) Execute(ctx context.Context, call agent.ToolCallRequest) (agent.ToolCallResult, error) {
	if r == nil || r.Registry == nil {
		return agent.ToolCallResult{ID: call.ID}, errors.New("runtime: tool registry is not configured")
	}

	args := map[string]any{}
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return agent.ToolCallResult{ID: call.ID}, fmt.Errorf("runtime: invalid tool args: %w", err)
		}
	}
	args = normalizeToolArgs(call.Name, args)

	res, err := r.Registry.Execute(ctx, r.AgentID, call.Name, r.Workspace, args)
	if err != nil {
		return agent.ToolCallResult{ID: call.ID}, err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return agent.ToolCallResult{ID: call.ID}, err
	}
	return agent.ToolCallResult{ID: call.ID, Output: string(b)}, nil
}

func normalizeToolArgs(toolName string, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}

	ensurePath := func() {
		if getStringArg(args, "path") != "" {
			args["path"] = sanitizePathArg(getStringArg(args, "path"))
			return
		}
		for _, key := range []string{"file", "filename", "target", "name"} {
			if value := getStringArg(args, key); value != "" {
				args["path"] = sanitizePathArg(value)
				return
			}
		}
	}

	switch toolName {
	case "fs.list":
		ensurePath()
		if getStringArg(args, "path") == "" {
			args["path"] = "."
		}
	case "fs.read":
		ensurePath()
	case "fs.delete":
		ensurePath()
	case "fs.move":
		if getStringArg(args, "src") == "" {
			for _, key := range []string{"source", "from", "path", "file"} {
				if value := getStringArg(args, key); value != "" {
					args["src"] = sanitizePathArg(value)
					break
				}
			}
		}
		if getStringArg(args, "dst") == "" {
			for _, key := range []string{"destination", "to", "target", "new_path"} {
				if value := getStringArg(args, key); value != "" {
					args["dst"] = sanitizePathArg(value)
					break
				}
			}
		}
	case "fs.write":
		ensurePath()
		if getStringArg(args, "content") == "" {
			for _, key := range []string{"text", "body", "code", "value", "data", "newText"} {
				if value := getStringArg(args, key); value != "" {
					args["content"] = value
					break
				}
			}
		}
		if getStringArg(args, "content") == "" {
			path := getStringArg(args, "path")
			if idx := strings.Index(path, ","); idx > 0 {
				pathPart := strings.TrimSpace(path[:idx])
				rest := strings.TrimSpace(path[idx+1:])
				pathPart = trimMatchingQuotes(pathPart)
				pathPart = strings.Trim(pathPart, `"'`)
				rest = strings.TrimSpace(strings.TrimPrefix(rest, "\"\"\""))
				rest = strings.TrimSpace(strings.TrimSuffix(rest, "\"\"\""))
				rest = trimMatchingQuotes(rest)
				if pathPart != "" {
					args["path"] = pathPart
				}
				if rest != "" {
					args["content"] = rest
				}
			}
		}
	case "fs.edit":
		ensurePath()
		if getStringArg(args, "patch") == "" {
			for _, key := range []string{"diff", "unified_diff", "unifiedDiff"} {
				if value := getStringArg(args, key); value != "" {
					args["patch"] = value
					break
				}
			}
		}
		if getStringArg(args, "old") == "" {
			for _, key := range []string{"find", "from"} {
				if value := getStringArg(args, key); value != "" {
					args["old"] = value
					break
				}
			}
		}
		if getStringArg(args, "new") == "" {
			for _, key := range []string{"replace", "to", "newText", "value"} {
				if value := getStringArg(args, key); value != "" {
					args["new"] = value
					break
				}
			}
		}
	case "code.search":
		if getStringArg(args, "pattern") == "" {
			if value := getStringArg(args, "query"); value != "" {
				args["pattern"] = value
			}
		}
	case "skill.read":
		if getStringArg(args, "name") == "" {
			for _, key := range []string{"skill", "skill_name", "skillName", "id"} {
				if value := getStringArg(args, key); value != "" {
					args["name"] = strings.TrimSpace(value)
					break
				}
			}
		}
		if getStringArg(args, "path") == "" {
			for _, key := range []string{"file", "filename", "skill_path", "skillPath"} {
				if value := getStringArg(args, key); value != "" {
					args["path"] = sanitizePathArg(value)
					break
				}
			}
		}
		if getStringArg(args, "root") == "" {
			for _, key := range []string{"dir", "directory"} {
				if value := getStringArg(args, key); value != "" {
					args["root"] = sanitizePathArg(value)
					break
				}
			}
		}
	case "http.request":
		if getStringArg(args, "url") == "" {
			for _, key := range []string{"uri", "endpoint", "link"} {
				if value := getStringArg(args, key); value != "" {
					args["url"] = strings.TrimSpace(value)
					break
				}
			}
		}
	case "session.close":
		if getStringArg(args, "session_id") == "" {
			for _, key := range []string{"id", "session", "sessionId"} {
				if value := getStringArg(args, key); value != "" {
					args["session_id"] = strings.TrimSpace(value)
					break
				}
			}
		}
	case "agent.create", "agent.switch":
		if getStringArg(args, "agent_id") == "" {
			for _, key := range []string{"id", "agent", "name", "agentId"} {
				if value := getStringArg(args, key); value != "" {
					args["agent_id"] = strings.TrimSpace(value)
					break
				}
			}
		}
	case "policy.list", "policy.grant", "policy.revoke":
		if getStringArg(args, "agent_id") == "" {
			for _, key := range []string{"id", "agent", "target_agent", "targetAgent", "agentId"} {
				if value := getStringArg(args, key); value != "" {
					args["agent_id"] = strings.TrimSpace(value)
					break
				}
			}
		}
		if toolName == "policy.grant" || toolName == "policy.revoke" {
			if getStringArg(args, "capability") == "" {
				for _, key := range []string{"tool", "permission", "grant", "cap"} {
					if value := getStringArg(args, key); value != "" {
						args["capability"] = strings.TrimSpace(value)
						break
					}
				}
			}
		}
	case "shell.exec":
		if getStringArg(args, "command") == "" {
			if value := getStringArg(args, "cmd"); value != "" {
				args["command"] = value
			} else if value := getStringArg(args, "path"); value != "" {
				args["command"] = value
			}
		}
		command := getStringArg(args, "command")
		if command != "" && strings.Contains(command, " ") && len(getStringSliceArg(args, "args")) == 0 {
			args["command"] = "bash"
			args["args"] = []string{"-lc", command}
		}
	}

	return args
}

func getStringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func getStringSliceArg(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	out := make([]string, 0)
	switch raw := v.(type) {
	case []string:
		for _, item := range raw {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
	case []any:
		for _, item := range raw {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func trimMatchingQuotes(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 2 {
		return v
	}
	if (strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`)) || (strings.HasPrefix(v, `'`) && strings.HasSuffix(v, `'`)) {
		return strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}

func sanitizePathArg(path string) string {
	path = strings.TrimSpace(path)
	path = trimMatchingQuotes(path)
	path = strings.Trim(path, "`")
	if path == "```" {
		return ""
	}
	if strings.HasPrefix(path, "```") {
		path = strings.TrimPrefix(path, "```")
	}
	if strings.HasSuffix(path, "```") {
		path = strings.TrimSuffix(path, "```")
	}
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return ""
	}
	return path
}

func (e *Engine) allowedTools(cfg config.Config) []string {
	toolsList := []string{"fs.read", "fs.list", "fs.write", "fs.append", "fs.delete", "fs.move", "fs.edit", "code.search", "config.get", "config.set", "secrets.get", "secrets.set", "secrets.list", "skill.list", "skill.read", "scheduler.list", "scheduler.add", "scheduler.remove", "scheduler.pause", "scheduler.resume", "session.list", "session.close", "agent.list", "agent.create", "agent.switch", "policy.list", "policy.grant", "policy.revoke", "run.list", "run.get", "run.cancel", "metrics.get", "time.now"}
	if cfg.Network.Enabled {
		toolsList = append(toolsList, "http.request")
	}
	if cfg.Shell.EnableExec && cfg.Sandbox.Active && strings.ToLower(cfg.Sandbox.Provider) != "none" {
		toolsList = append(toolsList, "shell.exec")
	}
	return toolsList
}

func (e *Engine) effectiveCapabilities(agentID string, allowedTools []string) []string {
	base := append([]string(nil), allowedTools...)
	if agentID == "default" {
		base = append(base, "policy.admin")
	}
	base = policy.NormalizeCapabilities(base)

	grantsPath := filepath.Join(e.rootDir, ".openclawssy", "policy", "capabilities.json")
	persisted, err := policy.LoadGrants(grantsPath)
	if err != nil {
		return base
	}
	stored, ok := persisted[agentID]
	if !ok {
		return base
	}

	allowedSet := make(map[string]bool, len(allowedTools))
	for _, tool := range allowedTools {
		canonical := policy.CanonicalCapability(tool)
		if canonical == "" {
			continue
		}
		allowedSet[canonical] = true
	}

	out := make([]string, 0, len(stored))
	for _, capability := range stored {
		canonical := policy.CanonicalCapability(capability)
		if canonical == "" {
			continue
		}
		if canonical == "policy.admin" || allowedSet[canonical] {
			out = append(out, canonical)
		}
	}
	return policy.NormalizeCapabilities(out)
}

type sandboxShellExecutor struct {
	provider sandbox.Provider
}

func (s *sandboxShellExecutor) Exec(_ context.Context, command string, args []string) (string, string, int, error) {
	result, err := s.provider.Exec(sandbox.Command{Name: command, Args: args})
	return result.Stdout, result.Stderr, result.ExitCode, err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
