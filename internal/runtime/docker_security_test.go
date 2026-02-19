package runtime

import (
	"strings"
	"testing"
)

// TestDockerResolvePath verifies that dockerResolvePath enforces /workspace
// containment at the engine/policy layer, providing a second line of defence
// independent of the sandbox.validateContainerPath guard.
//
// These tests do NOT require Docker — they exercise pure Go path logic.
func TestDockerResolvePath(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		wantErr  bool
		wantPath string
	}{
		// ---- valid relative paths ----
		{"relative file", "foo.txt", false, "/workspace/foo.txt"},
		{"relative nested", "a/b/c.txt", false, "/workspace/a/b/c.txt"},
		{"dot file", ".hidden", false, "/workspace/.hidden"},

		// ---- valid absolute paths ----
		{"absolute workspace root", "/workspace", false, "/workspace"},
		{"absolute workspace file", "/workspace/foo.txt", false, "/workspace/foo.txt"},
		{"absolute workspace nested", "/workspace/a/b/c", false, "/workspace/a/b/c"},

		// ---- traversal attempts — relative ----
		{"relative traversal 2 dots", "../../etc/passwd", true, ""},
		{"relative traversal embedded", "a/../../etc/passwd", true, ""},
		{"relative traversal dotdot", "../etc/shadow", true, ""},

		// ---- traversal attempts — absolute ----
		{"absolute traversal via workspace", "/workspace/../../etc/passwd", true, ""},
		{"absolute traversal via workspace 2", "/workspace/../etc/shadow", true, ""},

		// ---- host absolute paths ----
		{"host /etc/passwd", "/etc/passwd", true, ""},
		{"host /home", "/home/user/.ssh/id_rsa", true, ""},
		{"host /proc/self/environ", "/proc/self/environ", true, ""},
		{"root /", "/", true, ""},
		{"host /tmp", "/tmp/evil", true, ""},
		{"host /var/run/docker.sock", "/var/run/docker.sock", true, ""},

		// ---- null byte injection ----
		// Null bytes are stripped before validation. After stripping:
		//   "foo\x00bar" → "foobar" → resolves to /workspace/foobar (allowed)
		//   "\x00foo"    → "foo"    → resolves to /workspace/foo    (allowed)
		// The null bytes themselves do not cause an error; the sanitized path
		// must still pass all other guards.  Only a null-only input fails.
		{"null byte only", "\x00", true, ""},

		// ---- empty and whitespace paths ----
		{"empty string", "", true, ""},
		// ---- whitespace only ----
		{"whitespace only", "   ", true, ""},
	}

	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			got, err := dockerResolvePath("", tc.target)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for target %q, got path %q", tc.target, got)
				return
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for target %q: %v", tc.target, err)
				return
			}
			if !tc.wantErr && tc.wantPath != "" && got != tc.wantPath {
				t.Errorf("path mismatch for %q: got %q, want %q", tc.target, got, tc.wantPath)
			}
		})
	}
}

// TestDockerResolvePathContainmentGuarantee is an exhaustive fuzz-style test
// of edge cases that could conceivably bypass the earlier traversal check
// but then produce an out-of-workspace path after filepath.Clean().
func TestDockerResolvePathContainmentGuarantee(t *testing.T) {
	// All of these should produce an error — none should resolve to a path
	// outside /workspace.
	malicious := []string{
		"/etc/passwd",
		"/proc/1/mem",
		"/sys/kernel/security/apparmor/policy",
		"/var/run/docker.sock",
		"/root/.ssh/authorized_keys",
		"/workspace/../../../etc/passwd",
		"workspace/../../etc/crontab",
		strings.Repeat("../", 20) + "etc/passwd",
	}

	for _, target := range malicious {
		target := target
		t.Run(target, func(t *testing.T) {
			got, err := dockerResolvePath("", target)
			if err != nil {
				return // correctly rejected
			}
			// If no error, the path MUST be within /workspace.
			if got != "/workspace" && !strings.HasPrefix(got, "/workspace/") {
				t.Errorf("SECURITY: dockerResolvePath(%q) returned out-of-workspace path %q with no error",
					target, got)
			}
		})
	}
}

// TestDockerResolvePathLengthLimit verifies excessively long paths are rejected.
func TestDockerResolvePathLengthLimit(t *testing.T) {
	// Build a path clearly over 4096 bytes: "a/" repeated 2049 times = 4098 bytes.
	long := strings.Repeat("a/", 2049) + "file.txt"
	if len(long) <= 4096 {
		t.Fatalf("test setup error: path is only %d bytes, expected > 4096", len(long))
	}
	_, err := dockerResolvePath("", long)
	if err == nil {
		t.Errorf("expected error for path of %d bytes (> 4096), got nil", len(long))
	}
}
