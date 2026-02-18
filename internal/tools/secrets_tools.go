package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"openclawssy/internal/config"
	"openclawssy/internal/secrets"
)

func registerSecretsTools(reg *Registry, configuredPath string) error {
	if err := reg.Register(ToolSpec{
		Name:        "secrets.set",
		Description: "Store encrypted secret value by key",
		Required:    []string{"key", "value"},
		ArgTypes:    map[string]ArgType{"key": ArgTypeString, "value": ArgTypeString},
	}, secretsSet(configuredPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "secrets.get",
		Description: "Get encrypted secret value by key",
		Required:    []string{"key"},
		ArgTypes:    map[string]ArgType{"key": ArgTypeString},
	}, secretsGet(configuredPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "secrets.list",
		Description: "List encrypted secret keys",
	}, secretsList(configuredPath)); err != nil {
		return err
	}
	return nil
}

func secretsSet(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		key, err := getString(req.Args, "key")
		if err != nil {
			return nil, err
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("key cannot be empty")
		}
		value, err := getString(req.Args, "value")
		if err != nil {
			return nil, err
		}

		store, err := openSecretsStore(req.Workspace, configuredPath)
		if err != nil {
			return nil, err
		}
		if err := store.Set(key, value); err != nil {
			return nil, err
		}

		return map[string]any{
			"key":     key,
			"updated": true,
			"summary": fmt.Sprintf("stored secret key %s", key),
		}, nil
	}
}

func secretsGet(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		key, err := getString(req.Args, "key")
		if err != nil {
			return nil, err
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("key cannot be empty")
		}

		store, err := openSecretsStore(req.Workspace, configuredPath)
		if err != nil {
			return nil, err
		}
		value, found, err := store.Get(key)
		if err != nil {
			return nil, err
		}
		if !found {
			return map[string]any{"key": key, "found": false}, nil
		}
		return map[string]any{"key": key, "found": true, "value": value}, nil
	}
}

func secretsList(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		store, err := openSecretsStore(req.Workspace, configuredPath)
		if err != nil {
			return nil, err
		}
		keys, err := store.ListKeys()
		if err != nil {
			return nil, err
		}
		sort.Strings(keys)
		return map[string]any{"keys": keys}, nil
	}
}

func openSecretsStore(workspace, configuredPath string) (*secrets.Store, error) {
	path, err := resolveOpenClawssyPath(workspace, configuredPath, "config", "config.json")
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadOrDefault(path)
	if err != nil {
		return nil, err
	}
	return secrets.NewStore(cfg)
}
