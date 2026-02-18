package policy

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadGrants(t *testing.T) {
	t.Run("NonExistentFile", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nonexistent.json")
		grants, err := LoadGrants(path)
		if err != nil {
			t.Fatalf("LoadGrants failed: %v", err)
		}
		if len(grants) != 0 {
			t.Errorf("expected empty map, got %v", grants)
		}
	})

	t.Run("EmptyPath", func(t *testing.T) {
		grants, err := LoadGrants("")
		if err != nil {
			t.Fatalf("LoadGrants failed: %v", err)
		}
		if len(grants) != 0 {
			t.Errorf("expected empty map, got %v", grants)
		}
	})

	t.Run("EmptyFile", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.json")
		if err := os.WriteFile(path, []byte{}, 0644); err != nil {
			t.Fatalf("failed to create empty file: %v", err)
		}
		grants, err := LoadGrants(path)
		if err != nil {
			t.Fatalf("LoadGrants failed: %v", err)
		}
		if len(grants) != 0 {
			t.Errorf("expected empty map, got %v", grants)
		}
	})

	t.Run("ValidJSON", func(t *testing.T) {
		content := `{"agents": {"agent1": ["fs.read", "BASH.EXEC"], "agent2": ["fs.write"]}}`
		path := filepath.Join(t.TempDir(), "grants.json")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create grants file: %v", err)
		}

		grants, err := LoadGrants(path)
		if err != nil {
			t.Fatalf("LoadGrants failed: %v", err)
		}

		expected := map[string][]string{
			"agent1": {"fs.read", "shell.exec"}, // BASH.EXEC -> shell.exec and sorted
			"agent2": {"fs.write"},
		}

		if !reflect.DeepEqual(grants, expected) {
			t.Errorf("expected %v, got %v", expected, grants)
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		content := `{"agents": {`
		path := filepath.Join(t.TempDir(), "invalid.json")
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create invalid file: %v", err)
		}

		_, err := LoadGrants(path)
		if err == nil {
			t.Fatal("expected error for invalid JSON, got nil")
		}
	})
}

func TestSaveGrants(t *testing.T) {
	t.Run("SaveAndPermissions", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "subdir", "grants.json")

		grants := map[string][]string{
			"agent1": {"fs.read", "bash.exec"},
		}

		if err := SaveGrants(path, grants); err != nil {
			t.Fatalf("SaveGrants failed: %v", err)
		}

		// Check if file exists
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("failed to stat file: %v", err)
		}

		// Check permissions (on Unix-like systems)
		mode := info.Mode().Perm()
		if mode&0777 != 0600 {
			t.Errorf("expected permissions 0600, got %o", mode)
		}

		// Check content
		loaded, err := LoadGrants(path)
		if err != nil {
			t.Fatalf("LoadGrants failed: %v", err)
		}

		expected := map[string][]string{
			"agent1": {"fs.read", "shell.exec"}, // normalized
		}
		if !reflect.DeepEqual(loaded, expected) {
			t.Errorf("expected %v, got %v", expected, loaded)
		}
	})

	t.Run("EmptyPath", func(t *testing.T) {
		if err := SaveGrants("", nil); err != nil {
			t.Fatalf("SaveGrants failed: %v", err)
		}
	})
}

func TestGrantsRoundTrip(t *testing.T) {
	grants := map[string][]string{
		"agent1": {"fs.read", "fs.write", "bash.exec", "UNKNOWN.TOOL"},
		"agent2": {},
	}

	path := filepath.Join(t.TempDir(), "roundtrip.json")
	if err := SaveGrants(path, grants); err != nil {
		t.Fatalf("SaveGrants failed: %v", err)
	}

	loaded, err := LoadGrants(path)
	if err != nil {
		t.Fatalf("LoadGrants failed: %v", err)
	}

	expected := map[string][]string{
		"agent1": {"fs.read", "fs.write", "shell.exec", "unknown.tool"}, // sorted and normalized
		"agent2": {},
	}
	// normalize expected for sort order
	expected["agent1"] = NormalizeCapabilities(expected["agent1"])

	if !reflect.DeepEqual(loaded, expected) {
		t.Errorf("expected %v, got %v", expected, loaded)
	}
}
