package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"openclawssy/internal/config"
)

func registerConfigTools(reg *Registry, configuredPath string) error {
	if err := reg.Register(ToolSpec{
		Name:        "config.get",
		Description: "Get runtime config (redacted)",
		ArgTypes:    map[string]ArgType{"field": ArgTypeString},
	}, configGet(configuredPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "config.set",
		Description: "Update safe runtime config fields",
		Required:    []string{"updates"},
		ArgTypes:    map[string]ArgType{"updates": ArgTypeObject, "dry_run": ArgTypeBool},
	}, configSet(configuredPath)); err != nil {
		return err
	}
	return nil
}

func configGet(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		cfgPath, err := resolveOpenClawssyPath(req.Workspace, configuredPath, "config", "config.json")
		if err != nil {
			return nil, err
		}
		cfg, err := config.LoadOrDefault(cfgPath)
		if err != nil {
			return nil, err
		}
		redacted := redactedConfigForTool(cfg)

		field := ""
		if raw, ok := req.Args["field"]; ok {
			value, ok := raw.(string)
			if !ok {
				return nil, errors.New("field must be a string")
			}
			field = strings.TrimSpace(value)
		}
		if field == "" {
			return map[string]any{"config": redacted}, nil
		}

		value, ok := configGetField(redacted, field)
		if !ok {
			return nil, fmt.Errorf("invalid config field: %s", field)
		}
		return map[string]any{"field": field, "value": value}, nil
	}
}

func configSet(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		cfgPath, err := resolveOpenClawssyPath(req.Workspace, configuredPath, "config", "config.json")
		if err != nil {
			return nil, err
		}
		cfg, err := config.LoadOrDefault(cfgPath)
		if err != nil {
			return nil, err
		}

		rawUpdates, ok := req.Args["updates"].(map[string]any)
		if !ok {
			return nil, errors.New("updates must be an object")
		}
		if len(rawUpdates) == 0 {
			return nil, errors.New("updates cannot be empty")
		}

		updatedFields := make([]string, 0, len(rawUpdates))
		for key, value := range rawUpdates {
			if err := applyConfigFieldUpdate(&cfg, key, value); err != nil {
				return nil, err
			}
			updatedFields = append(updatedFields, key)
		}
		sort.Strings(updatedFields)

		if err := cfg.Validate(); err != nil {
			return nil, err
		}

		dryRun := getBoolArg(req.Args, "dry_run", false)
		if !dryRun {
			if err := config.Save(cfgPath, cfg); err != nil {
				return nil, err
			}
		}

		return map[string]any{
			"updated_fields": updatedFields,
			"dry_run":        dryRun,
			"config":         redactedConfigForTool(cfg),
		}, nil
	}
}

func redactedConfigForTool(cfg config.Config) config.Config {
	redacted := cfg.Redacted()
	redacted.Secrets.StoreFile = ""
	redacted.Secrets.MasterKeyFile = ""
	return redacted
}

func applyConfigFieldUpdate(cfg *config.Config, field string, value any) error {
	if cfg == nil {
		return errors.New("config is required")
	}
	field = strings.TrimSpace(field)
	if field == "" {
		return errors.New("update field name cannot be empty")
	}

	switch field {
	case "output.thinking_mode":
		mode, err := requireString(value, field)
		if err != nil {
			return err
		}
		mode = config.NormalizeThinkingMode(mode)
		if !config.IsValidThinkingMode(mode) {
			return errors.New("output.thinking_mode must be one of never|on_error|always")
		}
		cfg.Output.ThinkingMode = mode
	case "output.max_thinking_chars":
		n, err := requireInt(value, field)
		if err != nil {
			return err
		}
		cfg.Output.MaxThinkingChars = n
	case "chat.rate_limit_per_min":
		n, err := requireInt(value, field)
		if err != nil {
			return err
		}
		cfg.Chat.RateLimitPerMin = n
	case "chat.global_rate_limit_per_min":
		n, err := requireInt(value, field)
		if err != nil {
			return err
		}
		cfg.Chat.GlobalRateLimitPerMin = n
	case "discord.rate_limit_per_min":
		if !cfg.Discord.Enabled {
			return errors.New("discord.rate_limit_per_min can only be set when discord.enabled is true")
		}
		n, err := requireInt(value, field)
		if err != nil {
			return err
		}
		cfg.Discord.RateLimitPerMin = n
	case "discord.command_prefix":
		if !cfg.Discord.Enabled {
			return errors.New("discord.command_prefix can only be set when discord.enabled is true")
		}
		prefix, err := requireString(value, field)
		if err != nil {
			return err
		}
		if strings.TrimSpace(prefix) == "" {
			return errors.New("discord.command_prefix cannot be empty")
		}
		cfg.Discord.CommandPrefix = prefix
	case "engine.max_concurrent_runs":
		n, err := requireInt(value, field)
		if err != nil {
			return err
		}
		cfg.Engine.MaxConcurrentRuns = n
	case "scheduler.max_concurrent_jobs":
		n, err := requireInt(value, field)
		if err != nil {
			return err
		}
		cfg.Scheduler.MaxConcurrentJobs = n
	case "network.enabled":
		b, err := requireBool(value, field)
		if err != nil {
			return err
		}
		cfg.Network.Enabled = b
	case "network.allowed_domains":
		domains, err := requireStringSlice(value, field)
		if err != nil {
			return err
		}
		cfg.Network.AllowedDomains = domains
	case "network.allow_localhosts":
		b, err := requireBool(value, field)
		if err != nil {
			return err
		}
		cfg.Network.AllowLocalhosts = b
	case "shell.enable_exec":
		b, err := requireBool(value, field)
		if err != nil {
			return err
		}
		if b && !cfg.Sandbox.Active {
			return errors.New("shell.enable_exec cannot be true when sandbox.active is false")
		}
		cfg.Shell.EnableExec = b
	default:
		return fmt.Errorf("field is not mutable: %s", field)
	}

	return nil
}

func configGetField(cfg config.Config, field string) (any, bool) {
	switch field {
	case "output.thinking_mode":
		return cfg.Output.ThinkingMode, true
	case "output.max_thinking_chars":
		return cfg.Output.MaxThinkingChars, true
	case "chat.rate_limit_per_min":
		return cfg.Chat.RateLimitPerMin, true
	case "chat.global_rate_limit_per_min":
		return cfg.Chat.GlobalRateLimitPerMin, true
	case "discord.rate_limit_per_min":
		return cfg.Discord.RateLimitPerMin, true
	case "discord.command_prefix":
		return cfg.Discord.CommandPrefix, true
	case "engine.max_concurrent_runs":
		return cfg.Engine.MaxConcurrentRuns, true
	case "scheduler.max_concurrent_jobs":
		return cfg.Scheduler.MaxConcurrentJobs, true
	case "network.enabled":
		return cfg.Network.Enabled, true
	case "network.allowed_domains":
		return cfg.Network.AllowedDomains, true
	case "network.allow_localhosts":
		return cfg.Network.AllowLocalhosts, true
	case "shell.enable_exec":
		return cfg.Shell.EnableExec, true
	case "config":
		return cfg, true
	default:
		return nil, false
	}
}

func requireString(value any, field string) (string, error) {
	v, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	return v, nil
}

func requireBool(value any, field string) (bool, error) {
	v, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a bool", field)
	}
	return v, nil
}

func requireInt(value any, field string) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case uint:
		return int(v), nil
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint64:
		return int(v), nil
	case float32:
		if math.Trunc(float64(v)) != float64(v) {
			return 0, fmt.Errorf("%s must be an integer", field)
		}
		return int(v), nil
	case float64:
		if math.Trunc(v) != v {
			return 0, fmt.Errorf("%s must be an integer", field)
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", field)
	}
}

func requireStringSlice(value any, field string) ([]string, error) {
	switch raw := value.(type) {
	case []string:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			trimmed := strings.TrimSpace(item)
			if trimmed == "" {
				return nil, fmt.Errorf("%s cannot contain empty entries", field)
			}
			out = append(out, trimmed)
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be an array of strings", field)
			}
			trimmed := strings.TrimSpace(s)
			if trimmed == "" {
				return nil, fmt.Errorf("%s cannot contain empty entries", field)
			}
			out = append(out, trimmed)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be an array of strings", field)
	}
}
