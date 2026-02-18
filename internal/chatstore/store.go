package chatstore

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrSessionNotFound = errors.New("chatstore: session not found")
var ErrSessionClosed = errors.New("chatstore: session closed")

const DefaultMaxHistoryCount = 200

const lockAcquireTimeout = 5 * time.Second

const (
	messageScanBufferInit = 256 * 1024
	messageScanBufferMax  = 8 * 1024 * 1024
)

type Store struct {
	agentsRoot string

	// mu guards all process-local state and writes. This does not protect against
	// concurrent writers from other processes.
	mu    sync.RWMutex
	index map[string]string
}

type Session struct {
	SessionID string    `json:"session_id"`
	AgentID   string    `json:"agent_id"`
	Channel   string    `json:"channel"`
	UserID    string    `json:"user_id"`
	RoomID    string    `json:"room_id"`
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ClosedAt  time.Time `json:"closed_at,omitempty"`
}

func (s Session) IsClosed() bool {
	return !s.ClosedAt.IsZero()
}

type Message struct {
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	TS         time.Time `json:"ts"`
	RunID      string    `json:"run_id,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	ToolName   string    `json:"tool_name,omitempty"`
}

type CreateSessionInput struct {
	AgentID string
	Channel string
	UserID  string
	RoomID  string
	Title   string
}

func NewStore(agentsRoot string) (*Store, error) {
	if strings.TrimSpace(agentsRoot) == "" {
		return nil, fmt.Errorf("chatstore: agents root is required")
	}
	absRoot, err := filepath.Abs(agentsRoot)
	if err != nil {
		return nil, fmt.Errorf("chatstore: resolve agents root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return nil, fmt.Errorf("chatstore: create agents root: %w", err)
	}

	s := &Store{agentsRoot: absRoot, index: make(map[string]string)}
	if err := s.loadIndex(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) CreateSession(in CreateSessionInput) (Session, error) {
	if err := validateSegment("agent_id", in.AgentID); err != nil {
		return Session{}, err
	}
	if err := validateSegment("channel", in.Channel); err != nil {
		return Session{}, err
	}
	if strings.TrimSpace(in.UserID) == "" {
		return Session{}, fmt.Errorf("chatstore: user_id is required")
	}
	if strings.TrimSpace(in.RoomID) == "" {
		return Session{}, fmt.Errorf("chatstore: room_id is required")
	}

	now := time.Now().UTC()
	sessionID, err := newSessionID(now)
	if err != nil {
		return Session{}, err
	}
	session := Session{
		SessionID: sessionID,
		AgentID:   in.AgentID,
		Channel:   in.Channel,
		UserID:    in.UserID,
		RoomID:    in.RoomID,
		Title:     strings.TrimSpace(in.Title),
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.sessionDir(in.AgentID, sessionID)
	lockPath := filepath.Join(s.chatRoot(in.AgentID), ".chatstore.lock")
	if err := withCrossProcessLock(lockPath, lockAcquireTimeout, func() error {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("chatstore: create session dir: %w", err)
		}
		if err := writeJSONFile(filepath.Join(dir, "meta.json"), session); err != nil {
			return err
		}
		if err := ensureFile(filepath.Join(dir, "messages.jsonl"), 0o600); err != nil {
			return fmt.Errorf("chatstore: init messages file: %w", err)
		}
		return nil
	}); err != nil {
		return Session{}, err
	}
	s.index[sessionID] = dir
	return session, nil
}

func (s *Store) ListSessions(agentID, userID, roomID, channel string) ([]Session, error) {
	if err := validateSegment("agent_id", agentID); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	chatRoot := s.chatRoot(agentID)
	entries, err := os.ReadDir(chatRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("chatstore: list sessions: %w", err)
	}

	out := make([]Session, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() == "_active" {
			continue
		}
		metaPath := filepath.Join(chatRoot, entry.Name(), "meta.json")
		session, err := readSessionMeta(metaPath)
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				continue
			}
			return nil, err
		}
		if userID != "" && session.UserID != userID {
			continue
		}
		if roomID != "" && session.RoomID != roomID {
			continue
		}
		if channel != "" && session.Channel != channel {
			continue
		}
		out = append(out, session)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})

	return out, nil
}

func (s *Store) AppendMessage(sessionID string, msg Message) error {
	if err := validateSegment("session_id", sessionID); err != nil {
		return err
	}
	if strings.TrimSpace(msg.Role) == "" {
		return fmt.Errorf("chatstore: message role is required")
	}
	if strings.TrimSpace(msg.Content) == "" {
		return fmt.Errorf("chatstore: message content is required")
	}
	if msg.TS.IsZero() {
		msg.TS = time.Now().UTC()
	} else {
		msg.TS = msg.TS.UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir, err := s.sessionDirByIDLocked(sessionID)
	if err != nil {
		return err
	}

	lockPath := filepath.Join(dir, ".chatstore.lock")
	return withCrossProcessLock(lockPath, lockAcquireTimeout, func() error {
		line, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("chatstore: marshal message: %w", err)
		}

		msgPath := filepath.Join(dir, "messages.jsonl")
		f, err := os.OpenFile(msgPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("chatstore: open messages file: %w", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			_ = f.Close()
			return fmt.Errorf("chatstore: append message: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("chatstore: close messages file: %w", err)
		}

		metaPath := filepath.Join(dir, "meta.json")
		session, err := readSessionMeta(metaPath)
		if err != nil {
			return err
		}
		if msg.TS.After(session.UpdatedAt) {
			session.UpdatedAt = msg.TS
		} else {
			session.UpdatedAt = time.Now().UTC()
		}
		return writeJSONFile(metaPath, session)
	})
}

func (s *Store) ReadRecentMessages(sessionID string, limit int) ([]Message, error) {
	if err := validateSegment("session_id", sessionID); err != nil {
		return nil, err
	}
	limit = ClampHistoryCount(limit, DefaultMaxHistoryCount)

	s.mu.RLock()
	dir, err := s.sessionDirByIDLocked(sessionID)
	s.mu.RUnlock()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, "messages.jsonl")
	lockPath := filepath.Join(dir, ".chatstore.lock")
	all := make([]Message, 0)
	if err := withCrossProcessLock(lockPath, lockAcquireTimeout, func() error {
		f, err := os.Open(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("chatstore: open messages: %w", err)
		}
		defer f.Close()

		scanner, err := NewReverseScanner(f, messageScanBufferInit, messageScanBufferMax)
		if err != nil {
			return fmt.Errorf("chatstore: init scanner: %w", err)
		}

		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			var m Message
			if err := json.Unmarshal(line, &m); err != nil {
				continue
			}
			all = append(all, m)
			if len(all) >= limit {
				break
			}
		}
		if err := scanner.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				return fmt.Errorf("chatstore: scan messages: message exceeds %d bytes", messageScanBufferMax)
			}
			return fmt.Errorf("chatstore: scan messages: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Reverse the collected messages to restore chronological order
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}

	return all, nil
}

func (s *Store) GetSession(sessionID string) (Session, error) {
	if err := validateSegment("session_id", sessionID); err != nil {
		return Session{}, err
	}

	s.mu.RLock()
	dir, err := s.sessionDirByIDLocked(sessionID)
	s.mu.RUnlock()
	if err != nil {
		return Session{}, err
	}

	return readSessionMeta(filepath.Join(dir, "meta.json"))
}

func (s *Store) CloseSession(sessionID string) error {
	if err := validateSegment("session_id", sessionID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir, err := s.sessionDirByIDLocked(sessionID)
	if err != nil {
		return err
	}

	lockPath := filepath.Join(dir, ".chatstore.lock")
	return withCrossProcessLock(lockPath, lockAcquireTimeout, func() error {
		metaPath := filepath.Join(dir, "meta.json")
		session, err := readSessionMeta(metaPath)
		if err != nil {
			return err
		}
		if session.IsClosed() {
			return nil
		}
		now := time.Now().UTC()
		session.ClosedAt = now
		if now.After(session.UpdatedAt) {
			session.UpdatedAt = now
		}
		return writeJSONFile(metaPath, session)
	})
}

func (s *Store) SetActiveSessionPointer(agentID, channel, userID, roomID, sessionID string) error {
	if err := validateSegment("agent_id", agentID); err != nil {
		return err
	}
	if err := validateSegment("channel", channel); err != nil {
		return err
	}
	if err := validateSegment("user_id", userID); err != nil {
		return err
	}
	if err := validateSegment("room_id", roomID); err != nil {
		return err
	}
	if err := validateSegment("session_id", sessionID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir, err := s.sessionDirByIDLocked(sessionID)
	if err != nil {
		return err
	}
	session, err := readSessionMeta(filepath.Join(dir, "meta.json"))
	if err != nil {
		return err
	}
	if session.AgentID != agentID {
		return ErrSessionNotFound
	}
	if session.IsClosed() {
		return ErrSessionClosed
	}

	path := s.activePointerPath(agentID, channel, userID, roomID)
	payload := map[string]string{"session_id": sessionID}
	return withCrossProcessLock(path+".lock", lockAcquireTimeout, func() error {
		return writeJSONFile(path, payload)
	})
}

func (s *Store) GetActiveSessionPointer(agentID, channel, userID, roomID string) (string, error) {
	if err := validateSegment("agent_id", agentID); err != nil {
		return "", err
	}
	if err := validateSegment("channel", channel); err != nil {
		return "", err
	}
	if err := validateSegment("user_id", userID); err != nil {
		return "", err
	}
	if err := validateSegment("room_id", roomID); err != nil {
		return "", err
	}

	path := s.activePointerPath(agentID, channel, userID, roomID)
	lockPath := path + ".lock"
	var b []byte
	if err := withCrossProcessLock(lockPath, lockAcquireTimeout, func() error {
		readBytes, err := readFileWithBackup(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ErrSessionNotFound
			}
			return fmt.Errorf("chatstore: read active pointer: %w", err)
		}
		b = readBytes
		return nil
	}); err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return "", ErrSessionNotFound
		}
		return "", err
	}
	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		return "", fmt.Errorf("chatstore: parse active pointer: %w", err)
	}
	if payload.SessionID == "" {
		return "", ErrSessionNotFound
	}
	return payload.SessionID, nil
}

func ClampHistoryCount(requested, max int) int {
	if max <= 0 {
		max = DefaultMaxHistoryCount
	}
	if requested <= 0 || requested > max {
		return max
	}
	return requested
}

func (s *Store) loadIndex() error {
	pattern := filepath.Join(s.agentsRoot, "*", "memory", "chats", "*", "meta.json")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("chatstore: build index glob: %w", err)
	}
	for _, p := range paths {
		dir := filepath.Dir(p)
		sessionID := filepath.Base(dir)
		if sessionID == "_active" || sessionID == "" {
			continue
		}
		s.index[sessionID] = dir
	}
	return nil
}

func (s *Store) sessionDir(agentID, sessionID string) string {
	return filepath.Join(s.chatRoot(agentID), sessionID)
}

func (s *Store) chatRoot(agentID string) string {
	return filepath.Join(s.agentsRoot, agentID, "memory", "chats")
}

func (s *Store) activePointerPath(agentID, channel, userID, roomID string) string {
	return filepath.Join(s.chatRoot(agentID), "_active", channel, userID, roomID+".json")
}

func (s *Store) sessionDirByIDLocked(sessionID string) (string, error) {
	if dir, ok := s.index[sessionID]; ok {
		return dir, nil
	}

	pattern := filepath.Join(s.agentsRoot, "*", "memory", "chats", sessionID)
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("chatstore: find session: %w", err)
	}
	for _, p := range paths {
		metaPath := filepath.Join(p, "meta.json")
		if _, err := os.Stat(metaPath); err == nil {
			s.index[sessionID] = p
			return p, nil
		}
	}
	return "", ErrSessionNotFound
}

func readSessionMeta(path string) (Session, error) {
	b, err := readFileWithBackup(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Session{}, ErrSessionNotFound
		}
		return Session{}, fmt.Errorf("chatstore: read session meta: %w", err)
	}
	var session Session
	if err := json.Unmarshal(b, &session); err != nil {
		return Session{}, fmt.Errorf("chatstore: parse session meta: %w", err)
	}
	if session.SessionID == "" {
		return Session{}, fmt.Errorf("chatstore: invalid session meta")
	}
	return session, nil
}

func writeJSONFile(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("chatstore: marshal json: %w", err)
	}
	b = append(b, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("chatstore: create parent dir: %w", err)
	}

	if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
		_ = os.WriteFile(path+".bak", existing, 0o600)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-chatstore-*")
	if err != nil {
		return fmt.Errorf("chatstore: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chatstore: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chatstore: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("chatstore: close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chatstore: chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("chatstore: rename temp file: %w", err)
	}
	return nil
}

func readFileWithBackup(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		if json.Valid(b) {
			return b, nil
		}
		backup, backupErr := os.ReadFile(path + ".bak")
		if backupErr == nil && json.Valid(backup) {
			return backup, nil
		}
		return nil, fmt.Errorf("chatstore: invalid json at %s", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	backup, backupErr := os.ReadFile(path + ".bak")
	if backupErr == nil && json.Valid(backup) {
		return backup, nil
	}
	return nil, err
}

func ensureFile(path string, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE, mode)
	if err != nil {
		return err
	}
	return f.Close()
}

func validateSegment(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("chatstore: %s is required", name)
	}
	if strings.Contains(trimmed, "..") || strings.ContainsRune(trimmed, '/') || strings.ContainsRune(trimmed, '\\') {
		return fmt.Errorf("chatstore: invalid %s", name)
	}
	return nil
}

func newSessionID(now time.Time) (string, error) {
	randBytes := make([]byte, 4)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("chatstore: generate session id: %w", err)
	}
	return fmt.Sprintf("chat_%d_%s", now.UnixNano(), hex.EncodeToString(randBytes)), nil
}
