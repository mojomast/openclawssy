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

	reg.SetShellAllowedCommands([]string{"*"})
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"allowed by wildcard"},
	}); err != nil {
		t.Fatalf("expected wildcard allowlist to allow command, got %v", err)
	}

	reg.SetShellAllowedCommands([]string{"ls"})
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "ls",
		"args":    []any{"-la"},
	}); err != nil {
		t.Fatalf("expected ls to be allowed, got %v", err)
	}
	if _, err := reg.Execute(context.Background(), "agent", "shell.exec", ".", map[string]any{
		"command": "echo",
		"args":    []any{"still denied"},
	}); err == nil {
		t.Fatal("expected echo to be denied when only ls is allowlisted")
	}
}
