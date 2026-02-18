package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"openclawssy/internal/config"
)

const (
	defaultAgentListLimit = 50
	maxAgentListLimit     = 200
)

func registerAgentTools(reg *Registry, agentsPath, configPath string) error {
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
	}, agentCreate(agentsPath)); err != nil {
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

func agentCreate(configuredPath string) Handler {
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

func createAgentScaffold(agentRoot string, force bool) ([]string, error) {
	for _, dir := range []string{"memory", "audit", "runs"} {
		if err := os.MkdirAll(filepath.Join(agentRoot, dir), 0o755); err != nil {
			return nil, err
		}
	}

	files := map[string]string{
		"SOUL.md":     "# SOUL\n\nMission and behavior contract for this agent.\n",
		"RULES.md":    "# RULES\n\n- Follow workspace-only write policy.\n- Respect tool capabilities.\n",
		"TOOLS.md":    "# TOOLS\n\nEnabled core tools: fs.read, fs.list, fs.write, fs.append, fs.delete, fs.move, fs.edit, code.search, config.get, config.set, secrets.get, secrets.set, secrets.list, scheduler.list, scheduler.add, scheduler.remove, scheduler.pause, scheduler.resume, session.list, session.close, agent.list, agent.create, agent.switch, run.list, run.get, run.cancel, http.request, time.now.\n",
		"SPECPLAN.md": "# SPECPLAN\n\nDescribe specs and acceptance requirements before coding.\n",
		"DEVPLAN.md":  "# DEVPLAN\n\n- [ ] Implement task\n- [ ] Add tests\n- [ ] Update handoff\n",
		"HANDOFF.md":  "# HANDOFF\n\nStatus: initialized\n\nNext:\n- Define first run objective.\n",
	}

	seeded := make([]string, 0, len(files))
	for name, body := range files {
		path := filepath.Join(agentRoot, name)
		if !force {
			if _, err := os.Stat(path); err == nil {
				continue
			}
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
		seeded = append(seeded, name)
	}
	sort.Strings(seeded)
	return seeded, nil
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
