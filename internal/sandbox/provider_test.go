package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"openclawssy/internal/config"
)

func TestNoneProviderExecDenied(t *testing.T) {
	p := &NoneProvider{}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("start none provider: %v", err)
	}
	_, err := p.Exec(Command{Name: "pwd"})
	if !errors.Is(err, ErrExecDenied) {
		t.Fatalf("expected ErrExecDenied, got %v", err)
	}
}

func TestShellExecAllowedGating(t *testing.T) {
	none := &NoneProvider{}
	if ShellExecAllowed(none) {
		t.Fatal("none provider should never allow exec before start")
	}
	if err := none.Start(context.Background()); err != nil {
		t.Fatalf("start none provider: %v", err)
	}
	if ShellExecAllowed(none) {
		t.Fatal("none provider should never allow exec after start")
	}

	local, err := NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("new local provider: %v", err)
	}
	if ShellExecAllowed(local) {
		t.Fatal("local provider should not allow exec before start")
	}
	if err := local.Start(context.Background()); err != nil {
		t.Fatalf("start local provider: %v", err)
	}
	if !ShellExecAllowed(local) {
		t.Fatal("local provider should allow exec after start")
	}
}

func TestNewProviderRejectsUnsupportedProvider(t *testing.T) {
	_, err := NewProvider("badprovider", t.TempDir())
	if err == nil {
		t.Fatalf("expected unsupported provider error")
	}
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("expected ErrUnknownProvider, got %v", err)
	}
}

func TestNewProviderDockerReturnsNotYetError(t *testing.T) {
	_, err := NewProvider("docker", t.TempDir())
	if err == nil {
		t.Fatalf("expected docker provider error")
	}
	if !errors.Is(err, ErrDockerNotYet) {
		t.Fatalf("expected ErrDockerNotYet, got %v", err)
	}
}

// ---- LocalProvider file operation tests ----

func TestLocalProviderWriteAndReadFile(t *testing.T) {
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	if err != nil {
		t.Fatalf("new local provider: %v", err)
	}
	ctx := context.Background()
	path := filepath.Join(dir, "hello.txt")
	data := []byte("hello world")
	if err := p.WriteFile(ctx, path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := p.ReadFile(ctx, path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("ReadFile content mismatch: got %q want %q", got, data)
	}
}

func TestLocalProviderListDir(t *testing.T) {
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	if err != nil {
		t.Fatalf("new local provider: %v", err)
	}
	ctx := context.Background()

	// Create a couple of files and a sub-directory.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	entries, err := p.ListDir(ctx, dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(entries), entries)
	}
	nameSet := make(map[string]bool)
	for _, e := range entries {
		nameSet[e.Name] = true
	}
	for _, name := range []string{"a.txt", "b.txt", "subdir"} {
		if !nameSet[name] {
			t.Fatalf("expected entry %q in list, got %v", name, entries)
		}
	}
}

func TestLocalProviderMkdirAll(t *testing.T) {
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	if err != nil {
		t.Fatalf("new local provider: %v", err)
	}
	ctx := context.Background()
	newDir := filepath.Join(dir, "a", "b", "c")
	if err := p.MkdirAll(ctx, newDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("stat new dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected directory, got file")
	}
}

func TestLocalProviderRemove(t *testing.T) {
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	if err != nil {
		t.Fatalf("new local provider: %v", err)
	}
	ctx := context.Background()

	// Remove a single file.
	filePath := filepath.Join(dir, "remove_me.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := p.Remove(ctx, filePath, false); err != nil {
		t.Fatalf("Remove file: %v", err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err=%v", err)
	}

	// Remove a directory recursively.
	subDir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "child.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write child fixture: %v", err)
	}
	if err := p.Remove(ctx, subDir, true); err != nil {
		t.Fatalf("Remove dir recursive: %v", err)
	}
	if _, err := os.Stat(subDir); !os.IsNotExist(err) {
		t.Fatalf("expected dir removed, stat err=%v", err)
	}
}

func TestLocalProviderRename(t *testing.T) {
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	if err != nil {
		t.Fatalf("new local provider: %v", err)
	}
	ctx := context.Background()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("content"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := p.Rename(ctx, src, dst); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected source removed, stat err=%v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(got) != "content" {
		t.Fatalf("unexpected destination content: %q", got)
	}
}

func TestLocalProviderLstat(t *testing.T) {
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	if err != nil {
		t.Fatalf("new local provider: %v", err)
	}
	ctx := context.Background()

	// Existing file.
	filePath := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	info, exists, err := p.Lstat(ctx, filePath)
	if err != nil {
		t.Fatalf("Lstat existing: %v", err)
	}
	if !exists {
		t.Fatalf("expected exists=true for existing file")
	}
	if info.Name != "exists.txt" {
		t.Fatalf("expected name=exists.txt, got %q", info.Name)
	}
	if info.IsDir {
		t.Fatalf("expected IsDir=false for file")
	}

	// Non-existent path.
	_, exists, err = p.Lstat(ctx, filepath.Join(dir, "missing.txt"))
	if err != nil {
		t.Fatalf("Lstat missing: %v", err)
	}
	if exists {
		t.Fatalf("expected exists=false for missing path")
	}
}

func TestLocalProviderEvalSymlinks(t *testing.T) {
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	if err != nil {
		t.Fatalf("new local provider: %v", err)
	}
	ctx := context.Background()

	// EvalSymlinks on a real path should succeed.
	resolved, err := p.EvalSymlinks(ctx, dir)
	if err != nil {
		t.Fatalf("EvalSymlinks dir: %v", err)
	}
	if resolved == "" {
		t.Fatalf("expected non-empty resolved path")
	}

	// EvalSymlinks on a missing path should return error.
	_, err = p.EvalSymlinks(ctx, filepath.Join(dir, "nonexistent"))
	if err == nil {
		t.Fatalf("expected error for nonexistent path")
	}
}

// ---- NoneProvider file operation tests ----

func TestNoneProviderFileOpsReturnError(t *testing.T) {
	p := &NoneProvider{}
	ctx := context.Background()

	if _, err := p.ReadFile(ctx, "/tmp/x"); !errors.Is(err, errFileOpsNotAvailable) {
		t.Fatalf("ReadFile: expected errFileOpsNotAvailable, got %v", err)
	}
	if err := p.WriteFile(ctx, "/tmp/x", nil, 0o600); !errors.Is(err, errFileOpsNotAvailable) {
		t.Fatalf("WriteFile: expected errFileOpsNotAvailable, got %v", err)
	}
	if _, err := p.ListDir(ctx, "/tmp"); !errors.Is(err, errFileOpsNotAvailable) {
		t.Fatalf("ListDir: expected errFileOpsNotAvailable, got %v", err)
	}
	if err := p.MkdirAll(ctx, "/tmp/x", 0o755); !errors.Is(err, errFileOpsNotAvailable) {
		t.Fatalf("MkdirAll: expected errFileOpsNotAvailable, got %v", err)
	}
	if err := p.Remove(ctx, "/tmp/x", false); !errors.Is(err, errFileOpsNotAvailable) {
		t.Fatalf("Remove: expected errFileOpsNotAvailable, got %v", err)
	}
	if err := p.Rename(ctx, "/tmp/a", "/tmp/b"); !errors.Is(err, errFileOpsNotAvailable) {
		t.Fatalf("Rename: expected errFileOpsNotAvailable, got %v", err)
	}
	if _, _, err := p.Lstat(ctx, "/tmp/x"); !errors.Is(err, errFileOpsNotAvailable) {
		t.Fatalf("Lstat: expected errFileOpsNotAvailable, got %v", err)
	}
	if _, err := p.EvalSymlinks(ctx, "/tmp/x"); !errors.Is(err, errFileOpsNotAvailable) {
		t.Fatalf("EvalSymlinks: expected errFileOpsNotAvailable, got %v", err)
	}
}

// ---- DockerProvider unit tests (no Docker daemon needed) ----

func TestSanitizeDockerName(t *testing.T) {
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
		{"abc.def", "abc_def"},
	}
	for _, tc := range cases {
		got := sanitizeDockerName(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeDockerName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestShellescape(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello", "'hello'"},
		{"with space", "'with space'"},
		{"it's", `'it'"'"'s'`},
		{"/workspace/foo.txt", "'/workspace/foo.txt'"},
		{"", "''"},
	}
	for _, tc := range cases {
		got := shellescape(tc.in)
		if got != tc.want {
			t.Errorf("shellescape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNewDockerProviderDefaults(t *testing.T) {
	p, err := NewDockerProvider("myagent", config.DockerSandboxConfig{})
	if err != nil {
		t.Fatalf("NewDockerProvider: %v", err)
	}
	if p.ImageName() != "ubuntu:24.04" {
		t.Errorf("default image want ubuntu:24.04, got %q", p.ImageName())
	}
	if p.ContainerName() != "openclawssy_agent_myagent" {
		t.Errorf("container name want openclawssy_agent_myagent, got %q", p.ContainerName())
	}
	if p.VolumeName() != "openclawssy_ws_myagent" {
		t.Errorf("volume name want openclawssy_ws_myagent, got %q", p.VolumeName())
	}
}

func TestNewDockerProviderEmptyAgentID(t *testing.T) {
	_, err := NewDockerProvider("", config.DockerSandboxConfig{})
	if err == nil {
		t.Fatal("expected error for empty agentID")
	}
}

func TestNewDockerProviderCustomImage(t *testing.T) {
	p, err := NewDockerProvider("agent1", config.DockerSandboxConfig{Image: "debian:12"})
	if err != nil {
		t.Fatalf("NewDockerProvider: %v", err)
	}
	if p.ImageName() != "debian:12" {
		t.Errorf("expected image debian:12, got %q", p.ImageName())
	}
}

func TestDockerProviderFileOpsRequireStart(t *testing.T) {
	// Verify that file ops return ErrNotStarted before Start is called.
	p, err := NewDockerProvider("unstartedagent", config.DockerSandboxConfig{})
	if err != nil {
		t.Fatalf("NewDockerProvider: %v", err)
	}
	ctx := context.Background()

	if _, err := p.ReadFile(ctx, "/workspace/x"); !errors.Is(err, ErrNotStarted) {
		t.Errorf("ReadFile before Start: want ErrNotStarted, got %v", err)
	}
	if err := p.WriteFile(ctx, "/workspace/x", nil, 0o600); !errors.Is(err, ErrNotStarted) {
		t.Errorf("WriteFile before Start: want ErrNotStarted, got %v", err)
	}
	if _, err := p.ListDir(ctx, "/workspace"); !errors.Is(err, ErrNotStarted) {
		t.Errorf("ListDir before Start: want ErrNotStarted, got %v", err)
	}
	if err := p.MkdirAll(ctx, "/workspace/d", 0o755); !errors.Is(err, ErrNotStarted) {
		t.Errorf("MkdirAll before Start: want ErrNotStarted, got %v", err)
	}
	if err := p.Remove(ctx, "/workspace/x", false); !errors.Is(err, ErrNotStarted) {
		t.Errorf("Remove before Start: want ErrNotStarted, got %v", err)
	}
	if err := p.Rename(ctx, "/workspace/a", "/workspace/b"); !errors.Is(err, ErrNotStarted) {
		t.Errorf("Rename before Start: want ErrNotStarted, got %v", err)
	}
	if _, _, err := p.Lstat(ctx, "/workspace/x"); !errors.Is(err, ErrNotStarted) {
		t.Errorf("Lstat before Start: want ErrNotStarted, got %v", err)
	}
	if _, err := p.EvalSymlinks(ctx, "/workspace/x"); !errors.Is(err, ErrNotStarted) {
		t.Errorf("EvalSymlinks before Start: want ErrNotStarted, got %v", err)
	}
	if _, err := p.Exec(Command{Name: "echo"}); !errors.Is(err, ErrNotStarted) {
		t.Errorf("Exec before Start: want ErrNotStarted, got %v", err)
	}
}

func TestNewProviderForAgentDocker(t *testing.T) {
	// NewProviderForAgent for docker should return a *DockerProvider, not an error.
	p, err := NewProviderForAgent("docker", t.TempDir(), "testagent", config.DockerSandboxConfig{})
	if err != nil {
		t.Fatalf("NewProviderForAgent docker: %v", err)
	}
	dp, ok := p.(*DockerProvider)
	if !ok {
		t.Fatalf("expected *DockerProvider, got %T", p)
	}
	if dp.ContainerName() != "openclawssy_agent_testagent" {
		t.Errorf("unexpected container name: %q", dp.ContainerName())
	}
}

func TestNewProviderForAgentDefaultAgentID(t *testing.T) {
	// Empty agentID should default to "default".
	p, err := NewProviderForAgent("docker", t.TempDir(), "", config.DockerSandboxConfig{})
	if err != nil {
		t.Fatalf("NewProviderForAgent docker empty agentID: %v", err)
	}
	dp, ok := p.(*DockerProvider)
	if !ok {
		t.Fatalf("expected *DockerProvider, got %T", p)
	}
	if dp.ContainerName() != "openclawssy_agent_default" {
		t.Errorf("expected container name openclawssy_agent_default, got %q", dp.ContainerName())
	}
}
