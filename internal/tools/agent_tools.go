package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"openclawssy/internal/agentdocs"
	"openclawssy/internal/chatstore"
	clawdefuckifierpkg "openclawssy/internal/clawdefuckifier"
	"openclawssy/internal/config"
	"openclawssy/internal/instances"
)

const (
	defaultAgentListLimit = 50
	maxAgentListLimit     = 200
)

type AgentRunInput struct {
	InstanceID        string
	CallerAgentID     string
	TargetAgentID     string
	MessageID         string
	ParentRunID       string
	Message           string
	TaskID            string
	Source            string
	ThinkingMode      string
	AllowedTools      []string // Restricts which tools the subagent may use.
	MaxToolIterations int      // Caps tool iterations for this subagent run (0 = use default).
	TimeoutMS         int      // Applies a run timeout to this subagent when > 0.
}

type AgentRunOutput struct {
	RunID        string
	FinalText    string
	ArtifactPath string
	DurationMS   int64
	ToolCalls    int
	Provider     string
	Model        string
	Status       string
	MessageID    string
}

const (
	agentMessageStatusQueued       = "queued"
	agentMessageStatusAcknowledged = "acknowledged"
	agentMessageStatusRunning      = "running"
	agentMessageStatusCompleted    = "completed"
	agentMessageStatusFailed       = "failed"
)

type agentMessageEnvelope struct {
	MessageID       string `json:"message_id,omitempty"`
	Status          string `json:"status,omitempty"`
	InstanceID      string `json:"instance_id,omitempty"`
	FromAgentID     string `json:"from_agent_id,omitempty"`
	ToAgentID       string `json:"to_agent_id,omitempty"`
	Subject         string `json:"subject,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	SourceSessionID string `json:"source_session_id,omitempty"`
	Channel         string `json:"channel,omitempty"`
	UserID          string `json:"user_id,omitempty"`
	Message         string `json:"message,omitempty"`
	RelatedRunID    string `json:"related_run_id,omitempty"`
	Note            string `json:"note,omitempty"`
	Error           string `json:"error,omitempty"`
	SentAt          string `json:"sent_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type AgentRunner interface {
	ExecuteSubAgent(ctx context.Context, input AgentRunInput) (AgentRunOutput, error)
}

func registerAgentTools(reg *Registry, agentsPath, configPath, workspaceRoot string, runner AgentRunner) error {
	if err := reg.Register(ToolSpec{
		Name:        "agent.list",
		Description: "List available agents",
		ArgTypes: map[string]ArgType{
			"limit":  ArgTypeNumber,
			"offset": ArgTypeNumber,
		},
	}, agentList(agentsPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "agent.create",
		Description: "Create an agent scaffold",
		Required:    []string{"agent_id"},
		ArgTypes: map[string]ArgType{
			"agent_id": ArgTypeString,
			"force":    ArgTypeBool,
		},
	}, agentCreate(agentsPath, configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "agent.switch",
		Description: "Switch default agent in config",
		Required:    []string{"agent_id"},
		ArgTypes: map[string]ArgType{
			"agent_id":          ArgTypeString,
			"scope":             ArgTypeString,
			"create_if_missing": ArgTypeBool,
		},
	}, agentSwitch(agentsPath, configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "agent.profile.get",
		Description: "Get agent runtime profile configuration",
		ArgTypes: map[string]ArgType{
			"agent_id": ArgTypeString,
		},
	}, agentProfileGet(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "agent.profile.set",
		Description: "Update agent runtime profile configuration",
		Required:    []string{"agent_id"},
		ArgTypes: map[string]ArgType{
			"agent_id":             ArgTypeString,
			"enabled":              ArgTypeBool,
			"self_improvement":     ArgTypeBool,
			"model_provider":       ArgTypeString,
			"model_name":           ArgTypeString,
			"model_temperature":    ArgTypeNumber,
			"model_max_tokens":     ArgTypeNumber,
			"clear_model_override": ArgTypeBool,
		},
	}, agentProfileSet(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "agent.message.send",
		Description: "Send a message to another agent inbox",
		Required:    []string{"to_agent_id", "message"},
		ArgTypes: map[string]ArgType{
			"to_agent_id":       ArgTypeString,
			"message":           ArgTypeString,
			"task_id":           ArgTypeString,
			"subject":           ArgTypeString,
			"channel":           ArgTypeString,
			"user_id":           ArgTypeString,
			"session_id":        ArgTypeString,
			"source_session_id": ArgTypeString,
			"message_id":        ArgTypeString,
			"parent_run_id":     ArgTypeString,
			"auto_run":          ArgTypeBool,
		},
	}, agentMessageSend(agentsPath, configPath, runner)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "agent.message.inbox",
		Description: "Read inter-agent inbox messages",
		ArgTypes: map[string]ArgType{
			"agent_id": ArgTypeString,
			"limit":    ArgTypeNumber,
		},
	}, agentMessageInbox(agentsPath, configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "agent.run",
		Description: "Run a subagent task and return structured output",
		Required:    []string{"agent_id", "message"},
		ArgTypes: map[string]ArgType{
			"agent_id":            ArgTypeString,
			"message":             ArgTypeString,
			"message_id":          ArgTypeString,
			"parent_run_id":       ArgTypeString,
			"task_id":             ArgTypeString,
			"source_session_id":   ArgTypeString,
			"inbox_session_id":    ArgTypeString,
			"source":              ArgTypeString,
			"thinking_mode":       ArgTypeString,
			"allowed_tools":       ArgTypeArray,
			"max_tool_iterations": ArgTypeNumber,
			"timeout_ms":          ArgTypeNumber,
		},
	}, agentRun(agentsPath, configPath, runner)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "agent.prompt.read",
		Description: "Read an agent control-plane prompt file",
		Required:    []string{"file"},
		ArgTypes: map[string]ArgType{
			"agent_id": ArgTypeString,
			"file":     ArgTypeString,
		},
	}, agentPromptRead(agentsPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "agent.prompt.update",
		Description: "Update an agent control-plane prompt file",
		Required:    []string{"file", "content"},
		ArgTypes: map[string]ArgType{
			"agent_id": ArgTypeString,
			"file":     ArgTypeString,
			"content":  ArgTypeString,
		},
	}, agentPromptUpdate(agentsPath, configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "agent.prompt.suggest",
		Description: "Suggest system prompt improvements for an agent",
		ArgTypes: map[string]ArgType{
			"agent_id": ArgTypeString,
			"goal":     ArgTypeString,
			"focus":    ArgTypeString,
		},
	}, agentPromptSuggest(agentsPath, workspaceRoot)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "agent.identity.set",
		Description: "Set assistant/user names by writing SOUL.md",
		Required:    []string{"assistant_name", "user_name"},
		ArgTypes: map[string]ArgType{
			"agent_id":       ArgTypeString,
			"assistant_name": ArgTypeString,
			"user_name":      ArgTypeString,
		},
	}, agentIdentitySet(agentsPath)); err != nil {
		return err
	}
	return nil
}

func agentList(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		agentsRoot, err := resolveOpenClawssyPath(req.Workspace, configuredPath, "agents", "agents")
		if err != nil {
			return nil, err
		}

		entries, err := os.ReadDir(agentsRoot)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				sliced, meta := paginate([]string{}, req.Args, defaultAgentListLimit, maxAgentListLimit)
				meta["items"] = sliced
				return meta, nil
			}
			return nil, err
		}

		items := make([]string, 0, len(entries))
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			items = append(items, entry.Name())
		}
		sort.Strings(items)

		sliced, meta := paginate(items, req.Args, defaultAgentListLimit, maxAgentListLimit)
		meta["items"] = sliced
		return meta, nil
	}
}

func agentCreate(configuredPath, configPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		agentID, err := validatedAgentID(valueString(req.Args, "agent_id"))
		if err != nil {
			return nil, err
		}
		agentsRoot, err := resolveOpenClawssyPath(req.Workspace, configuredPath, "agents", "agents")
		if err != nil {
			return nil, err
		}

		agentRoot := filepath.Join(agentsRoot, agentID)
		_, statErr := os.Stat(agentRoot)
		existed := statErr == nil
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return nil, statErr
		}

		seeded, err := createAgentScaffold(agentRoot, getBoolArg(req.Args, "force", false))
		if err != nil {
			return nil, err
		}
		if err := maybeBootstrapClawDefuckifier(req.Workspace, configPath, agentID); err != nil {
			return nil, err
		}

		return map[string]any{
			"agent_id":     agentID,
			"path":         agentRoot,
			"created":      !existed,
			"seeded_files": seeded,
			"count":        len(seeded),
		}, nil
	}
}

func agentSwitch(agentsPath, configPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		agentID, err := validatedAgentID(valueString(req.Args, "agent_id"))
		if err != nil {
			return nil, err
		}

		agentsRoot, err := resolveOpenClawssyPath(req.Workspace, agentsPath, "agents", "agents")
		if err != nil {
			return nil, err
		}
		agentRoot := filepath.Join(agentsRoot, agentID)
		if _, err := os.Stat(agentRoot); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			if !getBoolArg(req.Args, "create_if_missing", false) {
				return nil, fmt.Errorf("agent does not exist: %s", agentID)
			}
			if _, err := createAgentScaffold(agentRoot, false); err != nil {
				return nil, err
			}
			if err := maybeBootstrapClawDefuckifier(req.Workspace, configPath, agentID); err != nil {
				return nil, err
			}
		}

		scope := strings.ToLower(strings.TrimSpace(valueString(req.Args, "scope")))
		if scope == "" {
			scope = "both"
		}
		if scope != "chat" && scope != "discord" && scope != "both" {
			return nil, errors.New("scope must be one of: chat, discord, both")
		}

		cfgPath, err := resolveOpenClawssyPath(req.Workspace, configPath, "config", "config.json")
		if err != nil {
			return nil, err
		}
		cfg, err := config.LoadOrDefault(cfgPath)
		if err != nil {
			return nil, err
		}

		updatedScopes := make([]string, 0, 2)
		if scope == "chat" || scope == "both" {
			cfg.Chat.DefaultAgentID = agentID
			updatedScopes = append(updatedScopes, "chat")
		}
		if scope == "discord" || scope == "both" {
			cfg.Discord.DefaultAgentID = agentID
			updatedScopes = append(updatedScopes, "discord")
		}

		if err := config.Save(cfgPath, cfg); err != nil {
			return nil, err
		}

		return map[string]any{
			"agent_id":              agentID,
			"scope":                 scope,
			"updated_scopes":        updatedScopes,
			"chat_default_agent":    cfg.Chat.DefaultAgentID,
			"discord_default_agent": cfg.Discord.DefaultAgentID,
		}, nil
	}
}

func agentProfileGet(configPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		cfgPath, err := resolveOpenClawssyPath(req.Workspace, configPath, "config", "config.json")
		if err != nil {
			return nil, err
		}
		cfg, err := config.LoadOrDefault(cfgPath)
		if err != nil {
			return nil, err
		}
		agentID := strings.TrimSpace(valueString(req.Args, "agent_id"))
		if agentID == "" {
			profiles := make(map[string]config.AgentProfile, len(cfg.Agents.Profiles))
			for id, profile := range cfg.Agents.Profiles {
				profiles[id] = profile
			}
			return map[string]any{
				"allow_agent_model_overrides": cfg.Agents.AllowAgentModelOverrides,
				"self_improvement_enabled":    cfg.Agents.SelfImprovementEnabled,
				"allow_inter_agent_messaging": cfg.Agents.AllowInterAgentMessaging,
				"enabled_agent_ids":           cfg.Agents.EnabledAgentIDs,
				"profiles":                    profiles,
			}, nil
		}
		if _, err := validatedAgentID(agentID); err != nil {
			return nil, err
		}
		profile, ok := cfg.Agents.Profiles[agentID]
		if !ok {
			profile = config.AgentProfile{}
		}
		return map[string]any{
			"agent_id":                    agentID,
			"profile":                     profile,
			"profile_exists":              ok,
			"allow_agent_model_overrides": cfg.Agents.AllowAgentModelOverrides,
			"self_improvement_enabled":    cfg.Agents.SelfImprovementEnabled,
			"agent_is_allowlisted":        containsTrimmedString(cfg.Agents.EnabledAgentIDs, agentID),
		}, nil
	}
}

func agentProfileSet(configPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		agentID, err := validatedAgentID(valueString(req.Args, "agent_id"))
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.AgentID) != agentID && !hasPolicyAdmin(req) {
			return nil, errors.New("cross-agent profile updates require policy.admin capability")
		}
		cfgPath, err := resolveOpenClawssyPath(req.Workspace, configPath, "config", "config.json")
		if err != nil {
			return nil, err
		}
		cfg, err := config.LoadOrDefault(cfgPath)
		if err != nil {
			return nil, err
		}

		profile := cfg.Agents.Profiles[agentID]
		if profile.Enabled == nil {
			defaultEnabled := true
			profile.Enabled = &defaultEnabled
		}

		if raw, ok := req.Args["enabled"]; ok {
			value, ok := raw.(bool)
			if !ok {
				return nil, errors.New("enabled must be a bool")
			}
			profile.Enabled = &value
		}
		if raw, ok := req.Args["self_improvement"]; ok {
			value, ok := raw.(bool)
			if !ok {
				return nil, errors.New("self_improvement must be a bool")
			}
			profile.SelfImprovement = value
		}

		if getBoolArg(req.Args, "clear_model_override", false) {
			profile.Model = config.ModelConfig{}
		}
		if provider := strings.TrimSpace(valueString(req.Args, "model_provider")); provider != "" {
			profile.Model.Provider = provider
		}
		if modelName := strings.TrimSpace(valueString(req.Args, "model_name")); modelName != "" {
			profile.Model.Name = modelName
		}
		if raw, ok := req.Args["model_temperature"]; ok {
			temp, err := requireFloat(raw, "model_temperature")
			if err != nil {
				return nil, err
			}
			profile.Model.Temperature = temp
		}
		if raw, ok := req.Args["model_max_tokens"]; ok {
			tokens, err := requireInt(raw, "model_max_tokens")
			if err != nil {
				return nil, err
			}
			profile.Model.MaxTokens = tokens
		}

		cfg.Agents.Profiles[agentID] = profile
		if err := config.Save(cfgPath, cfg); err != nil {
			return nil, err
		}

		return map[string]any{
			"agent_id": agentID,
			"profile":  profile,
		}, nil
	}
}

func agentMessageSend(agentsPath, configPath string, runner AgentRunner) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		instanceID, rootDir, instanceManifest, err := resolveAgentMessagingContext(req, configPath)
		if err != nil {
			return nil, err
		}
		if !instanceManifest.Messaging.Enabled || !instanceManifest.Messaging.AllowInterAgentMessaging {
			return nil, errors.New("inter-agent messaging is disabled for this instance")
		}
		cfgPath, err := resolveOpenClawssyPath(req.Workspace, configPath, "config", "config.json")
		if err != nil {
			return nil, err
		}
		cfg, err := config.LoadOrDefault(cfgPath)
		if err != nil {
			return nil, err
		}
		if !cfg.Agents.AllowInterAgentMessaging {
			return nil, errors.New("inter-agent messaging is disabled by config")
		}

		fromAgentID, err := validatedAgentID(req.AgentID)
		if err != nil {
			return nil, errors.New("request agent id is invalid")
		}
		fromManifest, err := instances.LoadAgentManifest(rootDir, instanceID, fromAgentID)
		if err != nil {
			return nil, fmt.Errorf("sender agent is not available in instance %q: %w", instanceID, err)
		}
		toAgentID, err := validatedAgentID(valueString(req.Args, "to_agent_id"))
		if err != nil {
			return nil, err
		}
		toManifest, err := instances.LoadAgentManifest(rootDir, instanceID, toAgentID)
		if err != nil {
			return nil, fmt.Errorf("recipient agent is not available in instance %q: %w", instanceID, err)
		}
		if !toManifest.Enabled {
			return nil, fmt.Errorf("recipient agent %q is inactive in instance %q", toAgentID, instanceID)
		}
		if err := validateMessagingPermission(fromManifest.Communication.CanMessage, toAgentID, "recipient agent is not allowed by sender communication.can_message"); err != nil {
			return nil, err
		}
		if err := validateMessagingPermission(toManifest.Communication.CanReceiveFrom, fromAgentID, "sender agent is not allowed by recipient communication.can_receive_from"); err != nil {
			return nil, err
		}
		message := strings.TrimSpace(valueString(req.Args, "message"))
		if message == "" {
			return nil, errors.New("message is required")
		}
		taskID := strings.TrimSpace(valueString(req.Args, "task_id"))
		sessionID := strings.TrimSpace(valueString(req.Args, "session_id"))
		if taskID == "" {
			if sessionID != "" {
				taskID = sessionID
			} else {
				taskID = "shared"
			}
		}
		subject := strings.TrimSpace(valueString(req.Args, "subject"))
		sourceChannel := strings.TrimSpace(valueString(req.Args, "channel"))
		sourceUserID := strings.TrimSpace(valueString(req.Args, "user_id"))
		now := time.Now().UTC()
		messageID := strings.TrimSpace(valueString(req.Args, "message_id"))
		if messageID == "" {
			messageID, err = newAgentMessageID(now)
			if err != nil {
				return nil, err
			}
		}

		store, err := openAgentChatStore(req.Workspace, agentsPath)
		if err != nil {
			return nil, err
		}
		sourceSessionID := strings.TrimSpace(valueString(req.Args, "source_session_id"))
		channel := "agent-mail"
		sessions, err := store.ListSessions(toAgentID, fromAgentID, taskID, channel)
		if err != nil {
			return nil, err
		}
		var session chatstore.Session
		if len(sessions) > 0 && !sessions[0].IsClosed() {
			session = sessions[0]
		} else {
			session, err = store.CreateSession(chatstore.CreateSessionInput{
				AgentID: toAgentID,
				Channel: channel,
				UserID:  fromAgentID,
				RoomID:  taskID,
				Title:   "inter-agent: " + fromAgentID + " -> " + toAgentID,
			})
			if err != nil {
				return nil, err
			}
		}

		envelope := agentMessageEnvelope{
			MessageID:       messageID,
			Status:          agentMessageStatusQueued,
			InstanceID:      instanceID,
			FromAgentID:     fromAgentID,
			ToAgentID:       toAgentID,
			Subject:         subject,
			TaskID:          taskID,
			SessionID:       sessionID,
			SourceSessionID: firstNonEmptyTrimmed(sourceSessionID, sessionID),
			Channel:         sourceChannel,
			UserID:          sourceUserID,
			Message:         message,
			SentAt:          now.Format(time.RFC3339),
			UpdatedAt:       now.Format(time.RFC3339),
		}
		raw, _ := json.Marshal(envelope)
		if err := store.AppendMessage(session.SessionID, chatstore.Message{
			Role:            "user",
			Content:         string(raw),
			TS:              now,
			MessageID:       messageID,
			Status:          agentMessageStatusQueued,
			InstanceID:      instanceID,
			FromAgentID:     fromAgentID,
			ToAgentID:       toAgentID,
			TaskID:          taskID,
			Subject:         subject,
			Channel:         sourceChannel,
			UserID:          sourceUserID,
			SourceSessionID: firstNonEmptyTrimmed(sourceSessionID, sessionID),
			UpdatedAt:       now,
		}); err != nil {
			return nil, err
		}
		_ = appendAgentMessageStatus(store, session.SessionID, agentMessageEnvelope{
			MessageID:       messageID,
			Status:          agentMessageStatusAcknowledged,
			InstanceID:      instanceID,
			FromAgentID:     fromAgentID,
			ToAgentID:       toAgentID,
			Subject:         subject,
			TaskID:          taskID,
			SourceSessionID: firstNonEmptyTrimmed(sourceSessionID, sessionID),
			Channel:         sourceChannel,
			UserID:          sourceUserID,
			Note:            "message accepted into inbox",
			SentAt:          now.Format(time.RFC3339),
		})

		autoRun := getBoolArg(req.Args, "auto_run", false)
		runID := ""
		lifecycleStatus := agentMessageStatusQueued
		if autoRun {
			if runner == nil {
				return nil, errors.New("agent auto-run is not configured")
			}
			out, runErr := runAgentMessageLifecycle(ctx, runner, store, req, agentMessageEnvelope{
				MessageID:       messageID,
				Status:          agentMessageStatusAcknowledged,
				InstanceID:      instanceID,
				FromAgentID:     fromAgentID,
				ToAgentID:       toAgentID,
				Subject:         subject,
				TaskID:          taskID,
				SessionID:       sessionID,
				SourceSessionID: firstNonEmptyTrimmed(sourceSessionID, sessionID),
				Channel:         sourceChannel,
				UserID:          sourceUserID,
				Message:         message,
				SentAt:          now.Format(time.RFC3339),
			}, session.SessionID)
			runID = strings.TrimSpace(out.RunID)
			lifecycleStatus = firstNonEmptyTrimmed(out.Status, agentMessageStatusCompleted)
			if runErr != nil {
				return map[string]any{
					"sent":              true,
					"message_id":        messageID,
					"status":            lifecycleStatus,
					"instance_id":       instanceID,
					"session_id":        session.SessionID,
					"inbox_session_id":  session.SessionID,
					"source_session_id": sessionID,
					"from_agent_id":     fromAgentID,
					"to_agent_id":       toAgentID,
					"task_id":           taskID,
					"channel":           sourceChannel,
					"user_id":           sourceUserID,
					"run_id":            runID,
				}, runErr
			}
		}

		return map[string]any{
			"sent":              true,
			"message_id":        messageID,
			"status":            lifecycleStatus,
			"instance_id":       instanceID,
			"session_id":        session.SessionID,
			"inbox_session_id":  session.SessionID,
			"source_session_id": sessionID,
			"from_agent_id":     fromAgentID,
			"to_agent_id":       toAgentID,
			"task_id":           taskID,
			"channel":           sourceChannel,
			"user_id":           sourceUserID,
			"run_id":            runID,
		}, nil
	}
}

func agentMessageInbox(agentsPath, configPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		instanceID, rootDir, instanceManifest, err := resolveAgentMessagingContext(req, configPath)
		if err != nil {
			return nil, err
		}
		if !instanceManifest.Messaging.Enabled || !instanceManifest.Messaging.AllowInterAgentMessaging {
			return nil, errors.New("inter-agent messaging is disabled for this instance")
		}
		cfgPath, err := resolveOpenClawssyPath(req.Workspace, configPath, "config", "config.json")
		if err != nil {
			return nil, err
		}
		cfg, err := config.LoadOrDefault(cfgPath)
		if err != nil {
			return nil, err
		}
		if !cfg.Agents.AllowInterAgentMessaging {
			return nil, errors.New("inter-agent messaging is disabled by config")
		}

		target := strings.TrimSpace(valueString(req.Args, "agent_id"))
		if target == "" {
			target = strings.TrimSpace(req.AgentID)
		}
		target, err = validatedAgentID(target)
		if err != nil {
			return nil, err
		}
		requestAgentID, err := validatedAgentID(req.AgentID)
		if err != nil {
			return nil, errors.New("request agent id is invalid")
		}
		if target != requestAgentID && !hasPolicyAdmin(req) {
			return nil, errors.New("cross-agent inbox reads require policy.admin capability")
		}
		if _, err := instances.LoadAgentManifest(rootDir, instanceID, target); err != nil {
			return nil, fmt.Errorf("agent is not available in instance %q: %w", instanceID, err)
		}
		store, err := openAgentChatStore(req.Workspace, agentsPath)
		if err != nil {
			return nil, err
		}

		sessions, err := store.ListSessions(target, "", "", "")
		if err != nil {
			return nil, err
		}
		limit := getIntArg(req.Args, "limit", 20)
		if limit <= 0 {
			limit = 20
		}
		messageByID := make(map[string]map[string]any)
		messageOrder := make([]string, 0, limit)
		for _, session := range sessions {
			if !strings.EqualFold(session.Channel, "agent-mail") {
				continue
			}
			recent, err := store.ReadRecentMessages(session.SessionID, limit*4)
			if err != nil {
				continue
			}
			for _, item := range recent {
				instanceValue := instanceID
				content := item.Content
				envelope := decodeAgentMessageEnvelope(item)
				if strings.TrimSpace(envelope.InstanceID) != "" {
					instanceValue = strings.TrimSpace(envelope.InstanceID)
				}
				messageID := firstNonEmptyTrimmed(item.MessageID, envelope.MessageID)
				if messageID == "" {
					continue
				}
				entry := map[string]any{
					"message_id":        messageID,
					"status":            firstNonEmptyTrimmed(item.Status, envelope.Status, agentMessageStatusQueued),
					"instance_id":       instanceValue,
					"session_id":        session.SessionID,
					"source_session_id": firstNonEmptyTrimmed(item.SourceSessionID, envelope.SourceSessionID, envelope.SessionID),
					"from":              firstNonEmptyTrimmed(item.FromAgentID, envelope.FromAgentID, session.UserID),
					"to_agent_id":       firstNonEmptyTrimmed(item.ToAgentID, envelope.ToAgentID, target),
					"subject":           firstNonEmptyTrimmed(item.Subject, envelope.Subject),
					"task_id":           firstNonEmptyTrimmed(item.TaskID, envelope.TaskID, session.RoomID),
					"channel":           firstNonEmptyTrimmed(item.Channel, envelope.Channel),
					"user_id":           firstNonEmptyTrimmed(item.UserID, envelope.UserID),
					"message":           firstNonEmptyTrimmed(envelope.Message),
					"related_run_id":    firstNonEmptyTrimmed(item.RelatedRunID, envelope.RelatedRunID),
					"note":              firstNonEmptyTrimmed(item.Note, envelope.Note),
					"error":             firstNonEmptyTrimmed(item.Error, envelope.Error),
					"role":              item.Role,
					"content":           content,
					"ts":                item.TS,
				}
				if existing, ok := messageByID[messageID]; ok {
					for _, key := range []string{"status", "related_run_id", "note", "error", "ts", "source_session_id"} {
						if value, exists := entry[key]; exists {
							existing[key] = value
						}
					}
					continue
				}
				messageByID[messageID] = entry
				messageOrder = append(messageOrder, messageID)
			}
		}
		messages := make([]map[string]any, 0, len(messageOrder))
		for i := len(messageOrder) - 1; i >= 0; i-- {
			messages = append(messages, messageByID[messageOrder[i]])
			if len(messages) >= limit {
				break
			}
		}
		return map[string]any{
			"instance_id": instanceID,
			"agent_id":    target,
			"count":       len(messages),
			"messages":    messages,
		}, nil
	}
}

func validateMessagingPermission(allowlist []string, otherAgentID string, deniedMessage string) error {
	if len(allowlist) == 0 {
		return nil
	}
	otherAgentID = strings.TrimSpace(otherAgentID)
	for _, allowed := range allowlist {
		if strings.TrimSpace(allowed) == otherAgentID {
			return nil
		}
	}
	return errors.New(deniedMessage)
}

func decodeAgentMessageEnvelope(item chatstore.Message) agentMessageEnvelope {
	envelope := agentMessageEnvelope{
		MessageID:       strings.TrimSpace(item.MessageID),
		Status:          strings.TrimSpace(item.Status),
		InstanceID:      strings.TrimSpace(item.InstanceID),
		FromAgentID:     strings.TrimSpace(item.FromAgentID),
		ToAgentID:       strings.TrimSpace(item.ToAgentID),
		Subject:         strings.TrimSpace(item.Subject),
		TaskID:          strings.TrimSpace(item.TaskID),
		Channel:         strings.TrimSpace(item.Channel),
		UserID:          strings.TrimSpace(item.UserID),
		SourceSessionID: strings.TrimSpace(item.SourceSessionID),
		RelatedRunID:    strings.TrimSpace(item.RelatedRunID),
		Note:            strings.TrimSpace(item.Note),
		Error:           strings.TrimSpace(item.Error),
	}
	if !item.UpdatedAt.IsZero() {
		envelope.UpdatedAt = item.UpdatedAt.UTC().Format(time.RFC3339)
	}
	if err := json.Unmarshal([]byte(item.Content), &envelope); err != nil {
		return envelope
	}
	return envelope
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func newAgentMessageID(now time.Time) (string, error) {
	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("generate agent message id: %w", err)
	}
	return fmt.Sprintf("msg_%d_%s", now.UnixNano(), hex.EncodeToString(randBytes)), nil
}

func resolveAgentMessagingContext(req Request, configPath string) (string, string, instances.InstanceManifest, error) {
	rootDir := strings.TrimSpace(filepath.Dir(filepath.Dir(resolvePathOrEmpty(req.Workspace, configPath))))
	if rootDir == "" {
		return "", "", instances.InstanceManifest{}, errors.New("workspace is required to resolve messaging instance")
	}
	instanceID := strings.TrimSpace(req.InstanceID)
	if instanceID == "" {
		activeID, err := instances.LoadActiveInstanceID(rootDir)
		if err == nil {
			instanceID = activeID
		}
	}
	if instanceID == "" {
		instanceID = instances.DefaultInstanceID
	}
	if _, err := instances.ValidateInstanceID(instanceID); err != nil {
		return "", "", instances.InstanceManifest{}, err
	}
	manifest, err := instances.LoadInstanceManifest(rootDir, instanceID)
	if err != nil {
		if _, bootstrapErr := instances.BootstrapDefaultInstance(rootDir); bootstrapErr == nil {
			manifest, err = instances.LoadInstanceManifest(rootDir, instanceID)
		}
		if err != nil {
			return "", "", instances.InstanceManifest{}, err
		}
	}
	return instanceID, rootDir, manifest, nil
}

func resolvePathOrEmpty(workspace, configuredPath string) string {
	resolved, err := resolveOpenClawssyPath(workspace, configuredPath, "config", "config.json")
	if err != nil {
		return ""
	}
	return resolved
}

func agentRun(agentsPath, configPath string, runner AgentRunner) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		if runner == nil {
			return nil, errors.New("agent runner is not configured")
		}
		cfgPath, err := resolveOpenClawssyPath(req.Workspace, configPath, "config", "config.json")
		if err != nil {
			return nil, err
		}
		cfg, err := config.LoadOrDefault(cfgPath)
		if err != nil {
			return nil, err
		}
		if !cfg.Agents.AllowInterAgentMessaging {
			return nil, errors.New("subagent runs are disabled by config (agents.allow_inter_agent_messaging=false)")
		}

		targetAgentID, err := validatedAgentID(valueString(req.Args, "agent_id"))
		if err != nil {
			return nil, err
		}
		caller, err := validatedAgentID(req.AgentID)
		if err != nil {
			return nil, errors.New("request agent id is invalid")
		}
		if caller != targetAgentID && !hasPolicyAdmin(req) {
			return nil, errors.New("cross-agent runs require policy.admin capability; use the main orchestrator agent or grant policy.admin explicitly")
		}

		msg := strings.TrimSpace(valueString(req.Args, "message"))
		if msg == "" {
			return nil, errors.New("message is required")
		}
		messageID := strings.TrimSpace(valueString(req.Args, "message_id"))
		var out AgentRunOutput
		if messageID != "" {
			store, storeErr := openAgentChatStore(req.Workspace, agentsPath)
			if storeErr != nil {
				return nil, storeErr
			}
			out, err = runAgentMessageLifecycle(ctx, runner, store, req, agentMessageEnvelope{
				MessageID:       messageID,
				InstanceID:      strings.TrimSpace(req.InstanceID),
				FromAgentID:     caller,
				ToAgentID:       targetAgentID,
				TaskID:          strings.TrimSpace(valueString(req.Args, "task_id")),
				SourceSessionID: strings.TrimSpace(valueString(req.Args, "source_session_id")),
				Message:         msg,
			}, strings.TrimSpace(valueString(req.Args, "inbox_session_id")))
		} else {
			out, err = runner.ExecuteSubAgent(ctx, AgentRunInput{
				InstanceID:        strings.TrimSpace(req.InstanceID),
				CallerAgentID:     caller,
				TargetAgentID:     targetAgentID,
				MessageID:         messageID,
				ParentRunID:       strings.TrimSpace(valueString(req.Args, "parent_run_id")),
				Message:           msg,
				TaskID:            strings.TrimSpace(valueString(req.Args, "task_id")),
				Source:            "subagent/" + caller,
				ThinkingMode:      strings.TrimSpace(valueString(req.Args, "thinking_mode")),
				AllowedTools:      stringSliceArg(req.Args, "allowed_tools"),
				MaxToolIterations: getIntArg(req.Args, "max_tool_iterations", 0),
				TimeoutMS:         getIntArg(req.Args, "timeout_ms", 0),
			})
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"agent_id":      targetAgentID,
			"run_id":        out.RunID,
			"message_id":    strings.TrimSpace(out.MessageID),
			"status":        firstNonEmptyTrimmed(out.Status, agentMessageStatusCompleted),
			"output":        out.FinalText,
			"artifact_path": out.ArtifactPath,
			"duration_ms":   out.DurationMS,
			"tool_calls":    out.ToolCalls,
			"provider":      out.Provider,
			"model":         out.Model,
		}, nil
	}
}

func appendAgentMessageStatus(store *chatstore.Store, sessionID string, envelope agentMessageEnvelope) error {
	now := time.Now().UTC()
	envelope.UpdatedAt = now.Format(time.RFC3339)
	raw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	return store.AppendMessage(sessionID, chatstore.Message{
		Role:            "system",
		Content:         string(raw),
		TS:              now,
		MessageID:       strings.TrimSpace(envelope.MessageID),
		Status:          strings.TrimSpace(envelope.Status),
		InstanceID:      strings.TrimSpace(envelope.InstanceID),
		FromAgentID:     strings.TrimSpace(envelope.FromAgentID),
		ToAgentID:       strings.TrimSpace(envelope.ToAgentID),
		TaskID:          strings.TrimSpace(envelope.TaskID),
		Subject:         strings.TrimSpace(envelope.Subject),
		Channel:         strings.TrimSpace(envelope.Channel),
		UserID:          strings.TrimSpace(envelope.UserID),
		SourceSessionID: strings.TrimSpace(envelope.SourceSessionID),
		RelatedRunID:    strings.TrimSpace(envelope.RelatedRunID),
		Note:            strings.TrimSpace(envelope.Note),
		Error:           strings.TrimSpace(envelope.Error),
		UpdatedAt:       now,
	})
}

func runAgentMessageLifecycle(ctx context.Context, runner AgentRunner, store *chatstore.Store, req Request, envelope agentMessageEnvelope, inboxSessionID string) (AgentRunOutput, error) {
	if runner == nil {
		return AgentRunOutput{}, errors.New("agent runner is not configured")
	}
	inboxSessionID = strings.TrimSpace(inboxSessionID)
	_ = appendAgentMessageStatus(store, inboxSessionID, agentMessageEnvelope{
		MessageID:       envelope.MessageID,
		Status:          agentMessageStatusRunning,
		InstanceID:      envelope.InstanceID,
		FromAgentID:     envelope.FromAgentID,
		ToAgentID:       envelope.ToAgentID,
		Subject:         envelope.Subject,
		TaskID:          envelope.TaskID,
		SourceSessionID: envelope.SourceSessionID,
		Channel:         envelope.Channel,
		UserID:          envelope.UserID,
		Note:            "subagent execution started",
		SentAt:          envelope.SentAt,
	})
	out, err := runner.ExecuteSubAgent(ctx, AgentRunInput{
		InstanceID:        strings.TrimSpace(envelope.InstanceID),
		CallerAgentID:     strings.TrimSpace(envelope.FromAgentID),
		TargetAgentID:     strings.TrimSpace(envelope.ToAgentID),
		MessageID:         strings.TrimSpace(envelope.MessageID),
		ParentRunID:       strings.TrimSpace(valueString(req.Args, "parent_run_id")),
		Message:           strings.TrimSpace(envelope.Message),
		TaskID:            strings.TrimSpace(envelope.TaskID),
		Source:            firstNonEmptyTrimmed(strings.TrimSpace(valueString(req.Args, "source")), "message/"+strings.TrimSpace(envelope.FromAgentID)),
		ThinkingMode:      strings.TrimSpace(valueString(req.Args, "thinking_mode")),
		AllowedTools:      stringSliceArg(req.Args, "allowed_tools"),
		MaxToolIterations: getIntArg(req.Args, "max_tool_iterations", 0),
		TimeoutMS:         getIntArg(req.Args, "timeout_ms", 0),
	})
	status := agentMessageStatusCompleted
	finalError := ""
	if err != nil {
		status = agentMessageStatusFailed
		finalError = strings.TrimSpace(err.Error())
	} else if strings.TrimSpace(out.Status) != "" {
		status = strings.TrimSpace(out.Status)
	}
	out.Status = status
	out.MessageID = strings.TrimSpace(envelope.MessageID)
	_ = appendAgentMessageStatus(store, inboxSessionID, agentMessageEnvelope{
		MessageID:       envelope.MessageID,
		Status:          status,
		InstanceID:      envelope.InstanceID,
		FromAgentID:     envelope.FromAgentID,
		ToAgentID:       envelope.ToAgentID,
		Subject:         envelope.Subject,
		TaskID:          envelope.TaskID,
		SourceSessionID: envelope.SourceSessionID,
		Channel:         envelope.Channel,
		UserID:          envelope.UserID,
		RelatedRunID:    strings.TrimSpace(out.RunID),
		Note:            "subagent execution finished",
		Error:           finalError,
		SentAt:          envelope.SentAt,
	})
	return out, err
}

func stringSliceArg(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func agentPromptRead(agentsPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		agentID := strings.TrimSpace(valueString(req.Args, "agent_id"))
		if agentID == "" {
			agentID = strings.TrimSpace(req.AgentID)
		}
		agentID, err := validatedAgentID(agentID)
		if err != nil {
			return nil, err
		}
		fileName, err := normalizedPromptFile(valueString(req.Args, "file"))
		if err != nil {
			return nil, err
		}
		agentsRoot, err := resolveOpenClawssyPath(req.Workspace, agentsPath, "agents", "agents")
		if err != nil {
			return nil, err
		}
		path := filepath.Join(agentsRoot, agentID, fileName)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"agent_id": agentID,
			"file":     fileName,
			"content":  string(raw),
			"bytes":    len(raw),
		}, nil
	}
}

func agentPromptUpdate(agentsPath, configPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		targetAgentID := strings.TrimSpace(valueString(req.Args, "agent_id"))
		if targetAgentID == "" {
			targetAgentID = strings.TrimSpace(req.AgentID)
		}
		targetAgentID, err := validatedAgentID(targetAgentID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.AgentID) != targetAgentID && !hasPolicyAdmin(req) {
			return nil, errors.New("cross-agent prompt updates require policy.admin capability")
		}

		cfgPath, err := resolveOpenClawssyPath(req.Workspace, configPath, "config", "config.json")
		if err != nil {
			return nil, err
		}
		cfg, err := config.LoadOrDefault(cfgPath)
		if err != nil {
			return nil, err
		}
		profile := cfg.Agents.Profiles[targetAgentID]
		if !cfg.Agents.SelfImprovementEnabled || !profile.SelfImprovement {
			return nil, errors.New("self-improvement is disabled (enable agents.self_improvement_enabled and agent profile self_improvement)")
		}

		fileName, err := normalizedPromptFile(valueString(req.Args, "file"))
		if err != nil {
			return nil, err
		}
		content := valueString(req.Args, "content")

		agentsRoot, err := resolveOpenClawssyPath(req.Workspace, agentsPath, "agents", "agents")
		if err != nil {
			return nil, err
		}
		path := filepath.Join(agentsRoot, targetAgentID, fileName)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return nil, err
		}
		return map[string]any{"updated": true, "agent_id": targetAgentID, "file": fileName, "bytes": len(content)}, nil
	}
}

func agentPromptSuggest(agentsPath, workspaceRoot string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		agentID := strings.TrimSpace(valueString(req.Args, "agent_id"))
		if agentID == "" {
			agentID = strings.TrimSpace(req.AgentID)
		}
		agentID, err := validatedAgentID(agentID)
		if err != nil {
			return nil, err
		}
		focus := strings.TrimSpace(valueString(req.Args, "focus"))
		goal := strings.TrimSpace(valueString(req.Args, "goal"))

		agentsRoot, err := resolveOpenClawssyPath(req.Workspace, agentsPath, "agents", "agents")
		if err != nil {
			return nil, err
		}
		docs := map[string]string{}
		for _, name := range []string{"SOUL.md", "RULES.md", "TOOLS.md"} {
			path := filepath.Join(agentsRoot, agentID, name)
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				continue
			}
			docs[name] = string(raw)
		}

		suggestions := make([]string, 0, 6)
		if !strings.Contains(strings.ToLower(docs["RULES.md"]), "verify") {
			suggestions = append(suggestions, "Add an explicit verification rule that requires tests/checks for non-trivial changes.")
		}
		if !strings.Contains(strings.ToLower(docs["SOUL.md"]), "tradeoff") {
			suggestions = append(suggestions, "In SOUL.md, require brief tradeoff notes when choosing between alternative implementations.")
		}
		if !strings.Contains(strings.ToLower(docs["RULES.md"]), "one precise question") {
			suggestions = append(suggestions, "Add a blocked-state rule: ask one precise question only after exhausting non-blocking work.")
		}
		if !strings.Contains(strings.ToLower(docs["TOOLS.md"]), "agent.message.send") {
			suggestions = append(suggestions, "Include inter-agent tools in TOOLS.md and when to use them for task handoffs.")
		}
		if focus != "" {
			suggestions = append(suggestions, "Focus area requested: "+focus+". Add a dedicated checklist section for this area.")
		}
		if goal != "" {
			suggestions = append(suggestions, "Goal alignment: add a mission line explicitly optimizing for \""+goal+"\".")
		}
		if len(suggestions) == 0 {
			suggestions = append(suggestions, "Current prompts are reasonably complete. Consider tightening wording to reduce ambiguity and enforce deterministic output format.")
		}

		rewrite := "# Suggested System Prompt Patch\n\n"
		rewrite += "## SOUL additions\n- Prioritize task completion with verifiable evidence and concise status updates.\n"
		rewrite += "- When blocked, ask one precise question with a recommended default.\n"
		rewrite += "\n## RULES additions\n- Use inter-agent messaging for cross-agent coordination and retain task IDs in handoffs.\n"
		rewrite += "- Run focused verification for non-trivial changes and report outcomes.\n"
		rewrite += "\n## TOOLS additions\n- Prefer `agent.message.send` and `agent.message.inbox` for structured collaboration.\n"
		if strings.TrimSpace(workspaceRoot) != "" {
			rewrite += "- Workspace root observed by runtime: `" + strings.TrimSpace(workspaceRoot) + "`.\n"
		}

		return map[string]any{
			"agent_id":       agentID,
			"focus":          focus,
			"goal":           goal,
			"suggestions":    suggestions,
			"proposed_patch": rewrite,
		}, nil
	}
}

func agentIdentitySet(agentsPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		targetAgentID := strings.TrimSpace(valueString(req.Args, "agent_id"))
		if targetAgentID == "" {
			targetAgentID = strings.TrimSpace(req.AgentID)
		}
		targetAgentID, err := validatedAgentID(targetAgentID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.AgentID) != targetAgentID && !hasPolicyAdmin(req) {
			return nil, errors.New("cross-agent identity updates require policy.admin capability")
		}

		assistantName := strings.TrimSpace(valueString(req.Args, "assistant_name"))
		if assistantName == "" {
			return nil, errors.New("assistant_name is required")
		}
		if err := validateIdentityName("assistant_name", assistantName); err != nil {
			return nil, err
		}
		userName := strings.TrimSpace(valueString(req.Args, "user_name"))
		if userName == "" {
			return nil, errors.New("user_name is required")
		}
		if err := validateIdentityName("user_name", userName); err != nil {
			return nil, err
		}

		agentsRoot, err := resolveOpenClawssyPath(req.Workspace, agentsPath, "agents", "agents")
		if err != nil {
			return nil, err
		}
		agentRoot := filepath.Join(agentsRoot, targetAgentID)
		if err := os.MkdirAll(agentRoot, 0o755); err != nil {
			return nil, err
		}
		path := filepath.Join(agentRoot, "SOUL.md")
		if raw, err := os.ReadFile(path); err == nil {
			if !agentdocs.SoulNeedsBootstrap(string(raw)) {
				return nil, errors.New("SOUL.md is already initialized; clear SOUL.md to rerun identity bootstrap")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		content := soulIdentityContent(assistantName, userName)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return nil, err
		}

		return map[string]any{
			"updated":        true,
			"agent_id":       targetAgentID,
			"file":           "SOUL.md",
			"assistant_name": assistantName,
			"user_name":      userName,
			"bytes":          len(content),
		}, nil
	}
}

func openAgentChatStore(workspace, agentsPath string) (*chatstore.Store, error) {
	path, err := resolveOpenClawssyPath(workspace, agentsPath, "chatstore", "agents")
	if err != nil {
		return nil, err
	}
	return chatstore.NewStore(path)
}

func normalizedPromptFile(raw string) (string, error) {
	fileName := strings.ToUpper(strings.TrimSpace(raw))
	if fileName == "" {
		return "", errors.New("file is required")
	}
	allowed := map[string]string{
		"SOUL":        "SOUL.md",
		"SOUL.MD":     "SOUL.md",
		"RULES":       "RULES.md",
		"RULES.MD":    "RULES.md",
		"TOOLS":       "TOOLS.md",
		"TOOLS.MD":    "TOOLS.md",
		"SPECPLAN":    "SPECPLAN.md",
		"SPECPLAN.MD": "SPECPLAN.md",
		"DEVPLAN":     "DEVPLAN.md",
		"DEVPLAN.MD":  "DEVPLAN.md",
		"HANDOFF":     "HANDOFF.md",
		"HANDOFF.MD":  "HANDOFF.md",
	}
	canonical, ok := allowed[fileName]
	if !ok {
		return "", errors.New("file must be one of SOUL.md, RULES.md, TOOLS.md, SPECPLAN.md, DEVPLAN.md, HANDOFF.md")
	}
	return canonical, nil
}

func containsTrimmedString(items []string, candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	for _, item := range items {
		if strings.TrimSpace(item) == candidate {
			return true
		}
	}
	return false
}

func requireFloat(value any, field string) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int32:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("%s must be a number", field)
	}
}

func hasPolicyAdmin(req Request) bool {
	reader, ok := req.Policy.(interface {
		HasCapability(agentID, capability string) bool
	})
	if !ok || reader == nil {
		return false
	}
	return reader.HasCapability(strings.TrimSpace(req.AgentID), "policy.admin")
}

func createAgentScaffold(agentRoot string, force bool) ([]string, error) {
	return agentdocs.SeedAgentScaffold(agentRoot, force)
}

func maybeBootstrapClawDefuckifier(workspace, configPath, agentID string) error {
	if !agentdocs.IsClawDefuckifierAgent(agentID) {
		return nil
	}
	cfgPath, err := resolveOpenClawssyPath(workspace, configPath, "config", "config.json")
	if err != nil {
		return err
	}
	return clawdefuckifierpkg.EnsureBootstrap(agentID, workspace, cfgPath)
}

func soulIdentityContent(assistantName, userName string) string {
	return "# SOUL\n\n" +
		"You are " + assistantName + ", a high-accountability software engineering agent.\n\n" +
		"## Identity\n" +
		"- Call the user " + userName + ".\n" +
		"- Refer to yourself as " + assistantName + ".\n\n" +
		"## Mission\n" +
		"- Deliver correct results with minimal user friction.\n" +
		"- Prefer concrete execution and evidence over speculation.\n" +
		"- Keep updates concise and actionable.\n\n" +
		"## Working Style\n" +
		"- Read the repo and runtime context before acting.\n" +
		"- Do the obvious safe work first; do not stall with unnecessary questions.\n" +
		"- When several reasonable options exist, choose the safest one and mention the main tradeoff briefly.\n\n" +
		"## Quality Bar\n" +
		"- Validate assumptions against repository context before making changes.\n" +
		"- Preserve user intent and existing architecture unless directed otherwise.\n" +
		"- Verify meaningful changes with the smallest relevant check and report any remaining risk.\n"
}

func validateIdentityName(field, value string) error {
	if len(value) > 80 {
		return fmt.Errorf("%s is too long (max 80 chars)", field)
	}
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' {
			return fmt.Errorf("%s must be a single line", field)
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == ' ' || r == '-' || r == '_' || r == '.' || r == '\'' {
			continue
		}
		return fmt.Errorf("%s contains unsupported characters", field)
	}
	return nil
}

func validatedAgentID(raw string) (string, error) {
	agentID := strings.TrimSpace(raw)
	if agentID == "" {
		return "", errors.New("agent_id is required")
	}
	if strings.Contains(agentID, "..") || strings.ContainsRune(agentID, '/') || strings.ContainsRune(agentID, '\\') {
		return "", errors.New("invalid agent_id")
	}
	return agentID, nil
}
