package sandbox

import (
	"context"
)

// SandboxFSAdapter adapts a Provider into a filesystem-like interface
// used internally for path resolution and file operations.
type SandboxFSAdapter struct {
	Provider Provider
}

// EvalSymlinks calls the provider's EvalSymlinks.
func (a *SandboxFSAdapter) EvalSymlinks(ctx context.Context, path string) (string, error) {
	return a.Provider.EvalSymlinks(ctx, path)
}

// Lstat calls the provider's Lstat.
func (a *SandboxFSAdapter) Lstat(ctx context.Context, path string) (FileInfo, bool, error) {
	return a.Provider.Lstat(ctx, path)
}
