package chat

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"openclawssy/internal/chatstore"
	"openclawssy/internal/messagecontent"
)

var (
	ErrNotAllowlisted = errors.New("chat sender is not allowlisted")
	ErrRateLimited    = errors.New("chat sender is rate limited")
)

type Message struct {
	UserID       string
	RoomID       string
	AgentID      string
	Source       string
	Text         string
	ContentParts []messagecontent.Part
	ThinkingMode string
}

type Result struct {
	ID        string
	Status    string
	Response  string
	SessionID string
}

type QueuedRun struct {
	ID     string
	Status string
}

type QueueFunc func(ctx context.Context, agentID, message string, contentParts []messagecontent.Part, source, sessionID, thinkingMode string) (QueuedRun, error)

type Connector struct {
	Allowlist      *Allowlist
	RateLimiter    *RateLimiter
	GlobalLimiter  *RateLimiter
	Queue          QueueFunc
	DefaultAgentID string
	Store          *chatstore.Store
	HistoryLimit   int
}

func (c *Connector) HandleMessage(ctx context.Context, msg Message) (Result, error) {
	if c == nil || c.Queue == nil {
		return Result{}, errors.New("chat queue is not configured")
	}
	if c.Store == nil {
		return Result{}, errors.New("chat store is not configured")
	}
	parts := messagecontent.Normalize(msg.ContentParts)
	if strings.TrimSpace(msg.Text) == "" {
		msg.Text = messagecontent.VisibleText(parts)
	}
	if strings.TrimSpace(msg.UserID) == "" || (strings.TrimSpace(msg.Text) == "" && len(parts) == 0) {
		return Result{}, errors.New("user id and message content are required")
	}
	if c.Allowlist != nil && !c.Allowlist.MessageAllowed(msg.UserID, msg.RoomID) {
		return Result{}, ErrNotAllowlisted
	}
	if c.GlobalLimiter != nil {
		if allowed, retryAfter := c.GlobalLimiter.AllowWithDetails("global"); !allowed {
			return Result{}, NewRateLimitError("global", retryAfter)
		}
	}
	if c.RateLimiter != nil {
		key := fmt.Sprintf("%s:%s", msg.UserID, msg.RoomID)
		if allowed, retryAfter := c.RateLimiter.AllowWithDetails(key); !allowed {
			return Result{}, NewRateLimitError("sender", retryAfter)
		}
	}

	source := strings.TrimSpace(msg.Source)
	if source == "" {
		source = "chat"
	}
	roomID := strings.TrimSpace(msg.RoomID)
	if roomID == "" {
		roomID = source
	}

	text := strings.TrimSpace(msg.Text)

	agentID := strings.TrimSpace(msg.AgentID)
	if agentID == "" {
		activeAgent, pointerErr := c.Store.GetActiveAgentPointer(source, msg.UserID, roomID)
		if pointerErr == nil {
			agentID = strings.TrimSpace(activeAgent)
		}
	}
	if agentID == "" {
		agentID = strings.TrimSpace(c.DefaultAgentID)
	}
	if agentID == "" {
		agentID = "default"
	}

	if text == "/agents" {
		agents, err := c.Store.ListAgents()
		if err != nil {
			return Result{}, err
		}
		if len(agents) == 0 {
			return Result{Response: "No agents found"}, nil
		}
		lines := make([]string, 0, len(agents)+1)
		lines = append(lines, "Available agents:")
		for _, candidate := range agents {
			if candidate == agentID {
				lines = append(lines, "- "+candidate+" (active)")
				continue
			}
			lines = append(lines, "- "+candidate)
		}
		return Result{Response: strings.Join(lines, "\n")}, nil
	}
	if text == "/agent" {
		return Result{Response: "Active agent: " + agentID}, nil
	}
	if strings.HasPrefix(text, "/agent ") {
		parts := strings.Fields(text)
		if len(parts) != 2 {
			return Result{Response: "Usage: /agent <agent_id>"}, nil
		}
		nextAgent := strings.TrimSpace(parts[1])
		if nextAgent == "" || strings.Contains(nextAgent, "..") || strings.ContainsRune(nextAgent, '/') || strings.ContainsRune(nextAgent, '\\') {
			return Result{Response: "Invalid agent id"}, nil
		}
		agents, err := c.Store.ListAgents()
		if err != nil {
			return Result{}, err
		}
		found := false
		for _, candidate := range agents {
			if candidate == nextAgent {
				found = true
				break
			}
		}
		if !found {
			return Result{Response: "Agent not found: " + nextAgent + " (create it first or run /agents)"}, nil
		}
		if err := c.Store.SetActiveAgentPointer(source, msg.UserID, roomID, nextAgent); err != nil {
			return Result{}, err
		}
		return Result{Response: "Switched active agent to: " + nextAgent}, nil
	}

	if text == "/new" {
		session, err := c.createAndSetActiveSession(agentID, source, msg.UserID, roomID)
		if err != nil {
			return Result{}, err
		}
		return Result{Response: "Started new chat: " + session.SessionID, SessionID: session.SessionID}, nil
	}
	if strings.HasPrefix(text, "/resume") {
		parts := strings.Fields(text)
		if len(parts) != 2 {
			return Result{Response: "Usage: /resume <session_id>"}, nil
		}
		session, err := c.Store.GetSession(parts[1])
		if err != nil {
			if errors.Is(err, chatstore.ErrSessionNotFound) {
				return Result{Response: "Session not found: " + parts[1]}, nil
			}
			return Result{}, err
		}
		if session.AgentID != agentID || session.Channel != source || session.UserID != msg.UserID || session.RoomID != roomID {
			return Result{Response: "Session not available in this chat context"}, nil
		}
		if session.IsClosed() {
			return Result{Response: "Session is closed: " + parts[1] + " (use /new to continue)"}, nil
		}
		if err := c.Store.SetActiveSessionPointer(agentID, source, msg.UserID, roomID, parts[1]); err != nil {
			return Result{}, err
		}
		return Result{Response: "Resumed chat: " + parts[1], SessionID: parts[1]}, nil
	}
	if text == "/chats" || text == "/sessions" {
		sessions, err := c.Store.ListSessions(agentID, msg.UserID, roomID, source)
		if err != nil {
			return Result{}, err
		}
		if len(sessions) == 0 {
			return Result{Response: "No chats found"}, nil
		}
		if len(sessions) > 10 {
			sessions = sessions[:10]
		}
		lines := make([]string, 0, len(sessions)+1)
		lines = append(lines, "Recent chats:")
		for i, session := range sessions {
			line := strconv.Itoa(i+1) + ". " + session.SessionID
			if session.IsClosed() {
				line += " (closed)"
			}
			lines = append(lines, line)
		}
		return Result{Response: strings.Join(lines, "\n")}, nil
	}

	session, err := c.resolveOrCreateActiveSession(agentID, source, msg.UserID, roomID)
	if err != nil {
		return Result{}, err
	}

	if err := c.Store.AppendMessage(session.SessionID, chatstore.Message{Role: "user", Content: msg.Text, ContentParts: parts}); err != nil {
		return Result{}, err
	}

	queued, err := c.Queue(ctx, agentID, msg.Text, parts, source, session.SessionID, msg.ThinkingMode)
	if err != nil {
		return Result{}, err
	}

	return Result{
		ID:        queued.ID,
		Status:    queued.Status,
		SessionID: session.SessionID,
		Response:  queuedStatusMessage(queued.ID, queued.Status),
	}, nil
}

func queuedStatusMessage(runID, status string) string {
	runID = strings.TrimSpace(runID)
	status = strings.TrimSpace(status)
	if status == "" {
		status = "queued"
	}
	if runID == "" {
		return "Working on it now. I will follow up here when it completes or if it fails."
	}
	return "Working on it now. Run " + runID + " is " + status + ". I will follow up here when it completes or if it fails."
}

func (c *Connector) createAndSetActiveSession(agentID, source, userID, roomID string) (chatstore.Session, error) {
	session, err := c.Store.CreateSession(chatstore.CreateSessionInput{AgentID: agentID, Channel: source, UserID: userID, RoomID: roomID})
	if err != nil {
		return chatstore.Session{}, err
	}
	if err := c.Store.SetActiveSessionPointer(agentID, source, userID, roomID, session.SessionID); err != nil {
		return chatstore.Session{}, err
	}
	return session, nil
}

func (c *Connector) resolveOrCreateActiveSession(agentID, source, userID, roomID string) (chatstore.Session, error) {
	activeID, err := c.Store.GetActiveSessionPointer(agentID, source, userID, roomID)
	if err != nil {
		if errors.Is(err, chatstore.ErrSessionNotFound) {
			return c.createAndSetActiveSession(agentID, source, userID, roomID)
		}
		return chatstore.Session{}, err
	}

	session, err := c.Store.GetSession(activeID)
	if err != nil {
		if errors.Is(err, chatstore.ErrSessionNotFound) {
			return c.createAndSetActiveSession(agentID, source, userID, roomID)
		}
		return chatstore.Session{}, err
	}
	if session.AgentID != agentID || session.Channel != source || session.UserID != userID || session.RoomID != roomID {
		return c.createAndSetActiveSession(agentID, source, userID, roomID)
	}
	if session.IsClosed() {
		return c.createAndSetActiveSession(agentID, source, userID, roomID)
	}
	return session, nil
}
