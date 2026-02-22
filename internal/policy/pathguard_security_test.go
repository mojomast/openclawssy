package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveReadPath_Security validates that protected control files
// cannot be read via ResolveReadPath, preventing sensitive data leakage.
func TestResolveReadPath_Security(t *testing.T) {
	ws := t.TempDir()

	// Create .openclawssy directory
	controlDir := filepath.Join(ws, ".openclawssy")
	if err := os.Mkdir(controlDir, 0755); err != nil {
		t.Fatalf("failed to create control dir: %v", err)
	}

	// Create protected files
	protectedFiles := []string{
		"master.key",
		"secrets.enc",
		"config.json",
	}

	for _, f := range protectedFiles {
		p := filepath.Join(controlDir, f)
		if err := os.WriteFile(p, []byte("secret-content"), 0600); err != nil {
			t.Fatalf("failed to write %s: %v", f, err)
		}
	}

	// Create normal file
	normalPath := filepath.Join(ws, "normal.txt")
	if err := os.WriteFile(normalPath, []byte("safe-content"), 0644); err != nil {
		t.Fatalf("failed to write normal.txt: %v", err)
	}

	e := &Enforcer{}

	// Test 1: Protected files should be blocked
	for _, f := range protectedFiles {
		target := filepath.Join(".openclawssy", f)
		_, err := e.ResolveReadPath(ws, target)
		if err == nil {
			t.Errorf("SECURITY VULNERABILITY: ResolveReadPath allowed reading protected file: %s", target)
		} else {
			// Verify it's the correct error
			pathErr, ok := err.(*PathError)
			if !ok {
				t.Errorf("Expected PathError, got %T: %v", err, err)
			} else if pathErr.Reason != "protected control-plane path" {
				t.Errorf("Expected 'protected control-plane path' error, got: %s", pathErr.Reason)
			}
		}
	}

	// Test 2: Reading .openclawssy directory itself should be blocked
	// because it matches the protected path check (ends with .openclawssy)
	_, err := e.ResolveReadPath(ws, ".openclawssy")
	if err == nil {
		t.Errorf("SECURITY VULNERABILITY: ResolveReadPath allowed resolving .openclawssy directory")
	} else {
		pathErr, ok := err.(*PathError)
		if !ok || pathErr.Reason != "protected control-plane path" {
			t.Errorf("Expected protected path error for .openclawssy, got: %v", err)
		}
	}

	// Test 3: Normal file should be allowed
	resolved, err := e.ResolveReadPath(ws, "normal.txt")
	if err != nil {
		t.Errorf("Failed to read normal file: %v", err)
	}
	// On Mac/Linux, resolved path might have /private prefix or similar, so check suffix or use exact match if deterministic
	// t.TempDir returns resolved path usually.
	// Let's just check it ends with normal.txt and exists
	if !strings.HasSuffix(resolved, "normal.txt") {
		t.Errorf("Resolved path %s does not end with normal.txt", resolved)
	}

	// Test 4: Protected file in subdirectory of agents
	// Structure: .openclawssy/agents/<agent>/config.json
	agentDir := filepath.Join(controlDir, "agents", "test-agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("failed to create agent dir: %v", err)
	}
	agentConfig := filepath.Join(agentDir, "config.json")
	if err := os.WriteFile(agentConfig, []byte("{}"), 0600); err != nil {
		t.Fatalf("failed to write agent config: %v", err)
	}

	targetAgentConfig := filepath.Join(".openclawssy", "agents", "test-agent", "config.json")
	_, err = e.ResolveReadPath(ws, targetAgentConfig)
	if err == nil {
		t.Errorf("SECURITY VULNERABILITY: Allowed reading agent config: %s", targetAgentConfig)
	}
}

// TestResolveWritePath_Security confirms write protection is still active
func TestResolveWritePath_Security(t *testing.T) {
	ws := t.TempDir()

	// Create .openclawssy directory so parent resolution succeeds
	controlDir := filepath.Join(ws, ".openclawssy")
	if err := os.Mkdir(controlDir, 0755); err != nil {
		t.Fatalf("failed to create control dir: %v", err)
	}

	e := &Enforcer{}

	// Try to write to .openclawssy/config.json
	target := ".openclawssy/config.json"
	_, err := e.ResolveWritePath(ws, target)
	if err == nil {
		t.Errorf("SECURITY VULNERABILITY: Allowed writing to protected file: %s", target)
	} else {
		pathErr, ok := err.(*PathError)
		if !ok || pathErr.Reason != "protected control-plane path" {
			t.Errorf("Expected protected path error for write, got: %v", err)
		}
	}
}

func TestSecurity_MasterKeyLeak(t *testing.T) {
	ws := t.TempDir()

	// Create master.key in the workspace root (simulating user mistake or misconfiguration)
	secret := "SUPER_SECRET_KEY"
	keyPath := filepath.Join(ws, "master.key")
	if err := os.WriteFile(keyPath, []byte(secret), 0600); err != nil {
		t.Fatalf("failed to write master.key: %v", err)
	}

	e := &Enforcer{}

	// Try to resolve it for reading
	// If it succeeds, the agent can read the master key!
	resolved, err := e.ResolveReadPath(ws, "master.key")
	if err == nil {
		t.Errorf("CRITICAL VULNERABILITY: Allowed reading master.key from workspace root: %s", resolved)
	} else {
		// Verify it's the correct error
		pathErr, ok := err.(*PathError)
		if !ok {
			t.Errorf("Expected PathError, got %T: %v", err, err)
		} else if pathErr.Reason != "protected control-plane path" {
			t.Errorf("Expected 'protected control-plane path' error, got: %s", pathErr.Reason)
		}
	}

	// Try to resolve it for writing
	// If it succeeds, the agent can overwrite the master key!
	resolvedWrite, err := e.ResolveWritePath(ws, "master.key")
	if err == nil {
		t.Errorf("CRITICAL VULNERABILITY: Allowed writing master.key to workspace root: %s", resolvedWrite)
	} else {
		pathErr, ok := err.(*PathError)
		if !ok || pathErr.Reason != "protected control-plane path" {
			t.Errorf("Expected protected path error for write, got: %v", err)
		}
	}
}
