//go:build integration

package sandbox

import (
	"context"
	"os"
	"strings"
	"testing"

	"openclawssy/internal/config"
)

// Integration tests for DockerProvider.
// Run with: go test -tags integration -v ./internal/sandbox/... -timeout 120s
//
// These tests require a working Docker daemon accessible by the current user.
// They create real containers and volumes labelled openclawssy=true and clean
// them up automatically.

func TestDockerProviderBasic(t *testing.T) {
	cfg := config.DockerSandboxConfig{
		Image:      "ubuntu:24.04",
		PullPolicy: "if-not-present",
	}

	p, err := NewDockerProvider("test_integration", cfg)
	if err != nil {
		t.Fatalf("NewDockerProvider: %v", err)
	}

	ctx := context.Background()

	// Always clean up, even on failure.
	t.Cleanup(func() {
		_ = p.Reset(context.Background())
	})

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	t.Logf("container: %s  volume: %s  image: %s",
		p.ContainerName(), p.VolumeName(), p.ImageName())

	// ---- WriteFile should create file inside container, NOT on host ----

	content := []byte("hello from docker integration test")
	const containerPath = "/workspace/test.txt"

	if err := p.WriteFile(ctx, containerPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// File must NOT exist on the host at /workspace/test.txt
	if _, err := os.Stat("/workspace/test.txt"); err == nil {
		t.Fatal("FAIL: file should NOT exist on host at /workspace/test.txt but it does")
	}
	t.Log("PASS: file does not exist on host (correct)")

	// ---- ReadFile should return the exact bytes written ----

	got, err := p.ReadFile(ctx, containerPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("ReadFile content mismatch: got %q, want %q", got, content)
	}
	t.Logf("PASS: ReadFile returned correct content (%d bytes)", len(got))

	// ---- Exec: pwd should return /workspace ----

	result, err := p.Exec(Command{Name: "pwd"})
	if err != nil {
		t.Fatalf("Exec pwd: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "/workspace" {
		t.Fatalf("pwd expected /workspace, got %q", strings.TrimSpace(result.Stdout))
	}
	t.Logf("PASS: pwd = %s", strings.TrimSpace(result.Stdout))

	// ---- ListDir should include test.txt ----

	entries, err := p.ListDir(ctx, "/workspace")
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Name == "test.txt" {
			found = true
			t.Logf("PASS: found test.txt in ListDir (isDir=%v size=%d)", e.IsDir, e.Size)
		}
	}
	if !found {
		t.Fatalf("test.txt not found in ListDir output: %v", entries)
	}

	// ---- Lstat on existing file ----

	info, exists, err := p.Lstat(ctx, containerPath)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if !exists {
		t.Fatal("Lstat: expected exists=true for test.txt")
	}
	if info.IsDir {
		t.Fatal("Lstat: expected IsDir=false for test.txt")
	}
	t.Logf("PASS: Lstat name=%q isDir=%v size=%d", info.Name, info.IsDir, info.Size)

	// ---- Lstat on non-existent path ----

	_, exists, err = p.Lstat(ctx, "/workspace/does_not_exist.txt")
	if err != nil {
		t.Fatalf("Lstat missing: %v", err)
	}
	if exists {
		t.Fatal("Lstat missing: expected exists=false")
	}
	t.Log("PASS: Lstat returns exists=false for missing path")

	// ---- EvalSymlinks ----

	resolved, err := p.EvalSymlinks(ctx, containerPath)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if resolved == "" {
		t.Fatal("EvalSymlinks returned empty string")
	}
	t.Logf("PASS: EvalSymlinks %q -> %q", containerPath, resolved)

	// ---- MkdirAll ----

	if err := p.MkdirAll(ctx, "/workspace/subdir/nested", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	info2, exists2, err := p.Lstat(ctx, "/workspace/subdir/nested")
	if err != nil || !exists2 || !info2.IsDir {
		t.Fatalf("MkdirAll did not create directory: exists=%v isDir=%v err=%v",
			exists2, info2.IsDir, err)
	}
	t.Log("PASS: MkdirAll created /workspace/subdir/nested")

	// ---- Rename ----

	if err := p.WriteFile(ctx, "/workspace/src.txt", []byte("move me"), 0o600); err != nil {
		t.Fatalf("WriteFile for rename test: %v", err)
	}
	if err := p.Rename(ctx, "/workspace/src.txt", "/workspace/dst.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	_, srcExists, _ := p.Lstat(ctx, "/workspace/src.txt")
	_, dstExists, _ := p.Lstat(ctx, "/workspace/dst.txt")
	if srcExists {
		t.Fatal("Rename: src should no longer exist")
	}
	if !dstExists {
		t.Fatal("Rename: dst should exist")
	}
	t.Log("PASS: Rename src.txt -> dst.txt")

	// ---- Remove ----

	if err := p.Remove(ctx, "/workspace/dst.txt", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	_, dstExists2, _ := p.Lstat(ctx, "/workspace/dst.txt")
	if dstExists2 {
		t.Fatal("Remove: dst.txt should not exist after Remove")
	}
	t.Log("PASS: Remove deleted dst.txt")

	// ---- ContainerStatus ----

	status := p.ContainerStatus(ctx)
	if status != "running" {
		t.Fatalf("expected container status 'running', got %q", status)
	}
	t.Logf("PASS: ContainerStatus = %q", status)

	// ---- Stop does not remove container ----

	if err := p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	statusAfterStop := p.ContainerStatus(context.Background())
	// Container should still exist (running or exited) — we don't kill it on Stop.
	if statusAfterStop == "not_found" {
		t.Fatal("Stop should not remove the container")
	}
	t.Logf("PASS: container still exists after Stop (status=%q)", statusAfterStop)
}

func TestDockerProviderWriteFileNoHostSideEffect(t *testing.T) {
	// Focused test: write a file and assert host filesystem is untouched.
	cfg := config.DockerSandboxConfig{
		Image:      "ubuntu:24.04",
		PullPolicy: "if-not-present",
	}

	p, err := NewDockerProvider("test_isolation", cfg)
	if err != nil {
		t.Fatalf("NewDockerProvider: %v", err)
	}

	ctx := context.Background()
	t.Cleanup(func() { _ = p.Reset(context.Background()) })

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	paths := []string{
		"/workspace/isolation_test.txt",
		"/workspace/subdir/deep.txt",
	}

	for _, cp := range paths {
		if err := p.WriteFile(ctx, cp, []byte("isolation check"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", cp, err)
		}
		// Check host filesystem — these paths must NOT exist.
		if _, err := os.Stat(cp); err == nil {
			t.Fatalf("FAIL isolation: %s exists on host after WriteFile!", cp)
		}
		t.Logf("PASS: %s does NOT exist on host", cp)
	}
}

func TestDockerProviderSanitizeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"myagent", "myagent"},
		{"my-agent", "my-agent"},
		{"my agent", "my_agent"},
		{"My/Agent", "my_agent"},
		{"", "default"},
		{"UPPER", "upper"},
		{"123", "123"},
	}
	for _, tc := range cases {
		got := sanitizeDockerName(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeDockerName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
