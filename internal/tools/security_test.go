package tools

import (
	"context"
	"strings"
	"testing"
)

func TestShellExecDefaultDenyAndAllowlist(t *testing.T) {
	reg := NewRegistry(fakePolicy{}, nil)
	reg.SetShellExecutor(fakeShell{})
	if err := RegisterCore(reg); err != nil {
		t.Fatalf("register core: %v", err)
	}

	_, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"should be denied"},
	})
	if err == nil {
		t.Fatal("expected policy denied error for empty allowlist")
	}
	if !strings.Contains(err.Error(), "command is not allowed") {
		t.Fatalf("expected allowlist denial error, got %v", err)
	}

	// "*" glob matches any invocation
	reg.SetShellAllowedCommands([]string{"*"})
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"allowed by wildcard"},
	}); err != nil {
		t.Fatalf("expected wildcard allowlist to allow command, got %v", err)
	}

	// "ls *" matches ls with any arguments
	reg.SetShellAllowedCommands([]string{"ls *"})
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "ls",
		"args":    []any{"-la"},
	}); err != nil {
		t.Fatalf("expected 'ls *' to allow 'ls -la', got %v", err)
	}
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"still denied"},
	}); err == nil {
		t.Fatal("expected echo to be denied when only 'ls *' is allowlisted")
	}

	// exact match without wildcards — use an argument so fakeShell doesn't panic
	reg.SetShellAllowedCommands([]string{"ls ."})
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "ls",
		"args":    []any{"."},
	}); err != nil {
		t.Fatalf("expected exact 'ls .' pattern to match 'ls .', got %v", err)
	}
	// bare "ls ." pattern must not match "ls -la" (no wildcard)
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "ls",
		"args":    []any{"-la"},
	}); err == nil {
		t.Fatal("expected 'ls .' (no wildcard) to NOT match 'ls -la'")
	}

	// "?" matches exactly one character
	reg.SetShellAllowedCommands([]string{"l? ."})
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "ls",
		"args":    []any{"."},
	}); err != nil {
		t.Fatalf("expected 'l? .' to match 'ls .', got %v", err)
	}
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	}); err == nil {
		t.Fatal("expected 'l? .' to NOT match 'echo hello'")
	}

	// last matching rule wins — deny after allow
	reg.SetShellAllowedCommands([]string{"git *", "git commit *"})
	// "git status" matches "git *" → allowed
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "git",
		"args":    []any{"status"},
	}); err != nil {
		t.Fatalf("expected 'git status' to be allowed, got %v", err)
	}
	// "git commit -m msg" matches both "git *" (allow) and "git commit *" (allow) — still allowed
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "git",
		"args":    []any{"commit", "-m", "msg"},
	}); err != nil {
		t.Fatalf("expected 'git commit' to be allowed, got %v", err)
	}
}

func TestCommandMatchesPattern(t *testing.T) {
	cases := []struct {
		invocation string
		pattern    string
		want       bool
	}{
		{"git status", "git *", true},
		{"git status --porcelain", "git *", true},
		{"git commit -m msg", "git commit *", true},
		{"echo hello", "git *", false},
		{"ls", "ls", true},
		{"ls -la", "ls", false},
		{"ls -la", "ls *", true},
		{"ls", "l?", true},
		{"ld", "l?", true},
		{"echo", "l?", false},
		{"any command", "*", true},
		{"", "git *", false},
		{"git status", "", false},
	}
	for _, tc := range cases {
		got, ok := commandMatchesPattern(tc.invocation, tc.pattern)
		if !ok {
			t.Errorf("commandMatchesPattern(%q, %q): malformed pattern", tc.invocation, tc.pattern)
			continue
		}
		if got != tc.want {
			t.Errorf("commandMatchesPattern(%q, %q) = %v, want %v", tc.invocation, tc.pattern, got, tc.want)
		}
	}
}
