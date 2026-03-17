package instances

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type controlPlaneFeatureCompatibility struct {
	Eval bool `json:"eval"`
}

type controlPlaneCompatibilityStore struct {
	Features controlPlaneFeatureCompatibility `json:"features"`
}

func LoadControlPlaneFeatureSet(rootDir string) (FeatureSet, error) {
	features := DefaultFeatureSet()
	path := filepath.Join(rootDir, ".openclawssy", "controlplane", "instances.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return features, nil
		}
		return FeatureSet{}, fmt.Errorf("read control plane features: %w", err)
	}

	store := controlPlaneCompatibilityStore{}
	if err := json.Unmarshal(raw, &store); err != nil {
		return FeatureSet{}, fmt.Errorf("decode control plane features: %w", err)
	}

	features.Eval.Enabled = store.Features.Eval
	features.Eval.Visible = store.Features.Eval
	return features, nil
}

func EvalFeatureEnabled(rootDir string) (bool, error) {
	features, err := LoadControlPlaneFeatureSet(rootDir)
	if err != nil {
		return false, err
	}
	return features.Eval.Enabled, nil
}

func EvalFeatureDisabledError() error {
	return errors.New("eval is disabled for this control plane")
}

func IsEvalFeatureDisabledError(err error) bool {
	return err != nil && strings.EqualFold(strings.TrimSpace(err.Error()), EvalFeatureDisabledError().Error())
}
