package config

import (
	"fmt"
	"os"
	"path/filepath"

	"openclawssy/internal/fsutil"
)

func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if old, err := os.ReadFile(path); err == nil {
		if err := fsutil.WriteFileAtomic(filepath.Join(dir, base+".bak"), old, mode); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	}

	if err := fsutil.WriteFileAtomic(path, data, mode); err != nil {
		return err
	}

	return nil
}
