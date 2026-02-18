package tools

import (
	"context"
	"errors"
	"strings"

	"openclawssy/internal/chatstore"
)

const (
	defaultSessionListLimit = 50
	maxSessionListLimit     = 200
)

func registerSessionTools(reg *Registry, configuredPath string) error {
	if err := reg.Register(ToolSpec{
		Name:        "session.list",
		Description: "List chat sessions",
		ArgTypes: map[string]ArgType{
			"agent_id":       ArgTypeString,
			"user_id":        ArgTypeString,
			"room_id":        ArgTypeString,
			"channel":        ArgTypeString,
			"limit":          ArgTypeNumber,
			"include_closed": ArgTypeBool,
		},
	}, sessionList(configuredPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "session.close",
		Description: "Close chat session by id",
		Required:    []string{"session_id"},
		ArgTypes: map[string]ArgType{
			"session_id": ArgTypeString,
			"id":         ArgTypeString,
		},
	}, sessionClose(configuredPath)); err != nil {
		return err
	}
	return nil
}

func sessionList(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		store, err := openChatStore(req.Workspace, configuredPath)
		if err != nil {
			return nil, err
		}

		agentID := strings.TrimSpace(valueString(req.Args, "agent_id"))
		if agentID == "" {
			agentID = strings.TrimSpace(req.AgentID)
		}
		if agentID == "" {
			agentID = "default"
		}

		userID := strings.TrimSpace(valueString(req.Args, "user_id"))
		roomID := strings.TrimSpace(valueString(req.Args, "room_id"))
		channel := strings.TrimSpace(valueString(req.Args, "channel"))

		sessions, err := store.ListSessions(agentID, userID, roomID, channel)
		if err != nil {
			return nil, err
		}

		includeClosed := getBoolArg(req.Args, "include_closed", false)
		if !includeClosed {
			filtered := make([]chatstore.Session, 0, len(sessions))
			for _, session := range sessions {
				if session.IsClosed() {
					continue
				}
				filtered = append(filtered, session)
			}
			sessions = filtered
		}

		limit := getIntArg(req.Args, "limit", defaultSessionListLimit)
		if limit <= 0 {
			limit = defaultSessionListLimit
		}
		if limit > maxSessionListLimit {
			limit = maxSessionListLimit
		}
		if len(sessions) > limit {
			sessions = sessions[:limit]
		}

		return map[string]any{
			"agent_id":       agentID,
			"user_id":        userID,
			"room_id":        roomID,
			"channel":        channel,
			"include_closed": includeClosed,
			"count":          len(sessions),
			"sessions":       sessions,
		}, nil
	}
}

func sessionClose(configuredPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		store, err := openChatStore(req.Workspace, configuredPath)
		if err != nil {
			return nil, err
		}

		sessionID := strings.TrimSpace(valueString(req.Args, "session_id"))
		if sessionID == "" {
			sessionID = strings.TrimSpace(valueString(req.Args, "id"))
		}
		if sessionID == "" {
			return nil, errors.New("session_id is required")
		}

		session, err := store.GetSession(sessionID)
		if err != nil {
			if errors.Is(err, chatstore.ErrSessionNotFound) {
				return map[string]any{"session_id": sessionID, "found": false, "closed": false}, nil
			}
			return nil, err
		}
		if session.IsClosed() {
			return map[string]any{"session_id": sessionID, "found": true, "closed": false, "already_closed": true}, nil
		}

		if err := store.CloseSession(sessionID); err != nil {
			return nil, err
		}

		updated, err := store.GetSession(sessionID)
		if err != nil {
			return nil, err
		}

		return map[string]any{
			"session_id": sessionID,
			"found":      true,
			"closed":     true,
			"closed_at":  updated.ClosedAt,
		}, nil
	}
}

func openChatStore(workspace, configuredPath string) (*chatstore.Store, error) {
	path, err := resolveOpenClawssyPath(workspace, configuredPath, "chatstore", "agents")
	if err != nil {
		return nil, err
	}
	return chatstore.NewStore(path)
}

func valueString(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}
