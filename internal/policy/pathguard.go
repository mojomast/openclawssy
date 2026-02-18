package policy

import (
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
)

var protectedControlFiles = map[string]bool{
	"config.json": true,
	"master.key":  true,
	"secrets.enc": true,
	"SOUL.md":     true,
	"RULES.md":    true,
	"SPECPLAN.md": true,
}

func HasTraversal(targetPath string) bool {
	normalized := strings.TrimSpace(targetPath)
	if normalized == "" {
		return false
	}
	normalized = strings.ReplaceAll(normalized, "\\", "/")
	normalized = stripWindowsPathRoot(normalized)
	clean := pathpkg.Clean(normalized)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return true
	}
	parts := strings.Split(normalized, "/")
	for _, p := range parts {
		if p == ".." {
			return true
		}
	}
	return false
}

func stripWindowsPathRoot(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "//?/") || strings.HasPrefix(trimmed, "//./") {
		trimmed = trimmed[4:]
	}
	if hasWindowsDrivePrefix(trimmed) {
		trimmed = trimmed[2:]
	}
	if strings.HasPrefix(trimmed, "//") {
		rest := strings.TrimPrefix(trimmed, "//")
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) == 3 {
			return "/" + parts[2]
		}
		return "/"
	}
	return trimmed
}

func hasWindowsDrivePrefix(value string) bool {
	if len(value) < 2 {
		return false
	}
	ch := value[0]
	if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')) {
		return false
	}
	return value[1] == ':'
}

func isAbsoluteTarget(target string) bool {
	clean := strings.TrimSpace(target)
	if clean == "" {
		return false
	}
	if filepath.IsAbs(clean) {
		return true
	}
	normalized := strings.ReplaceAll(clean, "\\", "/")
	if hasWindowsDrivePrefix(normalized) && len(normalized) > 2 && normalized[2] == '/' {
		return true
	}
	if strings.HasPrefix(normalized, "//") {
		return true
	}
	return false
}

func isWindowsDriveRelative(target string) bool {
	normalized := strings.ReplaceAll(strings.TrimSpace(target), "\\", "/")
	if !hasWindowsDrivePrefix(normalized) {
		return false
	}
	return len(normalized) == 2 || normalized[2] != '/'
}

func (e *Enforcer) ResolveReadPath(workspace, target string) (string, error) {
	return resolvePath(workspace, target, false)
}

func (e *Enforcer) ResolveWritePath(workspace, target string) (string, error) {
	return resolvePath(workspace, target, true)
}

func resolvePath(workspace, target string, write bool) (string, error) {
	if workspace == "" {
		return "", fmt.Errorf("workspace is required")
	}
	if target == "" {
		return "", &PathError{Path: target, Reason: "empty path"}
	}
	if isWindowsDriveRelative(target) {
		return "", &PathError{Path: target, Reason: "invalid path"}
	}
	if HasTraversal(target) {
		return "", &PathError{Path: target, Reason: "path traversal"}
	}

	wsAbs, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	wsReal, err := filepath.EvalSymlinks(wsAbs)
	if err != nil {
		return "", err
	}

	targetAbs := target
	if !isAbsoluteTarget(targetAbs) {
		targetAbs = filepath.Join(wsReal, targetAbs)
	} else {
		targetAbs = strings.ReplaceAll(targetAbs, "\\", string(filepath.Separator))
	}
	targetAbs = filepath.Clean(targetAbs)

	var candidate string
	if write {
		parent := filepath.Dir(targetAbs)
		parentReal, err := filepath.EvalSymlinks(parent)
		if err != nil {
			return "", &PathError{Path: target, Reason: "write parent does not exist or is invalid"}
		}
		candidate = filepath.Join(parentReal, filepath.Base(targetAbs))

		// If the target file exists, resolve it to ensure it is not a symlink pointing outside
		if _, err := os.Lstat(candidate); err == nil {
			if realPath, err := filepath.EvalSymlinks(candidate); err == nil {
				candidate = realPath
			}
		}
	} else {
		candidate, err = filepath.EvalSymlinks(targetAbs)
		if err != nil {
			return "", &PathError{Path: target, Reason: "read path does not exist or is invalid"}
		}
	}

	if !isWithinWorkspace(wsReal, candidate) {
		return "", &PathError{Path: target, Reason: "outside workspace"}
	}

	if write && isProtectedControlPath(candidate) {
		return "", &PathError{Path: target, Reason: "protected control-plane path"}
	}

	return candidate, nil
}

func isProtectedControlPath(absPath string) bool {
	parts := strings.Split(filepath.ToSlash(absPath), "/")
	for i := 0; i < len(parts); i++ {
		if parts[i] != ".openclawssy" {
			continue
		}
		if i+1 >= len(parts) {
			return true
		}
		base := filepath.Base(absPath)
		if protectedControlFiles[base] {
			return true
		}
		if i+3 < len(parts) && parts[i+1] == "agents" {
			if protectedControlFiles[base] {
				return true
			}
		}
	}
	return false
}

func isWithinWorkspace(workspaceReal, pathReal string) bool {
	rel, err := filepath.Rel(workspaceReal, pathReal)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}

func EnsureWorkspace(path string) error {
	if path == "" {
		return fmt.Errorf("workspace path is empty")
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("workspace path is not a directory: %s", path)
	}
	return nil
}
