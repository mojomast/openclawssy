package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/agentdocs"
	"openclawssy/internal/artifacts"
	"openclawssy/internal/audit"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/memory"
	memorystore "openclawssy/internal/memory/store"
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
	AgentID           string
	Message           string
	Source            string
	SessionID         string
	ThinkingMode      string
	AllowedTools      []string // If non-empty, overrides the default allowed tools for this run.
	MaxToolIterations int      // If >0, overrides the default tool iteration cap.
	TimeoutMS         int      // If >0, applies as a context deadline for this run.
	OnProgress        func(eventType string, data map[string]any)
}

const (
	maxSessionContextChars         = 60000
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
	if _, err := agentdocs.SeedAgentScaffold(agentRoot, force); err != nil {
		return fmt.Errorf("runtime: scaffold agent: %w", err)
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
	if !isAgentEnabled(cfg, agentID) {
		return RunResult{}, fmt.Errorf("runtime: agent %q is inactive by configuration", agentID)
	}
	selectedModel := resolveAgentModelConfig(cfg, agentID)
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
	memoryManager, memoryErr := memory.NewManager(e.agentsDir, agentID, memory.Options{
		Enabled:    cfg.Memory.Enabled,
		BufferSize: cfg.Memory.EventBufferSize,
	})
	if memoryErr != nil {
		slog.Warn("runtime: memory disabled for run", "run_id", runID, "error", memoryErr)
	}
	defer func() {
		if memoryManager == nil {
			return
		}
		go func(runID string, manager *memory.Manager) {
			if err := manager.Close(); err != nil {
				slog.Warn("runtime: memory close failure", "run_id", runID, "error", err)
			}
		}(runID, memoryManager)
	}()

	// Create cancellable context for this run and track it
	runCtxBase, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	runCtx := runCtxBase
	runTimeout := resolveRunTimeout(cfg.Engine)
	// Per-subagent timeout overrides the engine default when set.
	if in.TimeoutMS > 0 {
		runTimeout = time.Duration(in.TimeoutMS) * time.Millisecond
	}
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

	startEvent := map[string]any{"run_id": runID, "agent_id": agentID, "message": message, "model_provider": selectedModel.Provider, "model_name": selectedModel.Name}
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
	// Apply per-subagent restrictions from ExecuteInput (set by subagent adapter).
	if len(in.AllowedTools) > 0 {
		allowedTools = in.AllowedTools
	}
	if in.MaxToolIterations > 0 {
		maxToolIterations = in.MaxToolIterations
	}
	effectiveCaps := e.effectiveCapabilities(agentID, allowedTools)
	traceCollector := newRunTraceCollector(runID, sessionID, source, message)
	runCtx = withRunTraceCollector(runCtx, traceCollector)
	enforcer := policy.NewEnforcer(e.workspaceDir, map[string][]string{agentID: effectiveCaps})

	// For docker provider: use /workspace as the workspace root inside the
	// container.  All path resolution is redirected to the container paths.
	isDockerSandbox := cfg.Sandbox.Active && strings.ToLower(cfg.Sandbox.Provider) == "docker"
	var effectivePolicy tools.Policy = enforcer
	if isDockerSandbox {
		effectivePolicy = &dockerPolicyEnforcer{inner: enforcer}
	}

	registry := tools.NewRegistry(effectivePolicy, aud)

	// Resolve workspace root: in docker mode the workspace root inside the
	// container is always /workspace; on host it is e.workspaceDir.
	toolWorkspaceRoot := e.workspaceDir
	if isDockerSandbox {
		toolWorkspaceRoot = "/workspace"
	}

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
		WorkspaceRoot:   toolWorkspaceRoot,
		AgentRunner:     &subAgentRunner{engine: e},
	}); err != nil {
		return RunResult{}, fmt.Errorf("runtime: register core tools: %w", err)
	}
	registry.SetShellAllowedCommands(cfg.Shell.AllowedCommands)

	var provider sandbox.Provider
	if cfg.Sandbox.Active {
		provider, err = sandbox.NewProviderForAgent(
			cfg.Sandbox.Provider, e.workspaceDir, agentID, cfg.Sandbox.Docker)
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
		// For docker provider: route all fs.* operations through the container
		// so no workspace data ever touches the host filesystem.
		if isDockerSandbox {
			registry.SetSandboxProvider(&sandboxProviderAdapter{provider: provider})
		}
	}

	secretStore, _ := secrets.NewStore(cfg)
	lookup := func(name string) (string, bool, error) {
		if secretStore == nil {
			return "", false, nil
		}
		return secretStore.Get(name)
	}

	model, err := NewProviderModelForConfig(cfg, selectedModel, lookup)
	if err != nil {
		return RunResult{}, err
	}

	subAgentRunner := &subAgentRunner{engine: e}
	adapter := &agentSubAgentRunnerAdapter{
		runner:            subAgentRunner,
		subAgentDefaults:  cfg.Agents.SubAgentDefaults,
		subAgentOverrides: cfg.Agents.SubAgentOverrides,
	}

	runner := agent.Runner{
		Model:             model,
		ToolExecutor:      &RegistryExecutor{Registry: registry, AgentID: agentID, Workspace: e.workspaceDir},
		MaxToolIterations: agent.DefaultToolIterationCap,
		SubAgentRunner:    adapter,
	}
	toolSchemas := e.allowedToolSchemas(registry.List(), allowedTools)

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
	emitProgress := func(eventType string, data map[string]any) {
		if in.OnProgress == nil {
			return
		}
		if data == nil {
			data = map[string]any{}
		}
		in.OnProgress(eventType, data)
	}
	onToolCall := func(rec agent.ToolCallRecord) error { return nil }
	if conversationStore != nil {
		appendToolsAfterRun = false
		onToolCall = func(rec agent.ToolCallRecord) error {
			return appendToolCallMessage(conversationStore, sessionID, runID, rec)
		}
	}
	baseOnToolCall := onToolCall
	onToolCall = func(rec agent.ToolCallRecord) error {
		durationMS := rec.CompletedAt.Sub(rec.StartedAt).Milliseconds()
		emitProgress("tool_end", map[string]any{
			"tool":         rec.Request.Name,
			"tool_call_id": rec.Request.ID,
			"arguments":    rec.Request.Arguments,
			"summary":      summarizeToolExecution(rec.Request.Name, rec.Result.Output, rec.Result.Error),
			"output":       rec.Result.Output,
			"error":        strings.TrimSpace(rec.Result.Error),
			"duration_ms":  durationMS,
		})
		err := baseOnToolCall(rec)
		e.maybeTriggerProactiveMemoryHook(runCtx, cfg, registry, agentID, sessionID, runID, rec)
		return err
	}

	var onTextDelta func(delta string) error
	if in.OnProgress != nil {
		onTextDelta = func(delta string) error {
			if delta == "" {
				return nil
			}
			emitProgress("model_text", map[string]any{"text": delta, "partial": true})
			return nil
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
		ToolSchemas:       toolSchemas,
		OnToolCall:        onToolCall,
		SystemPromptExt:   e.memoryPromptExtender(cfg, agentID, runID),
		OnTextDelta:       onTextDelta,
	})
	if runErr == nil {
		emitProgress("model_text", map[string]any{"text": out.FinalText, "partial": false})
	}

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
				slog.Warn("runtime: failed to append run conversation", "run_id", runID, "session_id", sessionID, "error", err)
			}
		}
	}
	logToolCallbackFailures(runCtx, aud, runID, agentID, out.ToolCalls, source, sessionID)
	ingestRunMemoryEvents(runCtx, memoryManager, runMemoryEventInput{
		AgentID:   agentID,
		RunID:     runID,
		SessionID: sessionID,
		Source:    source,
		Message:   message,
		Output:    out,
		RunErr:    runErr,
	})
	if memoryManager != nil {
		stats := memoryManager.Stats()
		if stats.DroppedEvents > 0 {
			slog.Warn("runtime: dropped memory events", "dropped_events", stats.DroppedEvents, "run_id", runID)
		}
	}
	if runErr != nil && sessionID != "" {
		if err := e.appendRunFailureConversation(sessionID, runID, out, appendToolsAfterRun, runErr); err != nil {
			slog.Warn("runtime: failed to append run failure conversation", "run_id", runID, "session_id", sessionID, "error", err)
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
		slog.Warn("runtime: rejected run due to max concurrent runs limit", "limit", activeCap)
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
	soulContent := ""
	hasSoulDoc := false
	for _, name := range promptDocOrder {
		path := filepath.Join(agentRoot, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("runtime: read %s: %w", name, err)
		}
		content := string(data)
		docs = append(docs, agent.ArtifactDoc{Name: name, Content: content})
		if name == "SOUL.md" {
			soulContent = content
			hasSoulDoc = true
		}
	}

	// Check for identity.json - if present with valid identity, no bootstrap needed
	needsBootstrap := !hasSoulDoc || agentdocs.SoulNeedsBootstrap(soulContent)
	if needsBootstrap {
		identityPath := filepath.Join(agentRoot, "identity.json")
		if identityData, err := os.ReadFile(identityPath); err == nil {
			// Parse identity.json to extract names
			var identity struct {
				UserName      string `json:"user_name"`
				AssistantName string `json:"assistant_name"`
			}
			if err := json.Unmarshal(identityData, &identity); err == nil {
				if identity.UserName != "" && identity.AssistantName != "" {
					// Identity is set via identity.json, no bootstrap needed
					// Optionally inject identity into SOUL content or keep separate
					needsBootstrap = false
				}
			}
		}
	}

	if needsBootstrap {
		docs = append(docs, agent.ArtifactDoc{Name: "IDENTITY_BOOTSTRAP.md", Content: identityBootstrapDoc()})
	}
	docs = append(docs, agent.ArtifactDoc{Name: "RUNTIME_CONTEXT.md", Content: runtimeContextDoc(e.workspaceDir)})
	docs = append(docs, agent.ArtifactDoc{Name: "TOOL_CALLING_BEST_PRACTICES.md", Content: toolCallingBestPracticesDocWithAgentTools()})
	return docs, nil
}

func identityBootstrapDoc() string {
	return "# IDENTITY_BOOTSTRAP\n\n" +
		"SOUL.md is empty or uninitialized. Run identity hatch before normal task work.\n\n" +
		"## Required flow\n" +
		"- Ask at most one question per response until both names are known.\n" +
		"- Collect exactly two values: user preferred name and assistant name.\n" +
		"- When both are known, call `agent.identity.set` with `user_name` and `assistant_name`.\n" +
		"- After the tool succeeds, confirm the names and continue with the user's latest request.\n" +
		"- Do not call `agent.prompt.update` for this bootstrap flow.\n"
}

func runtimeContextDoc(workspaceDir string) string {
	bullets := []string{
		fmt.Sprintf("Workspace root: %s", workspaceDir),
		"File tools (fs.read/fs.list/fs.write/fs.append/fs.delete/fs.move/fs.edit/code.search) can only access paths inside workspace root.",
		"Config tools (config.get/config.set) can only read redacted config and mutate an allowlisted safe field subset.",
		"Secret tools (secrets.get/secrets.set/secrets.list) use encrypted secret storage; secret values are never written to audit fields in plaintext.",
		"Skill tools (skill.list/skill.read) discover workspace skills under skills/ and report required secret keys with missing-secret diagnostics.",
		"Memory tools (memory.search/memory.write/memory.update/memory.forget/memory.health/memory.checkpoint/memory.maintenance/decision.log) persist structured per-agent working memory in .openclawssy/agents/<agent>/memory/memory.db.",
		"Scheduler tools (scheduler.list/add/remove/pause/resume) persist jobs in .openclawssy/scheduler/jobs.json and enforce scheduler validation rules.",
		"Session tools (session.list/session.close) manage chat sessions persisted under .openclawssy/agents/*/memory/chats and closed sessions are not reused for routing.",
		"Agent tools (agent.list/agent.create/agent.switch/agent.identity.set) manage agent scaffolds, identities, and default routing; agent.profile.get/agent.profile.set control per-agent model and activation settings.",
		"Run tools (run.list/run.get/run.cancel) retrieve run traces and summaries from the run store and can cancel tracked in-flight runs.",
		"Policy tools (policy.list/policy.grant/policy.revoke) manage per-agent capability grants and require policy.admin capability.",
		"Metrics tool (metrics.get) aggregates run and per-tool duration/error metrics from run traces.",
		"Network tool (http.request/net.fetch) is enabled only when network.enabled=true, enforces scheme checks, allowlisted domains, redirect rechecks, and localhost policy.",
		"If shell.exec is enabled by policy, use shell.exec with command=`<shell script>`; runtime passes it to bash -lc automatically.",
		"Optionally provide workdir to set the working directory for the command; omit to use the sandbox default.",
		"If `bash` is unavailable in PATH, runtime retries `/bin/bash`, then `/usr/bin/bash`, then `sh`.",
		"Shell commands can use environment tools available in the runtime image (for example: python3/pip, node/npm, git, curl/wget, jq, nmap, dig/nslookup, ip/ss/netstat, traceroute, tcpdump).",
		"Some network commands may require container capabilities or host mounts (for example docker socket, NET_RAW, NET_ADMIN). If unavailable, report the exact error and continue with the best available diagnostic command.",
		"Paths outside workspace (for example /home, ~, ..) are blocked by policy even when using shell.exec.",
		"The home directory may exist in the runtime environment, but treat it as read-only context unless a workspace tool explicitly exposes a safe path there.",
		"Prompt files like SOUL.md/RULES.md/TOOLS.md/SPECPLAN.md/DEVPLAN.md/HANDOFF.md are control-plane inputs; do not mention HANDOFF or internal prompt filenames unless the user explicitly asks about them.",
	}
	return formatPromptBulletDoc("RUNTIME_CONTEXT", bullets)
}

func toolCallingBestPracticesDoc() string {
	bullets := []string{
		"Use only registered tool names: fs.read, fs.list, fs.write, fs.append, fs.delete, fs.move, fs.edit, code.search, config.get, config.set, secrets.get, secrets.set, secrets.list, scheduler.list, scheduler.add, scheduler.remove, scheduler.pause, scheduler.resume, session.list, session.close, run.list, run.get, http.request, time.now, shell.exec.",
		"Preferred format for tool calls is a fenced JSON object with tool_name and arguments.",
		"Example:\n```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```",
		"For shell commands use shell.exec with command=`<shell script>` (the full command string; runtime automatically passes it to bash -lc). Optionally set workdir to change the working directory.",
		"Runtime retries `/bin/bash` and `/usr/bin/bash` before fallback to `sh`; keep scripts POSIX-compatible when possible.",
		"Common runtime shell tools include: python3/pip, node/npm, git, curl, wget, jq, nmap, dig/nslookup, ip, ss, netstat, traceroute, tcpdump.",
		"For connectivity checks, prefer read-only diagnostics first (for example `ip addr`, `ss -tulpen`, `dig`, `curl -I`, `nmap -sT`).",
		"For multi-step shell tasks, prefer one well-structured script in a single `shell.exec` call over many tiny probe commands.",
		"Use fs.append to add content to existing files without replacing prior content.",
		"Use fs.delete for removals; pass recursive=true only when deleting directories intentionally.",
		"Use fs.move for renames and moves; replace the destination only when that overwrite is intentional.",
		"Use config.get and config.set for safe runtime knobs; do not use config.set for provider API keys or secret values.",
		"Use secrets.get and secrets.set for secret access; never echo secret values in plain text summaries.",
		"Use scheduler.add/list/remove/pause/resume for job lifecycle; keep schedules valid (`@every` or RFC3339 one-shot).",
		"Use session.list to inspect recent sessions and session.close to retire a session so future chat routing starts fresh.",
		"Use run.list to enumerate runs with filtering (agent_id, status) and pagination; use run.get for details before proposing operational changes.",
		"Do not invent tool names or arguments. If a capability is unavailable, say so plainly and continue with the best available path.",
		"When the task requires tools, chain tool calls until the task is complete or you are truly blocked; do not stop after a single successful read if more execution is clearly needed.",
	}
	return formatPromptBulletDoc("TOOL_CALLING_BEST_PRACTICES", bullets)
}

func toolCallingBestPracticesDocWithAgentTools() string {
	bullets := []string{
		"Use only registered tool names: fs.read, fs.list, fs.write, fs.append, fs.delete, fs.move, fs.edit, code.search, config.get, config.set, secrets.get, secrets.set, secrets.list, skill.list, skill.read, scheduler.list, scheduler.add, scheduler.remove, scheduler.pause, scheduler.resume, session.list, session.close, agent.list, agent.create, agent.switch, agent.profile.get, agent.profile.set, agent.message.send, agent.message.inbox, agent.run, agent.prompt.read, agent.prompt.update, agent.prompt.suggest, agent.identity.set, policy.list, policy.grant, policy.revoke, run.list, run.get, run.cancel, metrics.get, memory.search, memory.write, memory.update, memory.forget, memory.health, memory.checkpoint, memory.maintenance, decision.log, http.request, time.now, shell.exec.",
		"Preferred format for tool calls is a fenced JSON object with tool_name and arguments.",
		"Example:\n```json\n{\"tool_name\":\"fs.list\",\"arguments\":{\"path\":\".\"}}\n```",
		"For shell commands use shell.exec with command=`<shell script>` (the full command string; runtime automatically passes it to bash -lc). Optionally set workdir to change the working directory.",
		"Runtime retries `/bin/bash` and `/usr/bin/bash` before fallback to `sh`; keep scripts POSIX-compatible when possible.",
		"Common runtime shell tools include: python3/pip, node/npm, git, curl, wget, jq, nmap, dig/nslookup, ip, ss, netstat, traceroute, tcpdump.",
		"For connectivity checks, prefer read-only diagnostics first (for example `ip addr`, `ss -tulpen`, `dig`, `curl -I`, `nmap -sT`).",
		"For multi-step shell tasks, prefer one well-structured script in a single `shell.exec` call over many tiny probe commands.",
		"Use fs.append to add content to existing files without replacing prior content.",
		"Use fs.delete for removals; pass recursive=true only when deleting directories intentionally.",
		"Use fs.move for renames and moves; replace the destination only when that overwrite is intentional.",
		"Use config.get and config.set for safe runtime knobs; do not use config.set for provider API keys or secret values.",
		"Use secrets.get and secrets.set for secret access; never echo secret values in plain text summaries.",
		"Use skill.list and skill.read to discover workspace skills under skills/ and validate required secret keys before execution.",
		"Use scheduler.add and scheduler.resume together with scheduler.list/scheduler.remove/scheduler.pause for job lifecycle; keep schedules valid (`@every` or RFC3339 one-shot).",
		"Use session.list and session.close to inspect and retire sessions so future chat routing starts fresh.",
		"Use agent.list and agent.switch to inspect or change active routing targets; use agent.create to scaffold missing agents safely.",
		"Use agent.profile.get and agent.profile.set for per-agent activation and model/provider controls.",
		"Use agent.message.send and agent.message.inbox for structured inter-agent collaboration, and use agent.run for direct subagent execution.",
		"Use agent.prompt.read and agent.prompt.suggest for prompt governance; use agent.prompt.update only when self-improvement controls allow it.",
		"Use agent.identity.set to bootstrap SOUL identity fields when SOUL.md is empty; provide assistant_name and user_name.",
		"Use policy.list, policy.grant, and policy.revoke for capability governance; only agents with policy.admin should mutate grants.",
		"Use run.list to enumerate runs with filtering (agent_id, status) and pagination, use run.get for details, and use run.cancel to stop a running task when needed.",
		"Use metrics.get to inspect run-level and per-tool duration/error trends.",
		"Never emit pseudo-XML tool syntax (for example `<tool_call>...` or `<arg_value>`); runtime executes only valid JSON tool-call objects.",
		"Tool success pattern: successful calls usually return structured JSON result objects or plain shell.exec output; treat either as completion evidence and continue the task instead of re-calling the same tool without new inputs.",
		"Tool failure pattern: failed calls return coded errors (for example tool.input_invalid, policy.denied, timeout, internal.error); read the error, adjust arguments or strategy, and avoid blind retries with identical inputs.",
		"Do not invent tool names or arguments. If a capability is unavailable, say so plainly and continue with the best available path.",
		"When the task requires tools, chain tool calls until the task is complete or you are truly blocked; do not stop after a single successful read if more execution is clearly needed.",
	}
	return formatPromptBulletDoc("TOOL_CALLING_BEST_PRACTICES", bullets)
}

func formatPromptBulletDoc(title string, bullets []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(strings.TrimSpace(title))
	b.WriteString("\n\n")
	for i, bullet := range bullets {
		if i > 0 {
			b.WriteString("\n")
		}
		text := strings.TrimSpace(bullet)
		if strings.Contains(text, "\n") {
			parts := strings.Split(text, "\n")
			b.WriteString("- ")
			b.WriteString(parts[0])
			for _, part := range parts[1:] {
				b.WriteString("\n")
				b.WriteString(part)
			}
			continue
		}
		b.WriteString("- ")
		b.WriteString(text)
	}
	b.WriteString("\n")
	return b.String()
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
	if call.Name == "shell.exec" {
		return agent.ToolCallResult{ID: call.ID, Output: shellExecModelOutput(res)}, nil
	}
	b, err := json.Marshal(res)
	if err != nil {
		return agent.ToolCallResult{ID: call.ID}, err
	}
	return agent.ToolCallResult{ID: call.ID, Output: string(b)}, nil
}

func shellExecModelOutput(res map[string]any) string {
	stdout := strings.TrimSpace(fmt.Sprintf("%v", res["stdout"]))
	stderr := strings.TrimSpace(fmt.Sprintf("%v", res["stderr"]))
	errText := strings.TrimSpace(fmt.Sprintf("%v", res["error"]))

	chunks := make([]string, 0, 3)
	if stdout != "" && stdout != "<nil>" {
		chunks = append(chunks, stdout)
	}
	if stderr != "" && stderr != "<nil>" {
		chunks = append(chunks, stderr)
	}
	if len(chunks) == 0 && errText != "" && errText != "<nil>" {
		chunks = append(chunks, errText)
	}
	return strings.Join(chunks, "\n")
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
	case "secrets.get", "secrets.set":
		aliasKeys := []string{"name", "secret", "secret_key", "secretKey", "env", "env_var", "token"}
		if getStringArg(args, "key") == "" {
			for _, key := range aliasKeys {
				if value := getStringArg(args, key); value != "" {
					args["key"] = strings.TrimSpace(value)
					break
				}
			}
		}
		if key := canonicalRuntimeSecretKey(getStringArg(args, "key")); key != "" {
			args["key"] = key
		}
		for _, alias := range aliasKeys {
			delete(args, alias)
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
	case "agent.create", "agent.switch", "agent.run", "agent.prompt.read", "agent.prompt.update", "agent.prompt.suggest", "agent.profile.get", "agent.profile.set", "agent.identity.set":
		if getStringArg(args, "agent_id") == "" {
			for _, key := range []string{"id", "agent", "name", "agentId"} {
				if value := getStringArg(args, key); value != "" {
					args["agent_id"] = strings.TrimSpace(value)
					break
				}
			}
		}
	case "agent.message.send":
		if getStringArg(args, "to_agent_id") == "" {
			for _, key := range []string{"agent_id", "target_agent_id", "target", "to", "toAgentId"} {
				if value := getStringArg(args, key); value != "" {
					args["to_agent_id"] = strings.TrimSpace(value)
					break
				}
			}
		}
		if getStringArg(args, "message") == "" {
			for _, key := range []string{"content", "text", "body", "prompt"} {
				if value := getStringArg(args, key); value != "" {
					args["message"] = value
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
	}

	return args
}

func canonicalRuntimeSecretKey(raw string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return ""
	}
	lower := strings.ToLower(key)
	if strings.HasPrefix(lower, "provider/") && strings.HasSuffix(lower, "/api_key") {
		return lower
	}
	upper := strings.ToUpper(key)
	if strings.HasSuffix(upper, "_API_KEY") {
		provider := strings.TrimSuffix(upper, "_API_KEY")
		provider = strings.ReplaceAll(provider, "_", "-")
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider != "" {
			return fmt.Sprintf("provider/%s/api_key", provider)
		}
	}
	return key
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
	toolsList := []string{"fs.read", "fs.list", "fs.write", "fs.append", "fs.delete", "fs.move", "fs.edit", "code.search", "config.get", "config.set", "secrets.get", "secrets.set", "secrets.list", "skill.list", "skill.read", "scheduler.list", "scheduler.add", "scheduler.remove", "scheduler.pause", "scheduler.resume", "session.list", "session.close", "agent.list", "agent.create", "agent.switch", "agent.profile.get", "agent.profile.set", "agent.message.send", "agent.message.inbox", "agent.run", "agent.prompt.read", "agent.prompt.update", "agent.prompt.suggest", "agent.identity.set", "policy.list", "policy.grant", "policy.revoke", "run.list", "run.get", "run.cancel", "metrics.get", "memory.search", "memory.write", "memory.update", "memory.forget", "memory.health", "memory.checkpoint", "memory.maintenance", "decision.log", "time.now"}
	if cfg.Network.Enabled {
		toolsList = append(toolsList, "http.request")
	}
	if cfg.Shell.EnableExec && cfg.Sandbox.Active && strings.ToLower(cfg.Sandbox.Provider) != "none" {
		toolsList = append(toolsList, "shell.exec")
	}
	return toolsList
}

func (e *Engine) allowedToolSchemas(specs []tools.ToolSpec, allowed []string) []agent.ToolSchema {
	if len(specs) == 0 || len(allowed) == 0 {
		return nil
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		allowedSet[name] = struct{}{}
	}
	out := make([]agent.ToolSchema, 0, len(allowedSet))
	for _, spec := range specs {
		if _, ok := allowedSet[spec.Name]; !ok {
			continue
		}
		out = append(out, agent.ToolSchema{
			Name:        spec.Name,
			Description: strings.TrimSpace(spec.Description),
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           toolArgProperties(spec.ArgTypes),
				"required":             append([]string(nil), spec.Required...),
				"additionalProperties": false,
			},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func toolArgProperties(argTypes map[string]tools.ArgType) map[string]any {
	if len(argTypes) == 0 {
		return map[string]any{}
	}
	props := make(map[string]any, len(argTypes))
	for name, argType := range argTypes {
		props[name] = map[string]any{"type": jsonTypeForArgType(argType)}
	}
	return props
}

func jsonTypeForArgType(argType tools.ArgType) string {
	switch argType {
	case tools.ArgTypeString:
		return "string"
	case tools.ArgTypeNumber:
		return "number"
	case tools.ArgTypeBool:
		return "boolean"
	case tools.ArgTypeArray:
		return "array"
	case tools.ArgTypeObject:
		return "object"
	default:
		return "string"
	}
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

func isAgentEnabled(cfg config.Config, agentID string) bool {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return false
	}
	if len(cfg.Agents.EnabledAgentIDs) > 0 {
		enabled := false
		for _, item := range cfg.Agents.EnabledAgentIDs {
			if strings.TrimSpace(item) == agentID {
				enabled = true
				break
			}
		}
		if !enabled {
			return false
		}
	}
	profile, ok := cfg.Agents.Profiles[agentID]
	if !ok || profile.Enabled == nil {
		return true
	}
	return *profile.Enabled
}

func resolveAgentModelConfig(cfg config.Config, agentID string) config.ModelConfig {
	selected := cfg.Model
	if !cfg.Agents.AllowAgentModelOverrides {
		return selected
	}
	profile, ok := cfg.Agents.Profiles[strings.TrimSpace(agentID)]
	if !ok {
		return selected
	}
	override := profile.Model
	if strings.TrimSpace(override.Provider) != "" {
		selected.Provider = strings.TrimSpace(override.Provider)
	}
	if strings.TrimSpace(override.Name) != "" {
		selected.Name = strings.TrimSpace(override.Name)
	}
	if override.Temperature != 0 {
		selected.Temperature = override.Temperature
	}
	if override.MaxTokens > 0 {
		selected.MaxTokens = override.MaxTokens
	}
	return selected
}

type sandboxShellExecutor struct {
	provider sandbox.Provider
}

// dockerPolicyEnforcer wraps a policy.Enforcer and overrides path resolution
// for docker mode.  In docker mode the workspace root is /workspace inside the
// container, so all relative paths are resolved against that root and absolute
// paths must be prefixed with /workspace.
type dockerPolicyEnforcer struct {
	inner *policy.Enforcer
}

func (d *dockerPolicyEnforcer) CheckTool(agentID, tool string) error {
	return d.inner.CheckTool(agentID, tool)
}

func (d *dockerPolicyEnforcer) ResolveReadPath(_, target string) (string, error) {
	return dockerResolvePath("", target)
}

func (d *dockerPolicyEnforcer) ResolveWritePath(_, target string) (string, error) {
	return dockerResolvePath("", target)
}

// dockerResolvePath resolves a path relative to the container workspace root
// (/workspace).  Absolute paths must reside within /workspace.
//
// Security hardening applied:
//   - Null bytes are stripped (prevents bypass via C-string truncation)
//   - Whitespace is trimmed
//   - Empty and excessively long paths are rejected
//   - Raw traversal sequences are blocked via policy.HasTraversal()
//   - Absolute paths outside /workspace are rejected
//   - Relative paths are joined with /workspace and the result is re-verified
//     so that tricky cleaned paths (e.g. "a/../../etc") are always caught
func dockerResolvePath(_, target string) (string, error) {
	// Strip null bytes — these can bypass string comparisons in some contexts.
	target = strings.ReplaceAll(target, "\x00", "")
	target = strings.TrimSpace(target)

	if target == "" {
		return "", fmt.Errorf("sandbox: docker: empty path")
	}
	if len(target) > 4096 {
		return "", fmt.Errorf("sandbox: docker: path too long (%d bytes)", len(target))
	}

	// Block traversal sequences in the raw (pre-clean) path.
	if policy.HasTraversal(target) {
		return "", fmt.Errorf("sandbox: docker: path traversal not allowed: %s", target)
	}

	var resolved string
	if filepath.IsAbs(target) {
		// Absolute path: clean it and verify it stays within /workspace.
		resolved = filepath.Clean(target)
	} else {
		// Relative path: join with /workspace and clean.
		resolved = filepath.Clean(filepath.Join("/workspace", target))
	}

	// Final containment guard: even after all cleaning the result must be
	// /workspace itself or a direct descendant.  This catches edge cases like
	// filepath.Clean("/workspace/a/../../etc") → "/etc".
	if resolved != "/workspace" && !strings.HasPrefix(resolved, "/workspace/") {
		return "", fmt.Errorf("sandbox: docker: path outside workspace: %s", target)
	}

	return resolved, nil
}

type subAgentRunner struct {
	engine *Engine
}

func (s *subAgentRunner) ExecuteSubAgent(ctx context.Context, input tools.AgentRunInput) (tools.AgentRunOutput, error) {
	if s == nil || s.engine == nil {
		return tools.AgentRunOutput{}, errors.New("runtime: engine is not configured")
	}
	result, err := s.engine.ExecuteWithInput(ctx, ExecuteInput{
		AgentID:           strings.TrimSpace(input.TargetAgentID),
		Message:           input.Message,
		Source:            strings.TrimSpace(input.Source),
		ThinkingMode:      strings.TrimSpace(input.ThinkingMode),
		AllowedTools:      input.AllowedTools,
		MaxToolIterations: input.MaxToolIterations,
	})
	if err != nil {
		return tools.AgentRunOutput{}, err
	}
	return tools.AgentRunOutput{
		RunID:        result.RunID,
		FinalText:    result.FinalText,
		ArtifactPath: result.ArtifactPath,
		DurationMS:   result.DurationMS,
		ToolCalls:    result.ToolCalls,
		Provider:     result.Provider,
		Model:        result.Model,
	}, nil
}

// agentSubAgentRunnerAdapter adapts the runtime subAgentRunner to the agent.SubAgentRunner interface.
// It resolves per-agent restrictions from SubAgentDefaults/SubAgentOverrides before delegating.
type agentSubAgentRunnerAdapter struct {
	runner            tools.AgentRunner
	subAgentDefaults  config.SubAgentRestrictions
	subAgentOverrides map[string]config.SubAgentRestrictions
}

// resolveRestrictions returns the effective SubAgentRestrictions for the given agent,
// checking overrides first and falling back to defaults.
func (a *agentSubAgentRunnerAdapter) resolveRestrictions(agentID string) config.SubAgentRestrictions {
	if override, ok := a.subAgentOverrides[agentID]; ok {
		// Merge: override fields take precedence, fall back to defaults for zero values.
		r := a.subAgentDefaults
		if len(override.AllowedTools) > 0 {
			r.AllowedTools = override.AllowedTools
		}
		if override.MaxToolIterations > 0 {
			r.MaxToolIterations = override.MaxToolIterations
		}
		if override.TimeoutMS > 0 {
			r.TimeoutMS = override.TimeoutMS
		}
		if override.ThinkingMode != "" {
			r.ThinkingMode = override.ThinkingMode
		}
		if override.DelegationMode != "" {
			r.DelegationMode = override.DelegationMode
		}
		return r
	}
	return a.subAgentDefaults
}

func (a *agentSubAgentRunnerAdapter) ExecuteSubAgent(ctx context.Context, task agent.DecomposedTask) (agent.SubAgentOutput, error) {
	restrictions := a.resolveRestrictions(task.AgentID)

	thinkingMode := task.ThinkingMode
	if thinkingMode == "" {
		thinkingMode = restrictions.ThinkingMode
	}
	if thinkingMode == "" {
		thinkingMode = "never" // default for subagents
	}

	// Apply timeout from restrictions if set.
	execCtx := ctx
	if restrictions.TimeoutMS > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(restrictions.TimeoutMS)*time.Millisecond)
		defer cancel()
	}

	result, err := a.runner.ExecuteSubAgent(execCtx, tools.AgentRunInput{
		CallerAgentID:     "",
		TargetAgentID:     task.AgentID,
		Message:           task.Message,
		TaskID:            task.TaskID,
		Source:            "subagent/delegation",
		ThinkingMode:      thinkingMode,
		AllowedTools:      restrictions.AllowedTools,
		MaxToolIterations: restrictions.MaxToolIterations,
	})
	if err != nil {
		return agent.SubAgentOutput{Success: false, Error: err.Error()}, err
	}
	return agent.SubAgentOutput{
		RunID:     result.RunID,
		FinalText: result.FinalText,
		Success:   true,
	}, nil
}

func (s *sandboxShellExecutor) Exec(_ context.Context, command string, args []string, workDir string) (string, string, int, error) {
	result, err := s.provider.Exec(sandbox.Command{Name: command, Args: args, WorkDir: workDir})
	return result.Stdout, result.Stderr, result.ExitCode, err
}

// sandboxProviderAdapter bridges sandbox.Provider (which uses sandbox.FileInfo)
// to tools.SandboxFileOps (which uses tools.SandboxFileInfo), avoiding an
// import cycle between the tools and sandbox packages.
type sandboxProviderAdapter struct {
	provider sandbox.Provider
}

func (a *sandboxProviderAdapter) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return a.provider.ReadFile(ctx, path)
}

func (a *sandboxProviderAdapter) WriteFile(ctx context.Context, path string, data []byte, perm os.FileMode) error {
	return a.provider.WriteFile(ctx, path, data, perm)
}

func (a *sandboxProviderAdapter) ListDir(ctx context.Context, path string) ([]tools.SandboxFileInfo, error) {
	entries, err := a.provider.ListDir(ctx, path)
	if err != nil {
		return nil, err
	}
	out := make([]tools.SandboxFileInfo, len(entries))
	for i, e := range entries {
		out[i] = tools.SandboxFileInfo{
			Name:  e.Name,
			IsDir: e.IsDir,
			Size:  e.Size,
		}
	}
	return out, nil
}

func (a *sandboxProviderAdapter) MkdirAll(ctx context.Context, path string, perm os.FileMode) error {
	return a.provider.MkdirAll(ctx, path, perm)
}

func (a *sandboxProviderAdapter) Remove(ctx context.Context, path string, recursive bool) error {
	return a.provider.Remove(ctx, path, recursive)
}

func (a *sandboxProviderAdapter) Rename(ctx context.Context, src, dst string) error {
	return a.provider.Rename(ctx, src, dst)
}

func (a *sandboxProviderAdapter) Lstat(ctx context.Context, path string) (tools.SandboxFileInfo, bool, error) {
	info, exists, err := a.provider.Lstat(ctx, path)
	if err != nil {
		return tools.SandboxFileInfo{}, false, err
	}
	return tools.SandboxFileInfo{
		Name:  info.Name,
		IsDir: info.IsDir,
		Size:  info.Size,
	}, exists, nil
}

func (a *sandboxProviderAdapter) EvalSymlinks(ctx context.Context, path string) (string, error) {
	return a.provider.EvalSymlinks(ctx, path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (e *Engine) memoryPromptExtender(cfg config.Config, agentID, runID string) agent.SystemPromptExtender {
	if !cfg.Memory.Enabled {
		return nil
	}
	return func(ctx context.Context, basePrompt string, messages []agent.ChatMessage, message string, _ []agent.ToolCallResult) string {
		block, err := e.buildMemoryRecallBlock(ctx, cfg, agentID, message, messages)
		if err != nil {
			slog.Warn("runtime: memory recall unavailable", "run_id", runID, "error", err)
			return basePrompt
		}
		if strings.TrimSpace(block) == "" {
			return basePrompt
		}
		if strings.TrimSpace(basePrompt) == "" {
			return block
		}
		return strings.TrimSpace(basePrompt) + "\n\n" + block
	}
}

func (e *Engine) buildMemoryRecallBlock(ctx context.Context, cfg config.Config, agentID, message string, messages []agent.ChatMessage) (string, error) {
	if !cfg.Memory.Enabled {
		return "", nil
	}
	storePath := filepath.Join(e.agentsDir, agentID, "memory", "memory.db")
	store, err := memorystore.OpenSQLite(storePath, agentID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer func() { _ = store.Close() }()

	query := recallQueryFromMessages(message, messages)
	limit := cfg.Memory.MaxWorkingItems
	if limit <= 0 || limit > 24 {
		limit = 24
	}
	items, err := store.Search(ctx, memory.SearchParams{
		Query:         query,
		Limit:         limit,
		MinImportance: 3,
		Status:        memory.MemoryStatusActive,
	})
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		items, err = store.Search(ctx, memory.SearchParams{
			Query:         "",
			Limit:         limit,
			MinImportance: 3,
			Status:        memory.MemoryStatusActive,
		})
		if err != nil {
			return "", err
		}
	}
	if len(items) == 0 {
		return "", nil
	}

	maxChars := cfg.Memory.MaxPromptTokens * 4
	if maxChars <= 0 {
		maxChars = 4800
	}
	return formatRecallBlock(items, maxChars), nil
}

func recallQueryFromMessages(message string, messages []agent.ChatMessage) string {
	parts := []string{strings.TrimSpace(message)}
	for i := len(messages) - 1; i >= 0 && len(parts) < 3; i-- {
		if !strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			continue
		}
		content := strings.TrimSpace(messages[i].Content)
		if content != "" {
			parts = append(parts, content)
		}
	}
	joined := strings.Join(parts, " ")
	joined = strings.TrimSpace(joined)
	if len(joined) > 320 {
		joined = joined[:320]
	}
	return joined
}

func formatRecallBlock(items []memory.MemoryItem, maxChars int) string {
	if len(items) == 0 {
		return ""
	}
	sorted := append([]memory.MemoryItem(nil), items...)
	now := time.Now().UTC()
	sort.Slice(sorted, func(i, j int) bool {
		si := float64(sorted[i].Importance)*2 + recencyBoost(now, sorted[i].UpdatedAt)
		sj := float64(sorted[j].Importance)*2 + recencyBoost(now, sorted[j].UpdatedAt)
		if si == sj {
			return sorted[i].UpdatedAt.After(sorted[j].UpdatedAt)
		}
		return si > sj
	})

	lines := []string{"--- RELEVANT MEMORY ---"}
	used := len(lines[0]) + 1
	for _, item := range sorted {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = "unknown"
		}
		if len(id) > 8 {
			id = id[:8]
		}
		text := policy.RedactString(strings.TrimSpace(item.Content))
		if text == "" {
			continue
		}
		line := fmt.Sprintf("[MEM-%s] %s", id, text)
		if len(line) > 420 {
			line = line[:420] + "..."
		}
		if used+len(line)+1 > maxChars {
			break
		}
		lines = append(lines, line)
		used += len(line) + 1
	}
	if len(lines) == 1 {
		return ""
	}
	lines = append(lines, "------------------------")
	if used+len(lines[len(lines)-1]) > maxChars {
		if len(lines) <= 2 {
			return ""
		}
		return strings.Join(lines[:len(lines)-1], "\n")
	}
	return strings.Join(lines, "\n")
}

func recencyBoost(now, ts time.Time) float64 {
	if ts.IsZero() {
		return 0
	}
	age := now.Sub(ts)
	if age < 24*time.Hour {
		return 3
	}
	if age < 7*24*time.Hour {
		return 2
	}
	if age < 30*24*time.Hour {
		return 1
	}
	return 0
}

type proactiveMemorySignal struct {
	Trigger bool
	Reason  string
}

type proactiveSessionContext struct {
	SessionID string
	Channel   string
	UserID    string
}

func (e *Engine) maybeTriggerProactiveMemoryHook(ctx context.Context, cfg config.Config, registry *tools.Registry, agentID, sessionID, runID string, rec agent.ToolCallRecord) {
	if !cfg.Memory.Enabled || !cfg.Memory.ProactiveEnabled {
		return
	}
	if registry == nil || strings.TrimSpace(rec.Result.Error) != "" {
		return
	}
	signal := proactiveSignalFromToolOutput(rec.Request.Name, rec.Result.Output)
	if !signal.Trigger {
		return
	}
	sessionCtx, ok := e.proactiveContextFromSession(sessionID)
	if !ok {
		slog.Debug("runtime: skip proactive hook (missing channel/user/session context)", "run_id", runID, "tool", rec.Request.Name)
		return
	}
	toAgentID := strings.TrimSpace(cfg.Chat.DefaultAgentID)
	if toAgentID == "" {
		toAgentID = "default"
	}
	msg := fmt.Sprintf("Proactive memory signal: %s\nchannel=%s\nuser_id=%s\nsession_id=%s\nrun_id=%s", signal.Reason, sessionCtx.Channel, sessionCtx.UserID, sessionCtx.SessionID, runID)
	_, err := registry.Execute(ctx, agentID, "agent.message.send", e.workspaceDir, map[string]any{
		"to_agent_id": toAgentID,
		"subject":     "memory.proactive",
		"task_id":     sessionCtx.SessionID,
		"session_id":  sessionCtx.SessionID,
		"channel":     sessionCtx.Channel,
		"user_id":     sessionCtx.UserID,
		"message":     msg,
	})
	if err != nil {
		slog.Warn("runtime: proactive hook delivery failed", "run_id", runID, "tool", rec.Request.Name, "error", err)
	}
}

func (e *Engine) proactiveContextFromSession(sessionID string) (proactiveSessionContext, bool) {
	if e == nil || e.chatStore == nil {
		return proactiveSessionContext{}, false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return proactiveSessionContext{}, false
	}
	session, err := e.chatStore.GetSession(sessionID)
	if err != nil {
		return proactiveSessionContext{}, false
	}
	channel := strings.TrimSpace(session.Channel)
	userID := strings.TrimSpace(session.UserID)
	if channel == "" || userID == "" {
		return proactiveSessionContext{}, false
	}
	return proactiveSessionContext{SessionID: sessionID, Channel: channel, UserID: userID}, true
}

func proactiveSignalFromToolOutput(toolName, output string) proactiveMemorySignal {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name != "memory.checkpoint" && name != "memory.maintenance" {
		return proactiveMemorySignal{}
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return proactiveMemorySignal{}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return proactiveMemorySignal{}
	}
	if name == "memory.maintenance" {
		stale := int(numberField(payload, "archived_stale_count"))
		if stale > 0 {
			return proactiveMemorySignal{Trigger: true, Reason: fmt.Sprintf("maintenance archived %d stale memory items", stale)}
		}
		return proactiveMemorySignal{}
	}

	result, _ := payload["result"].(map[string]any)
	newItems, _ := result["new_items"].([]any)
	highImportance := 0
	preferenceReminder := false
	for _, raw := range newItems {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		importance := int(numberField(item, "importance"))
		if importance >= 4 {
			highImportance++
		}
		kind := strings.ToLower(strings.TrimSpace(stringField(item, "kind")))
		content := strings.ToLower(strings.TrimSpace(stringField(item, "content")))
		if kind == "preference" && containsReminderPreference(content) {
			preferenceReminder = true
		}
	}
	if highImportance > 0 {
		return proactiveMemorySignal{Trigger: true, Reason: fmt.Sprintf("checkpoint created %d high-importance memory items", highImportance)}
	}
	if preferenceReminder {
		return proactiveMemorySignal{Trigger: true, Reason: "checkpoint captured reminder preference"}
	}
	return proactiveMemorySignal{}
}

func numberField(value map[string]any, key string) float64 {
	if len(value) == 0 {
		return 0
	}
	raw, ok := value[key]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

func stringField(value map[string]any, key string) string {
	if len(value) == 0 {
		return ""
	}
	raw, ok := value[key]
	if !ok {
		return ""
	}
	s, _ := raw.(string)
	return s
}

func containsReminderPreference(content string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	markers := []string{"remind me", "reminder", "notify me", "follow up", "follow-up"}
	for _, marker := range markers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

type runMemoryEventInput struct {
	AgentID   string
	RunID     string
	SessionID string
	Source    string
	Message   string
	Output    agent.RunOutput
	RunErr    error
}

func ingestRunMemoryEvents(ctx context.Context, manager *memory.Manager, in runMemoryEventInput) {
	if manager == nil {
		return
	}

	now := time.Now().UTC()
	baseMetadata := map[string]any{}
	if in.Source != "" {
		baseMetadata["source"] = in.Source
	}
	if in.AgentID != "" {
		baseMetadata["agent_id"] = in.AgentID
	}

	ingest := func(event memory.Event) {
		if err := manager.IngestEvent(ctx, event); err != nil && !errors.Is(err, memory.ErrQueueFull) {
			slog.Warn("runtime: memory ingest failure", "run_id", in.RunID, "event_type", event.Type, "error", err)
		}
	}

	if strings.HasPrefix(strings.TrimSpace(in.Source), "scheduler") {
		ingest(memory.Event{
			Type:      memory.EventTypeSchedulerRun,
			Text:      policy.RedactString(in.Message),
			SessionID: in.SessionID,
			RunID:     in.RunID,
			Timestamp: now,
			Metadata:  cloneMetadata(baseMetadata),
		})
	}

	ingest(memory.Event{
		Type:      memory.EventTypeUserMessage,
		Text:      policy.RedactString(in.Message),
		SessionID: in.SessionID,
		RunID:     in.RunID,
		Timestamp: now,
		Metadata:  cloneMetadata(baseMetadata),
	})

	if strings.TrimSpace(in.Output.FinalText) != "" {
		ingest(memory.Event{
			Type:      memory.EventTypeAssistantOutput,
			Text:      policy.RedactString(in.Output.FinalText),
			SessionID: in.SessionID,
			RunID:     in.RunID,
			Timestamp: now,
			Metadata:  cloneMetadata(baseMetadata),
		})
	}

	for i, rec := range in.Output.ToolCalls {
		callMeta := cloneMetadata(baseMetadata)
		callMeta["tool"] = rec.Request.Name
		callMeta["tool_call_id"] = rec.Request.ID
		callMeta["tool_call_index"] = i
		callMeta["arguments"] = policy.RedactValue(rec.Request.Arguments)
		callText := renderMemoryEventText(map[string]any{
			"tool":      rec.Request.Name,
			"id":        rec.Request.ID,
			"arguments": policy.RedactValue(rec.Request.Arguments),
		})
		ingest(memory.Event{
			Type:      memory.EventTypeToolCall,
			Text:      callText,
			SessionID: in.SessionID,
			RunID:     in.RunID,
			Timestamp: rec.StartedAt,
			Metadata:  callMeta,
		})

		resultMeta := cloneMetadata(baseMetadata)
		resultMeta["tool"] = rec.Request.Name
		resultMeta["tool_call_id"] = rec.Request.ID
		resultMeta["tool_result_id"] = rec.Result.ID
		resultMeta["tool_call_index"] = i
		if strings.TrimSpace(rec.Result.Error) != "" {
			resultMeta["error"] = policy.RedactString(rec.Result.Error)
		}
		resultText := policy.RedactString(rec.Result.Output)
		if resultText == "" && strings.TrimSpace(rec.Result.Error) != "" {
			resultText = policy.RedactString(rec.Result.Error)
		}
		ingest(memory.Event{
			Type:      memory.EventTypeToolResult,
			Text:      resultText,
			SessionID: in.SessionID,
			RunID:     in.RunID,
			Timestamp: rec.CompletedAt,
			Metadata:  resultMeta,
		})

		if strings.TrimSpace(rec.Result.Error) != "" {
			errMeta := cloneMetadata(baseMetadata)
			errMeta["tool"] = rec.Request.Name
			errMeta["tool_call_id"] = rec.Request.ID
			errMeta["tool_result_id"] = rec.Result.ID
			errMeta["tool_call_index"] = i
			ingest(memory.Event{
				Type:      memory.EventTypeError,
				Text:      policy.RedactString(rec.Result.Error),
				SessionID: in.SessionID,
				RunID:     in.RunID,
				Timestamp: rec.CompletedAt,
				Metadata:  errMeta,
			})
		}
	}

	if in.RunErr != nil {
		errMeta := cloneMetadata(baseMetadata)
		errMeta["error_type"] = "run_error"
		ingest(memory.Event{
			Type:      memory.EventTypeError,
			Text:      policy.RedactString(in.RunErr.Error()),
			SessionID: in.SessionID,
			RunID:     in.RunID,
			Timestamp: now,
			Metadata:  errMeta,
		})
	}
}

func renderMemoryEventText(payload map[string]any) string {
	if len(payload) == 0 {
		return ""
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return policy.RedactString(string(b))
}

func cloneMetadata(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	clone := make(map[string]any, len(meta))
	for k, v := range meta {
		clone[k] = v
	}
	return clone
}
