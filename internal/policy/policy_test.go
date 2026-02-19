package policy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTraversalDenied(t *testing.T) {
	ws := t.TempDir()
	enf := NewEnforcer(ws, map[string][]string{"agent": {"fs.read"}})

	_, err := enf.ResolveReadPath(ws, "../secret.txt")
	if err == nil {
		t.Fatalf("expected traversal denial")
	}
}

func TestHasTraversalWindowsStylePaths(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: `..\\secret.txt`, want: true},
		{path: `safe\\nested\\file.txt`, want: false},
		{path: `C:\\temp\\..\\secret.txt`, want: true},
		{path: `C:\\temp\\logs\\app.txt`, want: false},
		{path: `\\\\server\\share\\dir\\..\\secret.txt`, want: true},
	}

	for _, tc := range tests {
		if got := HasTraversal(tc.path); got != tc.want {
			t.Fatalf("HasTraversal(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestResolveWritePathRejectsWindowsTraversalVariants(t *testing.T) {
	ws := t.TempDir()
	enf := NewEnforcer(ws, map[string][]string{"agent": {"fs.write"}})

	paths := []string{`..\\secret.txt`, `dir\\..\\secret.txt`, `dir/..\\secret.txt`, `C:relative\\file.txt`}
	for _, target := range paths {
		_, err := enf.ResolveWritePath(ws, target)
		if err == nil {
			t.Fatalf("expected traversal/invalid path denial for %q", target)
		}
		pathErr, ok := err.(*PathError)
		if !ok {
			t.Fatalf("expected PathError for %q, got %T", target, err)
		}
		if pathErr.Reason != "path traversal" && pathErr.Reason != "invalid path" {
			t.Fatalf("unexpected reason for %q: %s", target, pathErr.Reason)
		}
	}
}

func TestResolveWritePathRejectsWindowsAbsoluteTargets(t *testing.T) {
	ws := t.TempDir()
	enf := NewEnforcer(ws, map[string][]string{"agent": {"fs.write"}})

	absoluteTargets := []string{`C:\\Windows\\System32\\drivers\\etc\\hosts`, `\\\\server\\share\\secret.txt`}
	for _, target := range absoluteTargets {
		_, err := enf.ResolveWritePath(ws, target)
		if err == nil {
			t.Fatalf("expected absolute target %q to be denied", target)
		}
		if !strings.Contains(err.Error(), "outside workspace") && !strings.Contains(err.Error(), "write parent does not exist") {
			t.Fatalf("expected absolute path denial reason for %q, got %v", target, err)
		}
	}
}

func TestIsAbsoluteTargetRecognizesWindowsForms(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{target: `C:\\repo\\file.txt`, want: true},
		{target: `C:/repo/file.txt`, want: true},
		{target: `\\\\server\\share\\file.txt`, want: true},
		{target: `//server/share/file.txt`, want: true},
		{target: `C:relative\\file.txt`, want: false},
		{target: `workspace\\file.txt`, want: false},
	}
	for _, tc := range tests {
		if got := isAbsoluteTarget(tc.target); got != tc.want {
			t.Fatalf("isAbsoluteTarget(%q) = %v, want %v", tc.target, got, tc.want)
		}
	}
}

func TestSymlinkEscapeDeniedOnRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior requires elevated privileges on many windows setups")
	}

	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}

	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	link := filepath.Join(ws, "escape.txt")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	enf := NewEnforcer(ws, map[string][]string{"agent": {"fs.read"}})
	_, err := enf.ResolveReadPath(ws, "escape.txt")
	if err == nil {
		t.Fatalf("expected symlink escape denial")
	}
}

func TestSymlinkEscapeDeniedOnWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior requires elevated privileges on many windows setups")
	}

	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}

	linkDir := filepath.Join(ws, "out")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Fatalf("create symlink dir: %v", err)
	}

	enf := NewEnforcer(ws, map[string][]string{"agent": {"fs.write"}})
	_, err := enf.ResolveWritePath(ws, filepath.Join("out", "new.txt"))
	if err == nil {
		t.Fatalf("expected symlink write escape denial")
	}
}

func TestProtectedPathDeniedOnWrite(t *testing.T) {
	root := t.TempDir()
	ws := root
	if err := os.MkdirAll(filepath.Join(root, ".openclawssy", "agents", "default"), 0o755); err != nil {
		t.Fatalf("mkdir control plane: %v", err)
	}

	enf := NewEnforcer(ws, map[string][]string{"agent": {"fs.write"}})
	if _, err := enf.ResolveWritePath(ws, ".openclawssy/config.json"); err == nil {
		t.Fatalf("expected protected config denial")
	}
	if _, err := enf.ResolveWritePath(ws, ".openclawssy/agents/default/SOUL.md"); err == nil {
		t.Fatalf("expected protected SOUL denial")
	}
	if _, err := enf.ResolveWritePath(ws, ".openclawssy/secrets.enc"); err == nil {
		t.Fatalf("expected protected secrets denial")
	}
}

func TestProtectedPathDeniedViaSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior requires elevated privileges on many windows setups")
	}
	root := t.TempDir()
	ws := root
	if err := os.MkdirAll(filepath.Join(root, ".openclawssy", "agents", "default"), 0o755); err != nil {
		t.Fatalf("mkdir control plane: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "workspace"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, ".openclawssy"), filepath.Join(root, "workspace", "cp")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	enf := NewEnforcer(ws, map[string][]string{"agent": {"fs.write"}})
	if _, err := enf.ResolveWritePath(ws, "workspace/cp/config.json"); err == nil {
		t.Fatalf("expected symlink protected path denial")
	}
}

func TestCheckToolCanonicalizesGrantAlias(t *testing.T) {
	enf := NewEnforcer(t.TempDir(), map[string][]string{"agent": {"bash.exec"}})
	if err := enf.CheckTool("agent", "shell.exec"); err != nil {
		t.Fatalf("expected shell.exec allowed via bash.exec alias grant, got %v", err)
	}
}

func TestCheckToolCanonicalizesRequestAlias(t *testing.T) {
	enf := NewEnforcer(t.TempDir(), map[string][]string{"agent": {"shell.exec"}})
	if err := enf.CheckTool("agent", "bash.exec"); err != nil {
		t.Fatalf("expected bash.exec allowed via shell.exec grant, got %v", err)
	}
}

func TestSymlinkFileEscapeDeniedOnWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior requires elevated privileges on many windows setups")
	}

	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	outsideFile := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	linkPath := filepath.Join(ws, "link.txt")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	enf := NewEnforcer(ws, map[string][]string{"agent": {"fs.write"}})

	// Attempt to resolve write path for the symlink
	_, err := enf.ResolveWritePath(ws, "link.txt")
	if err == nil {
		t.Fatalf("expected symlink write escape denial")
	}
	pathErr, ok := err.(*PathError)
	if !ok || pathErr.Reason != "outside workspace" {
		t.Fatalf("expected outside workspace denial, got %v", err)
	}
}
