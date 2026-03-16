package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	EventRunStart          = "run.start"
	EventRunEnd            = "run.end"
	EventToolCall          = "tool.call"
	EventToolResult        = "tool.result"
	EventToolCallbackError = "tool.callback_error"
	EventPolicyDeny        = "policy.denied"
	EventDecisionRecord    = "decision.record"
	defaultFileMode        = 0o600
	defaultDirMode         = 0o755
	defaultLineBreak       = '\n'
	defaultFlushInterval   = 2 * time.Second
)

type Event struct {
	Timestamp   time.Time      `json:"ts"`
	Type        string         `json:"type"`
	RunID       string         `json:"run_id,omitempty"`
	InstanceID  string         `json:"instance_id,omitempty"`
	AgentID     string         `json:"agent_id,omitempty"`
	ParentRunID string         `json:"parent_run_id,omitempty"`
	Tool        string         `json:"tool,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
}

type Logger struct {
	path   string
	redact func(any) any
	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer

	flushInterval time.Duration
	lastFlushAt   time.Time
}

func NewLogger(path string, redact func(any) any) (*Logger, error) {
	return newLoggerWithFlushInterval(path, redact, defaultFlushInterval)
}

func newLoggerWithFlushInterval(path string, redact func(any) any, flushInterval time.Duration) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), defaultDirMode); err != nil {
		return nil, err
	}
	if redact == nil {
		redact = func(v any) any { return v }
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, defaultFileMode)
	if err != nil {
		return nil, err
	}
	if flushInterval <= 0 {
		flushInterval = defaultFlushInterval
	}
	return &Logger{
		path:          path,
		redact:        redact,
		file:          f,
		writer:        bufio.NewWriterSize(f, 32*1024),
		flushInterval: flushInterval,
		lastFlushAt:   time.Now().UTC(),
	}, nil
}

func (l *Logger) LogEvent(ctx context.Context, eventType string, fields map[string]any) error {
	_ = ctx
	e := Event{Type: eventType, Timestamp: time.Now().UTC()}

	if len(fields) > 0 {
		if runID, ok := fields["run_id"].(string); ok {
			e.RunID = runID
		}
		if agentID, ok := fields["agent_id"].(string); ok {
			e.AgentID = agentID
		}
		if instanceID, ok := fields["instance_id"].(string); ok {
			e.InstanceID = instanceID
		}
		if parentRunID, ok := fields["parent_run_id"].(string); ok {
			e.ParentRunID = parentRunID
		}
		if tool, ok := fields["tool"].(string); ok {
			e.Tool = tool
		}
		payload := make(map[string]any)
		for k, v := range fields {
			if k == "run_id" || k == "instance_id" || k == "agent_id" || k == "parent_run_id" || k == "tool" {
				continue
			}
			payload[k] = v
		}
		if len(payload) > 0 {
			if rv, ok := l.redact(payload).(map[string]any); ok {
				e.Payload = rv
			} else {
				e.Payload = payload
			}
		}
	}

	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, defaultLineBreak)

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil || l.writer == nil {
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, defaultFileMode)
		if err != nil {
			return err
		}
		l.file = f
		l.writer = bufio.NewWriterSize(f, 32*1024)
		l.lastFlushAt = time.Now().UTC()
	}

	if _, err := l.writer.Write(line); err != nil {
		return err
	}
	if eventType == EventRunEnd {
		return l.flushLocked(true)
	}
	if l.shouldPeriodicFlushLocked(time.Now().UTC()) {
		return l.flushLocked(true)
	}
	return nil
}

func (l *Logger) shouldPeriodicFlushLocked(now time.Time) bool {
	if l.flushInterval <= 0 {
		return false
	}
	if l.lastFlushAt.IsZero() {
		return true
	}
	return now.Sub(l.lastFlushAt) >= l.flushInterval
}

func (l *Logger) flushLocked(syncDisk bool) error {
	if l.writer != nil {
		if err := l.writer.Flush(); err != nil {
			return err
		}
	}
	if syncDisk && l.file != nil {
		if err := l.file.Sync(); err != nil {
			return err
		}
	}
	l.lastFlushAt = time.Now().UTC()
	return nil
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	if err := l.flushLocked(true); err != nil {
		_ = l.file.Close()
		l.file = nil
		l.writer = nil
		return err
	}
	err := l.file.Close()
	l.file = nil
	l.writer = nil
	return err
}
