package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to a file atomically by writing to a temporary file
// first and then renaming it. It ensures the directory exists and the file has
// the specified permissions.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("fsutil: ensure directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("fsutil: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Ensure we clean up the temp file if something goes wrong
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("fsutil: write temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("fsutil: sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("fsutil: close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("fsutil: chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("fsutil: rename temp file: %w", err)
	}

	// Sync the directory to ensure the rename is persisted.
	// We ignore errors here because the file is already renamed,
	// and syncing the directory is a best-effort durability guarantee.
	if f, err := os.Open(dir); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}

	return nil
}
