package tools

import (
	"context"
	"errors"
	"strings"

	httpchannel "openclawssy/internal/channels/http"
)

const (
	defaultRunListLimit = 50
	maxRunListLimit     = 200
)

func registerRunTools(reg *Registry, runsPath string, tracker RunCanceller) error {
	if err := reg.Register(ToolSpec{
		Name:        "run.list",
		Description: "List runs with filtering and pagination",
		ArgTypes: map[string]ArgType{
			"agent_id": ArgTypeString,
			"status":   ArgTypeString,
			"limit":    ArgTypeNumber,
			"offset":   ArgTypeNumber,
		},
	}, runList(runsPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "run.get",
		Description: "Get a specific run by ID",
		Required:    []string{"run_id"},
		ArgTypes: map[string]ArgType{
			"run_id": ArgTypeString,
			"id":     ArgTypeString,
		},
	}, runGet(runsPath)); err != nil {
		return err
	}
	if tracker != nil {
		if err := registerRunCancelTool(reg, tracker); err != nil {
			return err
		}
	}
	return nil
}

func runList(runsPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		store, err := openRunStore(req.Workspace, runsPath)
		if err != nil {
			return nil, err
		}

		runs, err := store.List(nil)
		if err != nil {
			return nil, err
		}

		agentID := strings.TrimSpace(valueString(req.Args, "agent_id"))
		status := strings.TrimSpace(valueString(req.Args, "status"))

		filtered := make([]httpchannel.Run, 0, len(runs))
		for _, run := range runs {
			if agentID != "" && run.AgentID != agentID {
				continue
			}
			if status != "" && !strings.EqualFold(run.Status, status) {
				continue
			}
			filtered = append(filtered, run)
		}

		runs = filtered

		sliced, meta := paginate(runs, req.Args, defaultRunListLimit, maxRunListLimit)
		meta["runs"] = sliced
		return meta, nil
	}
}

func runGet(runsPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		store, err := openRunStore(req.Workspace, runsPath)
		if err != nil {
			return nil, err
		}

		runID := strings.TrimSpace(valueString(req.Args, "run_id"))
		if runID == "" {
			runID = strings.TrimSpace(valueString(req.Args, "id"))
		}
		if runID == "" {
			return nil, errors.New("run_id is required")
		}

		run, err := store.Get(nil, runID)
		if err != nil {
			if errors.Is(err, httpchannel.ErrRunNotFound) {
				return map[string]any{"run_id": runID, "found": false}, nil
			}
			return nil, err
		}

		return map[string]any{
			"run":   run,
			"found": true,
		}, nil
	}
}

func openRunStore(workspace, configuredPath string) (*httpchannel.FileRunStore, error) {
	path, err := resolveOpenClawssyPath(workspace, configuredPath, "runs", "runs.json")
	if err != nil {
		return nil, err
	}
	return httpchannel.NewFileRunStore(path)
}
