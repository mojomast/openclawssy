package instances

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadControlPlaneFeatureSetDefaultsWhenStoreMissing(t *testing.T) {
	root := t.TempDir()
	features, err := LoadControlPlaneFeatureSet(root)
	if err != nil {
		t.Fatalf("LoadControlPlaneFeatureSet() error = %v", err)
	}
	if !features.Eval.Enabled || !features.Eval.Visible {
		t.Fatalf("expected eval enabled by default, got %+v", features.Eval)
	}
}

func TestLoadControlPlaneFeatureSetReadsEvalCompatibilityFlag(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".openclawssy", "controlplane")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir control plane: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "instances.json"), []byte("{\n  \"features\": {\n    \"eval\": false\n  }\n}\n"), 0o644); err != nil {
		t.Fatalf("write compatibility store: %v", err)
	}

	features, err := LoadControlPlaneFeatureSet(root)
	if err != nil {
		t.Fatalf("LoadControlPlaneFeatureSet() error = %v", err)
	}
	if features.Eval.Enabled || features.Eval.Visible {
		t.Fatalf("expected eval disabled from compatibility store, got %+v", features.Eval)
	}
}
