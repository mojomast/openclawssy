package tools

import (
	"context"
	"os"
)

// SandboxFileInfo describes a filesystem entry from a sandbox provider.
// Defined locally to avoid an import cycle (tools → sandbox → … → tools).
type SandboxFileInfo struct {
	Name  string
	IsDir bool
	Size  int64
}

// SandboxFileOps abstracts file I/O for the active sandbox.
// When nil, fs.* tools use the host OS directly (local / none providers).
// When non-nil (docker provider), all file operations are routed through the
// container so the sandbox can intercept them.
//
// sandbox.Provider satisfies this interface via the adapter in engine.go.
type SandboxFileOps interface {
	ReadFile(ctx context.Context, path string) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte, perm os.FileMode) error
	ListDir(ctx context.Context, path string) ([]SandboxFileInfo, error)
	MkdirAll(ctx context.Context, path string, perm os.FileMode) error
	Remove(ctx context.Context, path string, recursive bool) error
	Rename(ctx context.Context, src, dst string) error
	Lstat(ctx context.Context, path string) (SandboxFileInfo, bool, error)
	EvalSymlinks(ctx context.Context, path string) (string, error)
}
