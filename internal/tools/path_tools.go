package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// resolveOpenClawssyPath resolves the absolute path to a file or directory within the .openclawssy control plane.
// It uses configuredPath if provided, otherwise it resolves relative to the workspace sibling directory.
func resolveOpenClawssyPath(workspace, configuredPath, purpose string, subpaths ...string) (string, error) {
	if strings.TrimSpace(configuredPath) != "" {
		return configuredPath, nil
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", fmt.Errorf("workspace is required to resolve %s path", purpose)
	}
	wsAbs, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	rootDir := filepath.Dir(wsAbs)
	parts := append([]string{rootDir, ".openclawssy"}, subpaths...)
	return filepath.Join(parts...), nil
}
