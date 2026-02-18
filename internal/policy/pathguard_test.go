package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureWorkspace(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		setup   func(t *testing.T) string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
			errMsg:  "workspace path is empty",
		},
		{
			name: "valid directory",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
			wantErr: false,
		},
		{
			name: "non-existent path",
			setup: func(t *testing.T) string {
				// Use a path guaranteed not to exist
				return filepath.Join(t.TempDir(), "nonexistent")
			},
			wantErr: true,
			// The error from os.Stat is platform dependent but usually contains "no such file or directory"
			// or similar. We'll just check it's an error for now, as EnsureWorkspace just returns it.
		},
		{
			name: "path is a file",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				file := filepath.Join(dir, "testfile")
				if err := os.WriteFile(file, []byte("content"), 0644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
				return file
			},
			wantErr: true,
			errMsg:  "workspace path is not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path
			if tt.setup != nil {
				path = tt.setup(t)
			}
			err := EnsureWorkspace(path)
			if (err != nil) != tt.wantErr {
				t.Errorf("EnsureWorkspace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil {
					t.Errorf("EnsureWorkspace() expected error containing %q, got nil", tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("EnsureWorkspace() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}
