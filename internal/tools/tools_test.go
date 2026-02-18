package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/policy"
	"openclawssy/internal/scheduler"
	"openclawssy/internal/secrets"
)

type fakePolicy struct {
	deny bool
	ws   string
}

func (p fakePolicy) CheckTool(agentID, tool string) error {
	if p.deny {
		return &ToolError{Code: ErrCodePolicyDenied, Tool: tool, Message: "blocked"}
	}
	return nil
}

func (p fakePolicy) ResolveReadPath(workspace, target string) (string, error) {
	if filepath.IsAbs(target) {
		return target, nil
	}
	return filepath.Join(workspace, target), nil
}

func (p fakePolicy) ResolveWritePath(workspace, target string) (string, error) {
	if filepath.IsAbs(target) {
		return target, nil
	}
	return filepath.Join(workspace, target), nil
}

type memAudit struct {
	events []string
	recs   []auditRecord
}

type auditRecord struct {
	eventType string
	fields    map[string]any
}

type fakeShell struct{}

func (fakeShell) Exec(ctx context.Context, command string, args []string) (string, string, int, error) {
	_ = ctx
	return command + " " + args[0], "", 0, nil
}

type fallbackShell struct {
	calls []string
}

func (s *fallbackShell) Exec(ctx context.Context, command string, args []string) (string, string, int, error) {
	_ = ctx
	s.calls = append(s.calls, command)
	if command == "bash" {
		return "", "", -1, errors.New(`exec: "bash": executable file not found in $PATH`)
	}
	if command == "/bin/bash" {
		return "venv ready", "", 0, nil
	}
	if command == "/usr/bin/bash" {
		return "", "", -1, errors.New(`fork/exec /usr/bin/bash: no such file or directory`)
	}
	if command == "sh" {
		return "venv ready", "", 0, nil
	}
	return "", "", 127, errors.New("unexpected command")
}

type shOnlyFallbackShell struct {
	calls []string
}

func (s *shOnlyFallbackShell) Exec(ctx context.Context, command string, args []string) (string, string, int, error) {
	_ = ctx
	s.calls = append(s.calls, command)
	if command == "bash" {
		return "", "", -1, errors.New(`exec: "bash": executable file not found in $PATH`)
	}
	if command == "/bin/bash" {
		return "", "", -1, errors.New(`fork/exec /bin/bash: no such file or directory`)
	}
	if command == "/usr/bin/bash" {
		return "", "", -1, errors.New(`fork/exec /usr/bin/bash: no such file or directory`)
	}
	if command == "sh" {
		return "venv ready via sh", "", 0, nil
	}
	return "", "", 127, errors.New("unexpected command")
}

type exitStatusShell struct{}

func (exitStatusShell) Exec(ctx context.Context, command string, args []string) (string, string, int, error) {
	_ = ctx
	_ = command
	_ = args
	return "scan partial output", "permission denied", 1, errors.New("exit status 1")
}

func (m *memAudit) LogEvent(ctx context.Context, eventType string, fields map[string]any) error {
	_ = ctx
	copyFields := map[string]any{}
	for k, v := range fields {
		copyFields[k] = v
	}
	m.events = append(m.events, eventType)
	m.recs = append(m.recs, auditRecord{eventType: eventType, fields: copyFields})
	return nil
}

func TestRegistryRegisterAndNotFound(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	if err := reg.Register(ToolSpec{Name: "ok"}, func(ctx context.Context, req Request) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "missing", ".", nil)
	if err == nil {
		t.Fatalf("expected not found error")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodeNotFound {
		t.Fatalf("expected %s, got %s", ErrCodeNotFound, toolErr.Code)
	}
}

func TestRegistryMissingRequiredFieldUsesCanonicalInputInvalidCode(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	if err := reg.Register(ToolSpec{Name: "needs_path", Required: []string{"path"}}, func(ctx context.Context, req Request) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "needs_path", ".", map[string]any{})
	if err == nil {
		t.Fatalf("expected input invalid error")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodeInvalidInput {
		t.Fatalf("expected %s, got %s", ErrCodeInvalidInput, toolErr.Code)
	}
}

func TestRegistryRejectsWrongTypeForRequiredField(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	if err := reg.Register(ToolSpec{Name: "needs_path", Required: []string{"path"}, ArgTypes: map[string]ArgType{"path": ArgTypeString}}, func(ctx context.Context, req Request) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "needs_path", ".", map[string]any{"path": 42})
	if err == nil {
		t.Fatalf("expected invalid-input type error")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodeInvalidInput {
		t.Fatalf("expected %s, got %s", ErrCodeInvalidInput, toolErr.Code)
	}
	if !strings.Contains(toolErr.Message, "invalid type") {
		t.Fatalf("expected invalid type message, got %q", toolErr.Message)
	}
}

func TestRegistryPolicyDenied(t *testing.T) {
	a := &memAudit{}
	reg := NewRegistry(fakePolicy{deny: true}, a)
	if err := reg.Register(ToolSpec{Name: "t"}, func(ctx context.Context, req Request) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "t", ".", nil); err == nil {
		t.Fatalf("expected denied error")
	}

	foundDenied := false
	for _, e := range a.events {
		if e == "policy.denied" {
			foundDenied = true
		}
	}
	if !foundDenied {
		t.Fatalf("expected policy.denied audit event")
	}
}

func TestRegistryTimeoutIsStructuredAndAudited(t *testing.T) {
	a := &memAudit{}
	reg := NewRegistry(fakePolicy{}, a)
	if err := reg.Register(ToolSpec{Name: "slow"}, func(ctx context.Context, req Request) (map[string]any, error) {
		_ = req
		<-ctx.Done()
		return nil, ctx.Err()
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := reg.Execute(ctx, "agent", "slow", ".", nil)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected tool error, got %T", err)
	}
	if toolErr.Code != ErrCodeTimeout {
		t.Fatalf("expected timeout code, got %s", toolErr.Code)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "timeout") {
		t.Fatalf("expected structured timeout error text, got %q", err.Error())
	}

	foundAuditedTimeout := false
	for _, rec := range a.recs {
		if rec.eventType != "tool.result" {
			continue
		}
		errText, _ := rec.fields["error"].(string)
		if strings.Contains(strings.ToLower(errText), "timeout") {
			foundAuditedTimeout = true
			break
		}
	}
	if !foundAuditedTimeout {
		t.Fatalf("expected timeout to be present in tool.result audit event, records=%#v", a.recs)
	}
}

func TestCoreFsTools(t *testing.T) {
	ws := t.TempDir()
	audit := &memAudit{}
	reg := NewRegistry(fakePolicy{}, audit)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	writeRes, err := reg.Execute(context.Background(), "agent", "fs.write", ws, map[string]any{
		"path":    "hello.txt",
		"content": "hello world",
	})
	if err != nil {
		t.Fatalf("fs.write: %v", err)
	}
	if got := writeRes["lines"]; got != 1 {
		t.Fatalf("expected fs.write lines=1, got %#v", got)
	}
	if got := writeRes["summary"]; got != "wrote 1 line(s) to hello.txt" {
		t.Fatalf("unexpected fs.write summary: %#v", got)
	}

	readRes, err := reg.Execute(context.Background(), "agent", "fs.read", ws, map[string]any{"path": "hello.txt"})
	if err != nil {
		t.Fatalf("fs.read: %v", err)
	}
	if got := readRes["content"]; got != "hello world" {
		t.Fatalf("unexpected read content: %#v", got)
	}

	_, err = reg.Execute(context.Background(), "agent", "fs.edit", ws, map[string]any{
		"path": "hello.txt",
		"old":  "world",
		"new":  "there",
	})
	if err != nil {
		t.Fatalf("fs.edit: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(ws, "hello.txt"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "hello there" {
		t.Fatalf("unexpected edited content: %q", string(b))
	}

	appendRes, err := reg.Execute(context.Background(), "agent", "fs.append", ws, map[string]any{
		"path":    "hello.txt",
		"content": "\nagain",
	})
	if err != nil {
		t.Fatalf("fs.append: %v", err)
	}
	if got := appendRes["lines_appended"]; got != 2 {
		t.Fatalf("expected fs.append lines_appended=2, got %#v", got)
	}
	if got := appendRes["summary"]; got != "appended 2 line(s) to hello.txt" {
		t.Fatalf("unexpected fs.append summary: %#v", got)
	}

	b, err = os.ReadFile(filepath.Join(ws, "hello.txt"))
	if err != nil {
		t.Fatalf("read appended file: %v", err)
	}
	if string(b) != "hello there\nagain" {
		t.Fatalf("unexpected appended content: %q", string(b))
	}

	if _, err := reg.Execute(context.Background(), "agent", "fs.list", ws, map[string]any{"path": "."}); err != nil {
		t.Fatalf("fs.list: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "code.search", ws, map[string]any{"pattern": "hello"}); err != nil {
		t.Fatalf("code.search: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "time.now", ws, nil); err != nil {
		t.Fatalf("time.now: %v", err)
	}
}

func TestShellExecTool(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	reg.SetShellExecutor(fakeShell{})
	reg.SetShellAllowedCommands([]string{"echo"})
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	res, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"ok"},
	})
	if err != nil {
		t.Fatalf("shell.exec: %v", err)
	}
	if res["stdout"] != "echo ok" {
		t.Fatalf("unexpected stdout: %#v", res["stdout"])
	}
}

func TestShellExecFallsBackToShWhenBashUnavailable(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	shell := &fallbackShell{}
	reg.SetShellExecutor(shell)
	reg.SetShellAllowedCommands([]string{"bash"})
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	res, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "bash",
		"args":    []any{"-lc", "python -m venv .venv"},
	})
	if err != nil {
		t.Fatalf("shell.exec expected fallback success, got error: %v", err)
	}
	if len(shell.calls) != 2 || shell.calls[0] != "bash" || shell.calls[1] != "/bin/bash" {
		t.Fatalf("expected bash then /bin/bash fallback, got %#v", shell.calls)
	}
	if res["stdout"] != "venv ready" {
		t.Fatalf("unexpected stdout after fallback: %#v", res["stdout"])
	}
	if res["shell_fallback"] != "/bin/bash" {
		t.Fatalf("expected shell_fallback=/bin/bash, got %#v", res["shell_fallback"])
	}
}

func TestShellExecFallsBackToShWhenBashBinaryMissing(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	shell := &shOnlyFallbackShell{}
	reg.SetShellExecutor(shell)
	reg.SetShellAllowedCommands([]string{"bash"})
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	res, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "bash",
		"args":    []any{"-lc", "python -m venv .venv"},
	})
	if err != nil {
		t.Fatalf("shell.exec expected sh fallback success, got error: %v", err)
	}
	if len(shell.calls) != 4 || shell.calls[0] != "bash" || shell.calls[1] != "/bin/bash" || shell.calls[2] != "/usr/bin/bash" || shell.calls[3] != "sh" {
		t.Fatalf("expected bash -> /bin/bash -> /usr/bin/bash -> sh order, got %#v", shell.calls)
	}
	if res["stdout"] != "venv ready via sh" {
		t.Fatalf("unexpected stdout after sh fallback: %#v", res["stdout"])
	}
	if res["shell_fallback"] != "sh" {
		t.Fatalf("expected shell_fallback=sh, got %#v", res["shell_fallback"])
	}
}

func TestShellExecTreatsExitStatusAsResultNotToolFailure(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	reg.SetShellExecutor(exitStatusShell{})
	reg.SetShellAllowedCommands([]string{"bash"})
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	res, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "bash",
		"args":    []any{"-lc", "nmap -sS 127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("expected non-zero exit status to return structured result without tool failure, got: %v", err)
	}
	if res["exit_code"] != 1 {
		t.Fatalf("expected exit_code=1, got %#v", res["exit_code"])
	}
	if strings.TrimSpace(res["error"].(string)) != "exit status 1" {
		t.Fatalf("expected structured error field with exit status, got %#v", res["error"])
	}
}

func TestFsEditLinePatch(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	_, err := reg.Execute(context.Background(), "agent", "fs.edit", ws, map[string]any{
		"path":  "a.txt",
		"edits": []any{map[string]any{"startLine": 2, "endLine": 2, "newText": "TWO"}},
	})
	if err != nil {
		t.Fatalf("fs.edit patch: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(ws, "a.txt"))
	if string(b) != "one\nTWO\nthree\n" {
		t.Fatalf("unexpected patched content: %q", string(b))
	}
}

func TestFsEditUnifiedDiffSingleHunk(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	patch := "--- a/a.txt\n+++ b/a.txt\n@@ -2,1 +2,1 @@\n-two\n+TWO\n"
	res, err := reg.Execute(context.Background(), "agent", "fs.edit", ws, map[string]any{
		"path":  "a.txt",
		"patch": patch,
	})
	if err != nil {
		t.Fatalf("fs.edit unified diff: %v", err)
	}
	if got := res["mode"]; got != "unified_diff" {
		t.Fatalf("expected unified_diff mode, got %#v", got)
	}
	if got := res["applied_edits"]; got != 1 {
		t.Fatalf("expected applied_edits=1, got %#v", got)
	}
	b, _ := os.ReadFile(filepath.Join(ws, "a.txt"))
	if string(b) != "one\nTWO\nthree\n" {
		t.Fatalf("unexpected unified-diff content: %q", string(b))
	}
}

func TestFsEditUnifiedDiffRejectsMixedModes(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	_, err := reg.Execute(context.Background(), "agent", "fs.edit", ws, map[string]any{
		"path":  "a.txt",
		"patch": "@@ -1,1 +1,1 @@\n-one\n+ONE\n",
		"edits": []any{map[string]any{"startLine": 1, "endLine": 1, "newText": "ONE"}},
	})
	if err == nil {
		t.Fatalf("expected mixed-mode validation error")
	}
	if !strings.Contains(err.Error(), "exactly one edit mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFsEditUnifiedDiffRejectsContextMismatch(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	_, err := reg.Execute(context.Background(), "agent", "fs.edit", ws, map[string]any{
		"path":  "a.txt",
		"patch": "@@ -2,1 +2,1 @@\n-three\n+THREE\n",
	})
	if err == nil {
		t.Fatalf("expected context mismatch error")
	}
	if !strings.Contains(err.Error(), "context mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFsEditUnifiedDiffRejectsNoHunks(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}
	_, err := reg.Execute(context.Background(), "agent", "fs.edit", ws, map[string]any{
		"path":  "a.txt",
		"patch": "--- a/a.txt\n+++ b/a.txt\n",
	})
	if err == nil {
		t.Fatalf("expected no-hunks error")
	}
	if !strings.Contains(err.Error(), "does not contain any @@ hunks") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFsWriteRejectsWorkspaceControlPlaneFilename(t *testing.T) {
	ws := t.TempDir()
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent-123", "fs.write", ws, map[string]any{
		"path":    filepath.Join("notes", "SOUL.md"),
		"content": "new content",
	})
	if err == nil {
		t.Fatalf("expected control-plane filename guard error")
	}
	msg := err.Error()
	for _, needle := range []string{
		"does not control agent behavior",
		".openclawssy/agents/<agent_id>/SOUL.md",
		"dashboard Agent Files",
	} {
		if !strings.Contains(msg, needle) {
			t.Fatalf("expected error to contain %q, got %q", needle, msg)
		}
	}
}

func TestFsAppendRejectsWorkspaceControlPlaneFilename(t *testing.T) {
	ws := t.TempDir()
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent-123", "fs.append", ws, map[string]any{
		"path":    filepath.Join("notes", "SOUL.md"),
		"content": "new content",
	})
	if err == nil {
		t.Fatalf("expected control-plane filename guard error")
	}
	msg := err.Error()
	for _, needle := range []string{
		"does not control agent behavior",
		".openclawssy/agents/<agent_id>/SOUL.md",
		"dashboard Agent Files",
	} {
		if !strings.Contains(msg, needle) {
			t.Fatalf("expected error to contain %q, got %q", needle, msg)
		}
	}
}

func TestFsEditRejectsWorkspaceControlPlaneFilename(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "DEVPLAN.md"), []byte("draft"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent-123", "fs.edit", ws, map[string]any{
		"path": "DEVPLAN.md",
		"old":  "draft",
		"new":  "updated",
	})
	if err == nil {
		t.Fatalf("expected control-plane filename guard error")
	}
	msg := err.Error()
	for _, needle := range []string{
		"does not control agent behavior",
		".openclawssy/agents/<agent_id>/DEVPLAN.md",
		"dashboard Agent Files",
	} {
		if !strings.Contains(msg, needle) {
			t.Fatalf("expected error to contain %q, got %q", needle, msg)
		}
	}
}

func TestFsDeleteDeletesRegularFile(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "notes.txt"), []byte("draft"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.delete"}}), nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	res, err := reg.Execute(context.Background(), "agent", "fs.delete", ws, map[string]any{"path": "notes.txt"})
	if err != nil {
		t.Fatalf("fs.delete: %v", err)
	}
	if deleted, _ := res["deleted"].(bool); !deleted {
		t.Fatalf("expected deleted=true, got %#v", res)
	}
	if _, err := os.Stat(filepath.Join(ws, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected notes.txt removed, stat err=%v", err)
	}
}

func TestFsDeleteRejectsTraversalPath(t *testing.T) {
	ws := t.TempDir()
	reg := NewRegistry(policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.delete"}}), nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "fs.delete", ws, map[string]any{"path": "../outside.txt"})
	if err == nil {
		t.Fatalf("expected traversal rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "path traversal") {
		t.Fatalf("expected traversal error, got %q", err.Error())
	}
}

func TestFsDeleteRejectsProtectedControlPlanePath(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".openclawssy"), 0o755); err != nil {
		t.Fatalf("mkdir control-plane: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".openclawssy", "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write control-plane fixture: %v", err)
	}

	reg := NewRegistry(policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.delete"}}), nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "fs.delete", ws, map[string]any{"path": ".openclawssy/config.json"})
	if err == nil {
		t.Fatalf("expected protected control-plane path rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "protected control-plane") {
		t.Fatalf("expected protected control-plane message, got %q", err.Error())
	}
}

func TestFsDeleteRejectsWorkspaceControlPlaneFilename(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "RULES.md"), []byte("rules"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent-123", "fs.delete", ws, map[string]any{"path": "RULES.md"})
	if err == nil {
		t.Fatalf("expected control-plane filename guard error")
	}
	if !strings.Contains(err.Error(), "does not control agent behavior") {
		t.Fatalf("expected guard message, got %q", err.Error())
	}
}

func TestFsDeleteDirectoryRequiresRecursiveFlag(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "tmpdir"), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	reg := NewRegistry(policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.delete"}}), nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "fs.delete", ws, map[string]any{"path": "tmpdir"})
	if err == nil {
		t.Fatalf("expected directory recursive hint error")
	}
	if !strings.Contains(err.Error(), "recursive=true") {
		t.Fatalf("expected recursive hint in error, got %q", err.Error())
	}
}

func TestFsDeleteDirectoryRecursiveSucceeds(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "tmpdir", "child"), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "tmpdir", "child", "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.delete"}}), nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	res, err := reg.Execute(context.Background(), "agent", "fs.delete", ws, map[string]any{"path": "tmpdir", "recursive": true})
	if err != nil {
		t.Fatalf("recursive fs.delete: %v", err)
	}
	if deleted, _ := res["deleted"].(bool); !deleted {
		t.Fatalf("expected deleted=true, got %#v", res)
	}
	if _, err := os.Stat(filepath.Join(ws, "tmpdir")); !os.IsNotExist(err) {
		t.Fatalf("expected tmpdir removed, stat err=%v", err)
	}
}

func TestFsDeleteNonexistentForceModes(t *testing.T) {
	ws := t.TempDir()
	reg := NewRegistry(policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.delete"}}), nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "fs.delete", ws, map[string]any{"path": "missing.txt"}); err == nil {
		t.Fatalf("expected missing path error when force=false")
	}
	res, err := reg.Execute(context.Background(), "agent", "fs.delete", ws, map[string]any{"path": "missing.txt", "force": true})
	if err != nil {
		t.Fatalf("expected force delete no-op success, got %v", err)
	}
	if deleted, _ := res["deleted"].(bool); deleted {
		t.Fatalf("expected deleted=false for force no-op, got %#v", res)
	}
}

func TestFsMoveRenamesFile(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.move"}}), nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	res, err := reg.Execute(context.Background(), "agent", "fs.move", ws, map[string]any{"src": "a.txt", "dst": "b.txt"})
	if err != nil {
		t.Fatalf("fs.move: %v", err)
	}
	if moved, _ := res["moved"].(bool); !moved {
		t.Fatalf("expected moved=true, got %#v", res)
	}
	if _, err := os.Stat(filepath.Join(ws, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected source removed, stat err=%v", err)
	}
	b, err := os.ReadFile(filepath.Join(ws, "b.txt"))
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("unexpected destination content: %q", string(b))
	}
}

func TestFsMoveRejectsTraversalPath(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	reg := NewRegistry(policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.move"}}), nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "fs.move", ws, map[string]any{"src": "a.txt", "dst": "../b.txt"})
	if err == nil {
		t.Fatalf("expected traversal rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "path traversal") {
		t.Fatalf("expected path traversal error, got %q", err.Error())
	}
}

func TestFsMoveOverwriteModes(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("A"), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "b.txt"), []byte("B"), 0o600); err != nil {
		t.Fatalf("write destination fixture: %v", err)
	}
	reg := NewRegistry(policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.move"}}), nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "fs.move", ws, map[string]any{"src": "a.txt", "dst": "b.txt"}); err == nil {
		t.Fatalf("expected destination exists error with overwrite=false")
	}

	if _, err := reg.Execute(context.Background(), "agent", "fs.move", ws, map[string]any{"src": "a.txt", "dst": "b.txt", "overwrite": true}); err != nil {
		t.Fatalf("expected overwrite move success, got %v", err)
	}
	b, err := os.ReadFile(filepath.Join(ws, "b.txt"))
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(b) != "A" {
		t.Fatalf("expected destination content from source, got %q", string(b))
	}
}

func TestFsMoveMissingSourceAndControlPlaneGuard(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "DEVPLAN.md"), []byte("draft"), 0o600); err != nil {
		t.Fatalf("write control-plane fixture: %v", err)
	}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "fs.move", ws, map[string]any{"src": "missing.txt", "dst": "new.txt"}); err == nil {
		t.Fatalf("expected missing source error")
	}

	_, err := reg.Execute(context.Background(), "agent-123", "fs.move", ws, map[string]any{"src": "DEVPLAN.md", "dst": "renamed.md"})
	if err == nil {
		t.Fatalf("expected control-plane filename guard error")
	}
	if !strings.Contains(err.Error(), "does not control agent behavior") {
		t.Fatalf("expected control-plane guard message, got %q", err.Error())
	}
}

func TestConfigGetRedactsSensitiveFields(t *testing.T) {
	ws, cfgPath, reg := setupConfigToolRegistry(t)

	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load cfg: %v", err)
	}
	cfg.Providers.OpenAI.APIKey = "secret-openai"
	cfg.Discord.Token = "discord-secret"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	res, err := reg.Execute(context.Background(), "agent", "config.get", ws, map[string]any{})
	if err != nil {
		t.Fatalf("config.get: %v", err)
	}
	out, ok := res["config"].(config.Config)
	if !ok {
		t.Fatalf("expected config struct output, got %#v", res["config"])
	}
	if out.Providers.OpenAI.APIKey != "" {
		t.Fatalf("expected openai key redacted, got %q", out.Providers.OpenAI.APIKey)
	}
	if out.Discord.Token != "" {
		t.Fatalf("expected discord token redacted, got %q", out.Discord.Token)
	}
	if out.Secrets.StoreFile != "" || out.Secrets.MasterKeyFile != "" {
		t.Fatalf("expected secrets path fields redacted in config.get output, got %+v", out.Secrets)
	}
}

func TestConfigGetSingleField(t *testing.T) {
	ws, _, reg := setupConfigToolRegistry(t)
	res, err := reg.Execute(context.Background(), "agent", "config.get", ws, map[string]any{"field": "output.thinking_mode"})
	if err != nil {
		t.Fatalf("config.get field: %v", err)
	}
	if res["value"] != config.ThinkingModeNever {
		t.Fatalf("unexpected field value: %#v", res["value"])
	}
}

func TestConfigSetAppliesAndPersistsSafeUpdates(t *testing.T) {
	ws, cfgPath, reg := setupConfigToolRegistry(t)

	res, err := reg.Execute(context.Background(), "agent", "config.set", ws, map[string]any{
		"updates": map[string]any{
			"output.thinking_mode":       "on_error",
			"engine.max_concurrent_runs": 32,
		},
	})
	if err != nil {
		t.Fatalf("config.set: %v", err)
	}
	updatedFields, ok := res["updated_fields"].([]string)
	if !ok || len(updatedFields) != 2 {
		t.Fatalf("expected updated_fields list, got %#v", res["updated_fields"])
	}

	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load cfg after set: %v", err)
	}
	if cfg.Output.ThinkingMode != config.ThinkingModeOnError {
		t.Fatalf("expected thinking mode updated, got %q", cfg.Output.ThinkingMode)
	}
	if cfg.Engine.MaxConcurrentRuns != 32 {
		t.Fatalf("expected engine.max_concurrent_runs=32, got %d", cfg.Engine.MaxConcurrentRuns)
	}
}

func TestConfigSetDryRunDoesNotPersist(t *testing.T) {
	ws, cfgPath, reg := setupConfigToolRegistry(t)

	res, err := reg.Execute(context.Background(), "agent", "config.set", ws, map[string]any{
		"updates": map[string]any{
			"network.enabled": true,
		},
		"dry_run": true,
	})
	if err != nil {
		t.Fatalf("config.set dry_run: %v", err)
	}
	if dryRun, _ := res["dry_run"].(bool); !dryRun {
		t.Fatalf("expected dry_run=true response, got %#v", res["dry_run"])
	}

	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load cfg after dry run: %v", err)
	}
	if cfg.Network.Enabled {
		t.Fatal("expected network.enabled unchanged after dry_run")
	}
}

func TestConfigSetRejectsInvalidUpdates(t *testing.T) {
	ws, _, reg := setupConfigToolRegistry(t)

	if _, err := reg.Execute(context.Background(), "agent", "config.set", ws, map[string]any{
		"updates": map[string]any{
			"model.name": "unsafe",
		},
	}); err == nil {
		t.Fatal("expected disallowed field rejection")
	}

	if _, err := reg.Execute(context.Background(), "agent", "config.set", ws, map[string]any{
		"updates": map[string]any{
			"output.max_thinking_chars": 1,
		},
	}); err == nil {
		t.Fatal("expected out-of-range rejection")
	}

	if _, err := reg.Execute(context.Background(), "agent", "config.set", ws, map[string]any{
		"updates": map[string]any{
			"shell.enable_exec": true,
		},
	}); err == nil {
		t.Fatal("expected shell.enable_exec sandbox guard rejection")
	}
}

func setupConfigToolRegistry(t *testing.T) (string, string, *Registry) {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Workspace.Root = ws
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config fixture: %v", err)
	}

	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}
	return ws, cfgPath, reg
}

func TestSecretsToolsRoundTripAndList(t *testing.T) {
	ws, cfgPath := setupSecretsConfigFixture(t)
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "secrets.set", ws, map[string]any{"key": "provider/openrouter/api_key", "value": "secret-value"}); err != nil {
		t.Fatalf("secrets.set: %v", err)
	}
	getRes, err := reg.Execute(context.Background(), "agent", "secrets.get", ws, map[string]any{"key": "provider/openrouter/api_key"})
	if err != nil {
		t.Fatalf("secrets.get: %v", err)
	}
	if found, _ := getRes["found"].(bool); !found {
		t.Fatalf("expected found=true, got %#v", getRes)
	}
	if getRes["value"] != "secret-value" {
		t.Fatalf("unexpected secret value: %#v", getRes["value"])
	}

	listRes, err := reg.Execute(context.Background(), "agent", "secrets.list", ws, map[string]any{})
	if err != nil {
		t.Fatalf("secrets.list: %v", err)
	}
	keys, ok := listRes["keys"].([]string)
	if !ok || len(keys) != 1 || keys[0] != "provider/openrouter/api_key" {
		t.Fatalf("unexpected listed keys: %#v", listRes["keys"])
	}
}

func TestSecretsGetMissingKeyReturnsFoundFalse(t *testing.T) {
	ws, cfgPath := setupSecretsConfigFixture(t)
	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}
	res, err := reg.Execute(context.Background(), "agent", "secrets.get", ws, map[string]any{"key": "missing/key"})
	if err != nil {
		t.Fatalf("secrets.get missing key: %v", err)
	}
	if found, _ := res["found"].(bool); found {
		t.Fatalf("expected found=false for missing key, got %#v", res)
	}
}

func TestSecretsToolsAreCapabilityGated(t *testing.T) {
	ws, cfgPath := setupSecretsConfigFixture(t)
	enforcer := policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.read"}})
	reg := NewRegistry(enforcer, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "secrets.get", ws, map[string]any{"key": "provider/openrouter/api_key"})
	if err == nil {
		t.Fatal("expected capability denied error for secrets.get")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodePolicyDenied {
		t.Fatalf("expected policy.denied, got %s", toolErr.Code)
	}
}

func TestSecretsAuditNeverStoresPlaintextValues(t *testing.T) {
	ws, cfgPath := setupSecretsConfigFixture(t)
	audit := &memAudit{}
	reg := NewRegistry(fakePolicy{}, audit)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "secrets.set", ws, map[string]any{"key": "provider/openrouter/api_key", "value": "ultra-sensitive-token"}); err != nil {
		t.Fatalf("secrets.set: %v", err)
	}
	if _, err := reg.Execute(context.Background(), "agent", "secrets.get", ws, map[string]any{"key": "provider/openrouter/api_key"}); err != nil {
		t.Fatalf("secrets.get: %v", err)
	}

	for _, rec := range audit.recs {
		if rec.eventType != "tool.call" && rec.eventType != "tool.result" {
			continue
		}
		if strings.Contains(strings.ToLower(rec.eventType), "tool") {
			if strings.Contains(strings.ToLower(fmt.Sprintf("%v", rec.fields)), "ultra-sensitive-token") {
				t.Fatalf("secret plaintext leaked in audit record: %#v", rec)
			}
		}
	}
}

func setupSecretsConfigFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	masterPath := filepath.Join(root, ".openclawssy", "master.key")
	if _, err := secrets.GenerateAndWriteMasterKey(masterPath); err != nil {
		t.Fatalf("generate master key: %v", err)
	}

	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Workspace.Root = ws
	cfg.Secrets.StoreFile = filepath.Join(root, ".openclawssy", "secrets.enc")
	cfg.Secrets.MasterKeyFile = masterPath
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config fixture: %v", err)
	}
	return ws, cfgPath
}

func TestSkillToolsDiscoverAndReadWorkspaceSkillWithSecrets(t *testing.T) {
	ws, cfgPath := setupSecretsConfigFixture(t)
	skillsDir := filepath.Join(ws, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	skillBody := "# Perplexity Skill\n\nUse `PERPLEXITY_API_KEY` with http.request to query Perplexity."
	if err := os.WriteFile(filepath.Join(skillsDir, "perplexity.md"), []byte(skillBody), 0o600); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}
	if _, err := reg.Execute(context.Background(), "agent", "secrets.set", ws, map[string]any{"key": "PERPLEXITY_API_KEY", "value": "secret-value"}); err != nil {
		t.Fatalf("secrets.set: %v", err)
	}

	listRes, err := reg.Execute(context.Background(), "agent", "skill.list", ws, map[string]any{})
	if err != nil {
		t.Fatalf("skill.list: %v", err)
	}
	skills, ok := listRes["skills"].([]map[string]any)
	if !ok {
		raw, okAny := listRes["skills"].([]any)
		if !okAny {
			t.Fatalf("expected skills array, got %#v", listRes["skills"])
		}
		skills = make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			obj, ok := item.(map[string]any)
			if ok {
				skills = append(skills, obj)
			}
		}
	}
	if len(skills) != 1 {
		t.Fatalf("expected one discovered skill, got %#v", listRes["skills"])
	}
	if skills[0]["name"] != "perplexity" {
		t.Fatalf("expected perplexity skill, got %#v", skills[0]["name"])
	}
	if missing, ok := skills[0]["missing_secrets"].([]any); ok && len(missing) != 0 {
		t.Fatalf("expected no missing secrets, got %#v", skills[0]["missing_secrets"])
	}

	readRes, err := reg.Execute(context.Background(), "agent", "skill.read", ws, map[string]any{"name": "perplexity"})
	if err != nil {
		t.Fatalf("skill.read: %v", err)
	}
	if content, _ := readRes["content"].(string); !strings.Contains(content, "PERPLEXITY_API_KEY") {
		t.Fatalf("expected skill content to include required key, got %#v", readRes["content"])
	}
	if ready, _ := readRes["ready"].(bool); !ready {
		t.Fatalf("expected ready=true, got %#v", readRes["ready"])
	}
}

func TestSkillReadReturnsActionableErrorWhenSecretMissing(t *testing.T) {
	ws, cfgPath := setupSecretsConfigFixture(t)
	skillsDir := filepath.Join(ws, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "perplexity.md"), []byte("requires PERPLEXITY_API_KEY"), 0o600); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "skill.read", ws, map[string]any{"name": "perplexity"})
	if err == nil {
		t.Fatal("expected missing secret error")
	}
	if !strings.Contains(err.Error(), "missing required secrets for skill perplexity") || !strings.Contains(err.Error(), "provider/perplexity/api_key") {
		t.Fatalf("expected explicit missing secret guidance, got %q", err.Error())
	}
}

func TestSkillReadAcceptsProviderSecretAlias(t *testing.T) {
	ws, cfgPath := setupSecretsConfigFixture(t)
	skillsDir := filepath.Join(ws, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "perplexity.md"), []byte("requires PERPLEXITY_API_KEY"), 0o600); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}
	if _, err := reg.Execute(context.Background(), "agent", "secrets.set", ws, map[string]any{"key": "provider/perplexity/api_key", "value": "secret-value"}); err != nil {
		t.Fatalf("secrets.set: %v", err)
	}

	readRes, err := reg.Execute(context.Background(), "agent", "skill.read", ws, map[string]any{"name": "perplexity"})
	if err != nil {
		t.Fatalf("skill.read: %v", err)
	}
	required, _ := readRes["required_secrets"].([]string)
	if len(required) != 1 || required[0] != "provider/perplexity/api_key" {
		t.Fatalf("expected canonical required secret key, got %#v", readRes["required_secrets"])
	}
}

func TestHTTPRequestToolAllowsLocalhostAndTruncatesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer server.Close()

	ws, _, reg := setupNetworkToolRegistry(t, func(cfg *config.Config) {
		cfg.Network.Enabled = true
		cfg.Network.AllowLocalhosts = true
	})

	res, err := reg.Execute(context.Background(), "agent", "http.request", ws, map[string]any{
		"method":             "POST",
		"url":                server.URL,
		"body":               "ping",
		"max_response_bytes": 5,
	})
	if err != nil {
		t.Fatalf("http.request: %v", err)
	}
	if status, _ := res["status"].(int); status != http.StatusOK {
		t.Fatalf("expected status 200, got %#v", res["status"])
	}
	if body, _ := res["body"].(string); body != "hello" {
		t.Fatalf("expected truncated body 'hello', got %#v", res["body"])
	}
	if truncated, _ := res["truncated"].(bool); !truncated {
		t.Fatalf("expected truncated=true, got %#v", res["truncated"])
	}
}

func TestHTTPRequestToolRejectsWhenNetworkDisabled(t *testing.T) {
	ws, _, reg := setupNetworkToolRegistry(t, func(cfg *config.Config) {
		cfg.Network.Enabled = false
	})

	_, err := reg.Execute(context.Background(), "agent", "http.request", ws, map[string]any{"url": "https://example.com"})
	if err == nil {
		t.Fatal("expected network disabled error")
	}
	if !strings.Contains(err.Error(), "network is disabled") {
		t.Fatalf("expected network disabled error, got %q", err.Error())
	}
}

func TestHTTPRequestToolRejectsNonAllowlistedHost(t *testing.T) {
	ws, _, reg := setupNetworkToolRegistry(t, func(cfg *config.Config) {
		cfg.Network.Enabled = true
		cfg.Network.AllowLocalhosts = false
		cfg.Network.AllowedDomains = []string{"api.allowed.test"}
	})

	_, err := reg.Execute(context.Background(), "agent", "http.request", ws, map[string]any{"url": "https://example.com"})
	if err == nil {
		t.Fatal("expected non-allowlisted host denial")
	}
	if !strings.Contains(err.Error(), "network.allowed_domains") {
		t.Fatalf("expected allowlist error, got %q", err.Error())
	}
}

func TestHTTPRequestToolRedirectRechecksAllowlist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com", http.StatusFound)
	}))
	defer server.Close()

	ws, _, reg := setupNetworkToolRegistry(t, func(cfg *config.Config) {
		cfg.Network.Enabled = true
		cfg.Network.AllowLocalhosts = true
	})

	_, err := reg.Execute(context.Background(), "agent", "http.request", ws, map[string]any{"url": server.URL})
	if err == nil {
		t.Fatal("expected redirect host denial")
	}
	if !strings.Contains(err.Error(), "network.allowed_domains") {
		t.Fatalf("expected redirect allowlist error, got %q", err.Error())
	}
}

func setupNetworkToolRegistry(t *testing.T, mutate func(*config.Config)) (string, string, *Registry) {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Workspace.Root = ws
	if mutate != nil {
		mutate(&cfg)
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config fixture: %v", err)
	}

	reg := NewRegistry(fakePolicy{}, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}
	return ws, cfgPath, reg
}

func TestSessionToolsListAndCloseLifecycle(t *testing.T) {
	ws, agentsRoot, reg := setupSessionToolRegistry(t, fakePolicy{})
	store, err := chatstore.NewStore(agentsRoot)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	openSession, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("create open session: %v", err)
	}
	closedSession, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "u1", RoomID: "r1"})
	if err != nil {
		t.Fatalf("create closed session fixture: %v", err)
	}
	if err := store.CloseSession(closedSession.SessionID); err != nil {
		t.Fatalf("close fixture session: %v", err)
	}

	listRes, err := reg.Execute(context.Background(), "agent", "session.list", ws, map[string]any{"agent_id": "default", "user_id": "u1", "room_id": "r1", "channel": "dashboard"})
	if err != nil {
		t.Fatalf("session.list: %v", err)
	}
	sessions, ok := listRes["sessions"].([]chatstore.Session)
	if !ok {
		t.Fatalf("expected []chatstore.Session result, got %#v", listRes["sessions"])
	}
	if len(sessions) != 1 || sessions[0].SessionID != openSession.SessionID {
		t.Fatalf("expected only open session by default, got %#v", sessions)
	}

	listRes, err = reg.Execute(context.Background(), "agent", "session.list", ws, map[string]any{"agent_id": "default", "user_id": "u1", "room_id": "r1", "channel": "dashboard", "include_closed": true})
	if err != nil {
		t.Fatalf("session.list include closed: %v", err)
	}
	sessions = listRes["sessions"].([]chatstore.Session)
	if len(sessions) != 2 {
		t.Fatalf("expected two sessions with include_closed=true, got %#v", sessions)
	}

	closeRes, err := reg.Execute(context.Background(), "agent", "session.close", ws, map[string]any{"session_id": openSession.SessionID})
	if err != nil {
		t.Fatalf("session.close: %v", err)
	}
	if closed, _ := closeRes["closed"].(bool); !closed {
		t.Fatalf("expected closed=true, got %#v", closeRes)
	}

	updated, err := store.GetSession(openSession.SessionID)
	if err != nil {
		t.Fatalf("get closed session: %v", err)
	}
	if !updated.IsClosed() {
		t.Fatal("expected session to be closed")
	}

	closeRes, err = reg.Execute(context.Background(), "agent", "session.close", ws, map[string]any{"session_id": openSession.SessionID})
	if err != nil {
		t.Fatalf("session.close idempotent: %v", err)
	}
	if already, _ := closeRes["already_closed"].(bool); !already {
		t.Fatalf("expected already_closed=true on repeated close, got %#v", closeRes)
	}
}

func TestSessionToolsAreCapabilityGated(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Workspace.Root = ws
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config fixture: %v", err)
	}
	agentsPath := filepath.Join(root, ".openclawssy", "agents")

	enforcer := policy.NewEnforcer(ws, map[string][]string{"agent": {"fs.read"}})
	reg := NewRegistry(enforcer, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath, ChatstorePath: agentsPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "session.list", ws, map[string]any{"agent_id": "default"})
	if err == nil {
		t.Fatal("expected capability denied for session.list")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodePolicyDenied {
		t.Fatalf("expected policy.denied, got %s", toolErr.Code)
	}
}

func setupSessionToolRegistry(t *testing.T, pol Policy) (string, string, *Registry) {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Workspace.Root = ws
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config fixture: %v", err)
	}
	agentsPath := filepath.Join(root, ".openclawssy", "agents")

	reg := NewRegistry(pol, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath, ChatstorePath: agentsPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}
	return ws, agentsPath, reg
}

func TestAgentToolsListCreateSwitchLifecycle(t *testing.T) {
	ws, root, agentsPath, cfgPath, reg := setupAgentToolRegistry(t, fakePolicy{})

	listRes, err := reg.Execute(context.Background(), "agent", "agent.list", ws, map[string]any{})
	if err != nil {
		t.Fatalf("agent.list initial: %v", err)
	}
	if total, _ := listRes["total"].(int); total != 0 {
		t.Fatalf("expected no agents initially, got %#v", listRes)
	}

	if _, err := reg.Execute(context.Background(), "agent", "agent.create", ws, map[string]any{"agent_id": "beta"}); err != nil {
		t.Fatalf("agent.create beta: %v", err)
	}
	if _, err := reg.Execute(context.Background(), "agent", "agent.create", ws, map[string]any{"agent_id": "alpha"}); err != nil {
		t.Fatalf("agent.create alpha: %v", err)
	}

	listRes, err = reg.Execute(context.Background(), "agent", "agent.list", ws, map[string]any{"limit": 1, "offset": 1})
	if err != nil {
		t.Fatalf("agent.list paginated: %v", err)
	}
	if total, _ := listRes["total"].(int); total != 2 {
		t.Fatalf("expected total=2, got %#v", listRes)
	}
	items, ok := listRes["items"].([]string)
	if !ok || len(items) != 1 || items[0] != "beta" {
		t.Fatalf("expected sorted/paginated items [beta], got %#v", listRes["items"])
	}

	for _, p := range []string{
		filepath.Join(agentsPath, "alpha", "memory"),
		filepath.Join(agentsPath, "alpha", "audit"),
		filepath.Join(agentsPath, "alpha", "runs"),
		filepath.Join(agentsPath, "alpha", "SOUL.md"),
		filepath.Join(agentsPath, "alpha", "RULES.md"),
		filepath.Join(agentsPath, "alpha", "TOOLS.md"),
		filepath.Join(agentsPath, "alpha", "SPECPLAN.md"),
		filepath.Join(agentsPath, "alpha", "DEVPLAN.md"),
		filepath.Join(agentsPath, "alpha", "HANDOFF.md"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected scaffold path %s: %v", p, err)
		}
	}

	if _, err := reg.Execute(context.Background(), "agent", "agent.switch", ws, map[string]any{"agent_id": "beta", "scope": "chat"}); err != nil {
		t.Fatalf("agent.switch chat: %v", err)
	}
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Chat.DefaultAgentID != "beta" {
		t.Fatalf("expected chat default beta, got %q", cfg.Chat.DefaultAgentID)
	}
	if cfg.Discord.DefaultAgentID != "default" {
		t.Fatalf("expected discord default unchanged, got %q", cfg.Discord.DefaultAgentID)
	}

	if _, err := reg.Execute(context.Background(), "agent", "agent.switch", ws, map[string]any{"agent_id": "alpha"}); err != nil {
		t.Fatalf("agent.switch both: %v", err)
	}
	cfg, err = config.LoadOrDefault(filepath.Join(root, ".openclawssy", "config.json"))
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.Chat.DefaultAgentID != "alpha" || cfg.Discord.DefaultAgentID != "alpha" {
		t.Fatalf("expected chat+discord defaults switched to alpha, got chat=%q discord=%q", cfg.Chat.DefaultAgentID, cfg.Discord.DefaultAgentID)
	}
}

func TestAgentCreateForceOverwriteBehavior(t *testing.T) {
	ws, _, agentsPath, _, reg := setupAgentToolRegistry(t, fakePolicy{})
	if _, err := reg.Execute(context.Background(), "agent", "agent.create", ws, map[string]any{"agent_id": "default"}); err != nil {
		t.Fatalf("agent.create: %v", err)
	}
	toolsPath := filepath.Join(agentsPath, "default", "TOOLS.md")
	if err := os.WriteFile(toolsPath, []byte("custom-tools"), 0o600); err != nil {
		t.Fatalf("write custom tools fixture: %v", err)
	}

	if _, err := reg.Execute(context.Background(), "agent", "agent.create", ws, map[string]any{"agent_id": "default"}); err != nil {
		t.Fatalf("agent.create no-force: %v", err)
	}
	raw, err := os.ReadFile(toolsPath)
	if err != nil {
		t.Fatalf("read tools fixture: %v", err)
	}
	if string(raw) != "custom-tools" {
		t.Fatalf("expected no-force to keep existing seed file, got %q", string(raw))
	}

	if _, err := reg.Execute(context.Background(), "agent", "agent.create", ws, map[string]any{"agent_id": "default", "force": true}); err != nil {
		t.Fatalf("agent.create force: %v", err)
	}
	raw, err = os.ReadFile(toolsPath)
	if err != nil {
		t.Fatalf("read tools after force: %v", err)
	}
	if !strings.Contains(string(raw), "Enabled core tools") {
		t.Fatalf("expected force=true to rewrite seed file, got %q", string(raw))
	}
}

func TestAgentToolsRejectInvalidAgentID(t *testing.T) {
	ws, _, _, _, reg := setupAgentToolRegistry(t, fakePolicy{})

	if _, err := reg.Execute(context.Background(), "agent", "agent.create", ws, map[string]any{"agent_id": "../evil"}); err == nil {
		t.Fatal("expected invalid agent_id rejection for create")
	}
	if _, err := reg.Execute(context.Background(), "agent", "agent.switch", ws, map[string]any{"agent_id": "a/b"}); err == nil {
		t.Fatal("expected invalid agent_id rejection for switch")
	}
}

func TestAgentSwitchCreateIfMissing(t *testing.T) {
	ws, _, agentsPath, _, reg := setupAgentToolRegistry(t, fakePolicy{})

	if _, err := reg.Execute(context.Background(), "agent", "agent.switch", ws, map[string]any{"agent_id": "new-agent", "scope": "both"}); err == nil {
		t.Fatal("expected missing agent error when create_if_missing=false")
	}

	if _, err := reg.Execute(context.Background(), "agent", "agent.switch", ws, map[string]any{"agent_id": "new-agent", "scope": "both", "create_if_missing": true}); err != nil {
		t.Fatalf("agent.switch create_if_missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsPath, "new-agent", "SOUL.md")); err != nil {
		t.Fatalf("expected switched missing agent to be scaffolded: %v", err)
	}
}

func TestAgentToolsAreCapabilityGated(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Workspace.Root = ws
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config fixture: %v", err)
	}
	agentsPath := filepath.Join(root, ".openclawssy", "agents")

	enforcer := policy.NewEnforcer(ws, map[string][]string{"agent": {"fs.read"}})
	reg := NewRegistry(enforcer, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath, AgentsPath: agentsPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "agent.list", ws, map[string]any{})
	if err == nil {
		t.Fatal("expected capability denied for agent.list")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodePolicyDenied {
		t.Fatalf("expected policy.denied, got %s", toolErr.Code)
	}
}

func setupAgentToolRegistry(t *testing.T, pol Policy) (string, string, string, string, *Registry) {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Workspace.Root = ws
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config fixture: %v", err)
	}
	agentsPath := filepath.Join(root, ".openclawssy", "agents")

	reg := NewRegistry(pol, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath, AgentsPath: agentsPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}
	return ws, root, agentsPath, cfgPath, reg
}

func TestPolicyToolsGrantRevokeLifecycle(t *testing.T) {
	ws, policyPath, reg := setupPolicyToolRegistry(t, policy.NewEnforcer("", map[string][]string{
		"admin": {"policy.list", "policy.grant", "policy.revoke", "policy.admin"},
	}))

	listRes, err := reg.Execute(context.Background(), "admin", "policy.list", ws, map[string]any{"agent_id": "worker"})
	if err != nil {
		t.Fatalf("policy.list default: %v", err)
	}
	if src, _ := listRes["source"].(string); src != "default" {
		t.Fatalf("expected default source before persistence, got %#v", listRes)
	}

	if _, err := reg.Execute(context.Background(), "admin", "policy.grant", ws, map[string]any{"agent_id": "worker", "capability": "fs.delete"}); err != nil {
		t.Fatalf("policy.grant: %v", err)
	}

	listRes, err = reg.Execute(context.Background(), "admin", "policy.list", ws, map[string]any{"agent_id": "worker"})
	if err != nil {
		t.Fatalf("policy.list persisted: %v", err)
	}
	if src, _ := listRes["source"].(string); src != "persisted" {
		t.Fatalf("expected persisted source after grant, got %#v", listRes)
	}
	caps, ok := listRes["capabilities"].([]string)
	if !ok {
		t.Fatalf("expected []string capabilities, got %#v", listRes["capabilities"])
	}
	if !containsString(caps, "fs.delete") {
		t.Fatalf("expected fs.delete in capabilities after grant, got %#v", caps)
	}

	if _, err := reg.Execute(context.Background(), "admin", "policy.revoke", ws, map[string]any{"agent_id": "worker", "capability": "fs.write"}); err != nil {
		t.Fatalf("policy.revoke: %v", err)
	}
	listRes, err = reg.Execute(context.Background(), "admin", "policy.list", ws, map[string]any{"agent_id": "worker"})
	if err != nil {
		t.Fatalf("policy.list after revoke: %v", err)
	}
	caps = listRes["capabilities"].([]string)
	if containsString(caps, "fs.write") {
		t.Fatalf("expected fs.write revoked, got %#v", caps)
	}

	stored, err := policy.LoadGrants(policyPath)
	if err != nil {
		t.Fatalf("load policy grants: %v", err)
	}
	if len(stored["worker"]) == 0 {
		t.Fatalf("expected persisted grants for worker in %s", policyPath)
	}
}

func TestPolicyToolsRequirePolicyAdmin(t *testing.T) {
	ws, _, reg := setupPolicyToolRegistry(t, policy.NewEnforcer("", map[string][]string{
		"agent": {"policy.list", "policy.grant", "policy.revoke"},
	}))

	_, err := reg.Execute(context.Background(), "agent", "policy.list", ws, map[string]any{"agent_id": "worker"})
	if err == nil {
		t.Fatal("expected policy.admin denial for policy.list")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodePolicyDenied {
		t.Fatalf("expected policy.denied, got %s", toolErr.Code)
	}
}

func setupPolicyToolRegistry(t *testing.T, pol Policy) (string, string, *Registry) {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	policyPath := filepath.Join(root, ".openclawssy", "policy", "capabilities.json")

	reg := NewRegistry(pol, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, PolicyPath: policyPath, DefaultGrants: []string{"fs.read", "fs.write", "run.list", "run.get", "run.cancel", "metrics.get"}}); err != nil {
		t.Fatalf("register core: %v", err)
	}
	return ws, policyPath, reg
}

func TestMetricsGetAggregatesToolDurationsAndErrors(t *testing.T) {
	ws, runsPath, reg := setupRunToolRegistry(t, fakePolicy{})
	store, err := httpchannel.NewFileRunStore(runsPath)
	if err != nil {
		t.Fatalf("new run store: %v", err)
	}

	now := time.Now().UTC()
	_, err = store.Create(context.Background(), httpchannel.Run{
		ID:        "run_1",
		AgentID:   "default",
		Status:    "completed",
		ToolCalls: 2,
		CreatedAt: now.Add(-2 * time.Minute),
		UpdatedAt: now.Add(-2 * time.Minute),
		Trace: map[string]any{
			"tool_execution_results": []any{
				map[string]any{"tool": "fs.read", "duration_ms": 10, "error": ""},
				map[string]any{"tool": "fs.write", "duration_ms": 20, "error": "boom"},
			},
		},
	})
	if err != nil {
		t.Fatalf("create run_1: %v", err)
	}
	_, err = store.Create(context.Background(), httpchannel.Run{
		ID:        "run_2",
		AgentID:   "default",
		Status:    "failed",
		ToolCalls: 1,
		CreatedAt: now.Add(-1 * time.Minute),
		UpdatedAt: now.Add(-1 * time.Minute),
		Trace: map[string]any{
			"tool_execution_results": []any{
				map[string]any{"tool": "fs.read", "duration_ms": 30, "error": ""},
			},
		},
	})
	if err != nil {
		t.Fatalf("create run_2: %v", err)
	}

	res, err := reg.Execute(context.Background(), "agent", "metrics.get", ws, map[string]any{"agent_id": "default"})
	if err != nil {
		t.Fatalf("metrics.get: %v", err)
	}
	if totalCalls, _ := res["tool_calls_total"].(int); totalCalls != 3 {
		t.Fatalf("expected tool_calls_total=3, got %#v", res["tool_calls_total"])
	}
	runs, ok := res["runs"].(map[string]any)
	if !ok {
		t.Fatalf("expected runs map, got %#v", res["runs"])
	}
	statusCounts, ok := runs["status_counts"].(map[string]int)
	if !ok {
		t.Fatalf("expected status_counts map, got %#v", runs["status_counts"])
	}
	if statusCounts["completed"] != 1 || statusCounts["failed"] != 1 {
		t.Fatalf("unexpected status counts: %#v", statusCounts)
	}
	tools, ok := res["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("expected tools slice, got %#v", res["tools"])
	}
	readStats := map[string]any{}
	writeStats := map[string]any{}
	for _, item := range tools {
		if item["tool"] == "fs.read" {
			readStats = item
		}
		if item["tool"] == "fs.write" {
			writeStats = item
		}
	}
	if readStats["calls"] != 2 || readStats["errors"] != 0 || readStats["avg_duration_ms"] != int64(20) {
		t.Fatalf("unexpected fs.read metrics: %#v", readStats)
	}
	if writeStats["calls"] != 1 || writeStats["errors"] != 1 {
		t.Fatalf("unexpected fs.write metrics: %#v", writeStats)
	}
}

func TestMetricsGetIsCapabilityGated(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	runsPath := filepath.Join(root, ".openclawssy", "runs.json")

	enforcer := policy.NewEnforcer(ws, map[string][]string{"agent": {"fs.read"}})
	reg := NewRegistry(enforcer, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, RunsPath: runsPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "metrics.get", ws, map[string]any{})
	if err == nil {
		t.Fatal("expected capability denied for metrics.get")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodePolicyDenied {
		t.Fatalf("expected policy.denied, got %s", toolErr.Code)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestSchedulerToolsLifecycle(t *testing.T) {
	ws, _, reg := setupSchedulerToolRegistry(t, fakePolicy{})

	if _, err := reg.Execute(context.Background(), "agent", "scheduler.add", ws, map[string]any{
		"id":         "job-1",
		"schedule":   "@every 1m",
		"message":    "ping",
		"agent_id":   "default",
		"channel":    "dashboard",
		"user_id":    "u1",
		"room_id":    "room-a",
		"session_id": "chat-1",
	}); err != nil {
		t.Fatalf("scheduler.add: %v", err)
	}

	listRes, err := reg.Execute(context.Background(), "agent", "scheduler.list", ws, map[string]any{})
	if err != nil {
		t.Fatalf("scheduler.list: %v", err)
	}
	jobs, ok := listRes["jobs"].([]scheduler.Job)
	if !ok || len(jobs) != 1 {
		t.Fatalf("expected one scheduler job, got %#v", listRes["jobs"])
	}
	if jobs[0].ID != "job-1" {
		t.Fatalf("unexpected job id: %+v", jobs[0])
	}
	if jobs[0].Channel != "dashboard" || jobs[0].UserID != "u1" || jobs[0].RoomID != "room-a" || jobs[0].SessionID != "chat-1" {
		t.Fatalf("expected scheduler destination metadata to persist, got %+v", jobs[0])
	}

	if _, err := reg.Execute(context.Background(), "agent", "scheduler.pause", ws, map[string]any{}); err != nil {
		t.Fatalf("scheduler.pause global: %v", err)
	}
	listRes, err = reg.Execute(context.Background(), "agent", "scheduler.list", ws, map[string]any{})
	if err != nil {
		t.Fatalf("scheduler.list after pause: %v", err)
	}
	if paused, _ := listRes["paused"].(bool); !paused {
		t.Fatalf("expected paused=true after global pause, got %#v", listRes["paused"])
	}

	if _, err := reg.Execute(context.Background(), "agent", "scheduler.resume", ws, map[string]any{}); err != nil {
		t.Fatalf("scheduler.resume global: %v", err)
	}
	if _, err := reg.Execute(context.Background(), "agent", "scheduler.pause", ws, map[string]any{"id": "job-1"}); err != nil {
		t.Fatalf("scheduler.pause job: %v", err)
	}
	listRes, err = reg.Execute(context.Background(), "agent", "scheduler.list", ws, map[string]any{})
	if err != nil {
		t.Fatalf("scheduler.list after job pause: %v", err)
	}
	jobs = listRes["jobs"].([]scheduler.Job)
	if jobs[0].Enabled {
		t.Fatalf("expected job disabled after scheduler.pause id, got %+v", jobs[0])
	}

	if _, err := reg.Execute(context.Background(), "agent", "scheduler.resume", ws, map[string]any{"id": "job-1"}); err != nil {
		t.Fatalf("scheduler.resume job: %v", err)
	}
	if _, err := reg.Execute(context.Background(), "agent", "scheduler.remove", ws, map[string]any{"id": "job-1"}); err != nil {
		t.Fatalf("scheduler.remove: %v", err)
	}
	listRes, err = reg.Execute(context.Background(), "agent", "scheduler.list", ws, map[string]any{})
	if err != nil {
		t.Fatalf("scheduler.list after remove: %v", err)
	}
	jobs = listRes["jobs"].([]scheduler.Job)
	if len(jobs) != 0 {
		t.Fatalf("expected zero jobs after remove, got %#v", jobs)
	}
}

func TestSchedulerAddRejectsInvalidSchedule(t *testing.T) {
	ws, _, reg := setupSchedulerToolRegistry(t, fakePolicy{})
	if _, err := reg.Execute(context.Background(), "agent", "scheduler.add", ws, map[string]any{
		"id":       "job-invalid",
		"schedule": "daily",
		"message":  "ping",
	}); err == nil {
		t.Fatal("expected invalid schedule rejection")
	}
}

func TestSchedulerToolsAreCapabilityGated(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Workspace.Root = ws
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config fixture: %v", err)
	}
	jobsPath := filepath.Join(root, ".openclawssy", "scheduler", "jobs.json")

	enforcer := policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.read"}})
	reg := NewRegistry(enforcer, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath, SchedulerPath: jobsPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "scheduler.list", ws, map[string]any{})
	if err == nil {
		t.Fatal("expected capability denied for scheduler.list")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodePolicyDenied {
		t.Fatalf("expected policy.denied, got %s", toolErr.Code)
	}
}

func setupSchedulerToolRegistry(t *testing.T, pol Policy) (string, string, *Registry) {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	cfgPath := filepath.Join(root, ".openclawssy", "config.json")
	cfg := config.Default()
	cfg.Workspace.Root = ws
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config fixture: %v", err)
	}
	jobsPath := filepath.Join(root, ".openclawssy", "scheduler", "jobs.json")

	reg := NewRegistry(pol, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, ConfigPath: cfgPath, SchedulerPath: jobsPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}
	return ws, jobsPath, reg
}

func TestRunToolsListAndGet(t *testing.T) {
	ws, _, reg := setupRunToolRegistry(t, fakePolicy{})

	// Initially, no runs exist
	listRes, err := reg.Execute(context.Background(), "agent", "run.list", ws, map[string]any{})
	if err != nil {
		t.Fatalf("run.list: %v", err)
	}
	runs, ok := listRes["runs"].([]httpchannel.Run)
	if !ok {
		t.Fatalf("expected []httpchannel.Run result, got %#v", listRes["runs"])
	}
	if len(runs) != 0 {
		t.Fatalf("expected zero runs initially, got %d", len(runs))
	}

	// Get non-existent run
	getRes, err := reg.Execute(context.Background(), "agent", "run.get", ws, map[string]any{"run_id": "nonexistent"})
	if err != nil {
		t.Fatalf("run.get: %v", err)
	}
	found, _ := getRes["found"].(bool)
	if found {
		t.Fatalf("expected found=false for non-existent run")
	}
}

func TestRunToolsAreCapabilityGated(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	runsPath := filepath.Join(root, ".openclawssy", "runs.json")

	enforcer := policy.NewEnforcer(ws, map[string][]string{"agent": []string{"fs.read"}})
	reg := NewRegistry(enforcer, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, RunsPath: runsPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "run.list", ws, map[string]any{})
	if err == nil {
		t.Fatal("expected capability denied for run.list")
	}
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodePolicyDenied {
		t.Fatalf("expected policy.denied, got %s", toolErr.Code)
	}

	_, err = reg.Execute(context.Background(), "agent", "run.get", ws, map[string]any{"run_id": "test"})
	if err == nil {
		t.Fatal("expected capability denied for run.get")
	}
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T", err)
	}
	if toolErr.Code != ErrCodePolicyDenied {
		t.Fatalf("expected policy.denied, got %s", toolErr.Code)
	}
}

func setupRunToolRegistry(t *testing.T, pol Policy) (string, string, *Registry) {
	t.Helper()
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	runsPath := filepath.Join(root, ".openclawssy", "runs.json")

	reg := NewRegistry(pol, nil)
	if err := RegisterCoreWithOptions(reg, CoreOptions{EnableShellExec: true, RunsPath: runsPath}); err != nil {
		t.Fatalf("register core: %v", err)
	}
	return ws, runsPath, reg
}

type fakeRunTracker struct {
	cancels map[string]bool
}

func (f *fakeRunTracker) Cancel(runID string) error {
	if _, ok := f.cancels[runID]; !ok {
		return ErrRunNotFound
	}
	f.cancels[runID] = true
	return nil
}

func TestRunCancelTool_Success(t *testing.T) {
	tracker := &fakeRunTracker{cancels: map[string]bool{"run-123": false}}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := registerRunCancelTool(reg, tracker); err != nil {
		t.Fatalf("register run.cancel: %v", err)
	}

	res, err := reg.Execute(context.Background(), "agent", "run.cancel", ".", map[string]any{"run_id": "run-123"})
	if err != nil {
		t.Fatalf("run.cancel: %v", err)
	}

	if cancelled, _ := res["cancelled"].(bool); !cancelled {
		t.Fatalf("expected cancelled=true, got %#v", res)
	}
	if found, _ := res["found"].(bool); !found {
		t.Fatalf("expected found=true, got %#v", res)
	}
	if !tracker.cancels["run-123"] {
		t.Fatal("expected Cancel to be called on tracker")
	}
}

func TestRunCancelTool_NotFound(t *testing.T) {
	tracker := &fakeRunTracker{cancels: map[string]bool{}}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := registerRunCancelTool(reg, tracker); err != nil {
		t.Fatalf("register run.cancel: %v", err)
	}

	res, err := reg.Execute(context.Background(), "agent", "run.cancel", ".", map[string]any{"run_id": "nonexistent"})
	if err != nil {
		t.Fatalf("run.cancel: %v", err)
	}

	if found, _ := res["found"].(bool); found {
		t.Fatalf("expected found=false for nonexistent run, got %#v", res)
	}
	if cancelled, _ := res["cancelled"].(bool); cancelled {
		t.Fatalf("expected cancelled=false for nonexistent run, got %#v", res)
	}
}

func TestRunCancelTool_MissingRunID(t *testing.T) {
	tracker := &fakeRunTracker{cancels: map[string]bool{}}
	reg := NewRegistry(fakePolicy{}, nil)
	if err := registerRunCancelTool(reg, tracker); err != nil {
		t.Fatalf("register run.cancel: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "run.cancel", ".", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing run_id")
	}
}

func TestRunCancelTool_NilTracker(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	if err := registerRunCancelTool(reg, nil); err != nil {
		t.Fatalf("register run.cancel with nil tracker: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "run.cancel", ".", map[string]any{"run_id": "run-123"})
	if err == nil {
		t.Fatal("expected error when tracker is nil")
	}
}
