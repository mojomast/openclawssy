package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"openclawssy/internal/scheduler"
)

func registerSchedulerTools(reg *Registry, configuredPath string) error {
	if err := reg.Register(ToolSpec{
		Name:        "scheduler.list",
		Description: "List scheduler jobs and paused state",
	}, schedulerList(configuredPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "scheduler.add",
		Description: "Add scheduler job",
		Required:    []string{"schedule", "message"},
		ArgTypes: map[string]ArgType{
			"id":         ArgTypeString,
			"schedule":   ArgTypeString,
			"message":    ArgTypeString,
			"agent_id":   ArgTypeString,
			"enabled":    ArgTypeBool,
			"channel":    ArgTypeString,
			"user_id":    ArgTypeString,
			"room_id":    ArgTypeString,
			"session_id": ArgTypeString,
		},
	}, schedulerAdd(configuredPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "scheduler.remove",
		Description: "Remove scheduler job by id",
		Required:    []string{"id"},
		ArgTypes:    map[string]ArgType{"id": ArgTypeString},
	}, schedulerRemove(configuredPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "scheduler.pause",
		Description: "Pause scheduler globally or disable one job",
		ArgTypes:    map[string]ArgType{"id": ArgTypeString},
	}, schedulerPause(configuredPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "scheduler.resume",
		Description: "Resume scheduler globally or enable one job",
		ArgTypes:    map[string]ArgType{"id": ArgTypeString},
	}, schedulerResume(configuredPath)); err != nil {
		return err
	}
	return nil
}

func schedulerList(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		store, err := openSchedulerStore(req.Workspace, configuredPath)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"paused": store.IsPaused(),
			"jobs":   store.List(),
		}, nil
	}
}

func schedulerAdd(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		schedule, err := getString(req.Args, "schedule")
		if err != nil {
			return nil, err
		}
		message, err := getString(req.Args, "message")
		if err != nil {
			return nil, err
		}
		jobID := strings.TrimSpace(fmt.Sprintf("%v", req.Args["id"]))
		if jobID == "" || jobID == "<nil>" {
			jobID = fmt.Sprintf("job_%d", time.Now().UTC().UnixNano())
		}
		agentID := strings.TrimSpace(fmt.Sprintf("%v", req.Args["agent_id"]))
		if agentID == "" || agentID == "<nil>" {
			agentID = strings.TrimSpace(req.AgentID)
		}
		if agentID == "" {
			agentID = "default"
		}
		enabled := getBoolArg(req.Args, "enabled", true)
		channel := strings.TrimSpace(fmt.Sprintf("%v", req.Args["channel"]))
		if channel == "<nil>" {
			channel = ""
		}
		userID := strings.TrimSpace(fmt.Sprintf("%v", req.Args["user_id"]))
		if userID == "<nil>" {
			userID = ""
		}
		roomID := strings.TrimSpace(fmt.Sprintf("%v", req.Args["room_id"]))
		if roomID == "<nil>" {
			roomID = ""
		}
		sessionID := strings.TrimSpace(fmt.Sprintf("%v", req.Args["session_id"]))
		if sessionID == "<nil>" {
			sessionID = ""
		}

		store, err := openSchedulerStore(req.Workspace, configuredPath)
		if err != nil {
			return nil, err
		}
		job := scheduler.Job{
			ID:        jobID,
			Schedule:  schedule,
			AgentID:   agentID,
			Message:   message,
			Channel:   channel,
			UserID:    userID,
			RoomID:    roomID,
			SessionID: sessionID,
			Enabled:   enabled,
		}
		if err := store.Add(job); err != nil {
			return nil, err
		}
		return map[string]any{
			"added":      true,
			"id":         jobID,
			"agent_id":   agentID,
			"schedule":   schedule,
			"enabled":    enabled,
			"channel":    channel,
			"user_id":    userID,
			"room_id":    roomID,
			"session_id": sessionID,
		}, nil
	}
}

func schedulerRemove(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		id, err := getString(req.Args, "id")
		if err != nil {
			return nil, err
		}
		store, err := openSchedulerStore(req.Workspace, configuredPath)
		if err != nil {
			return nil, err
		}
		if err := store.Remove(id); err != nil {
			return nil, err
		}
		return map[string]any{"removed": true, "id": id}, nil
	}
}

func schedulerPause(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		store, err := openSchedulerStore(req.Workspace, configuredPath)
		if err != nil {
			return nil, err
		}
		id := strings.TrimSpace(fmt.Sprintf("%v", req.Args["id"]))
		if id == "" || id == "<nil>" {
			if err := store.SetPaused(true); err != nil {
				return nil, err
			}
			return map[string]any{"paused": true, "scope": "global"}, nil
		}
		if err := store.SetJobEnabled(id, false); err != nil {
			return nil, err
		}
		return map[string]any{"paused": true, "scope": "job", "id": id}, nil
	}
}

func schedulerResume(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		store, err := openSchedulerStore(req.Workspace, configuredPath)
		if err != nil {
			return nil, err
		}
		id := strings.TrimSpace(fmt.Sprintf("%v", req.Args["id"]))
		if id == "" || id == "<nil>" {
			if err := store.SetPaused(false); err != nil {
				return nil, err
			}
			return map[string]any{"paused": false, "scope": "global"}, nil
		}
		if err := store.SetJobEnabled(id, true); err != nil {
			return nil, err
		}
		return map[string]any{"paused": false, "scope": "job", "id": id}, nil
	}
}

func openSchedulerStore(workspace, configuredPath string) (*scheduler.Store, error) {
	path, err := resolveOpenClawssyPath(workspace, configuredPath, "scheduler", "scheduler", "jobs.json")
	if err != nil {
		return nil, err
	}
	return scheduler.NewStore(path)
}
