package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAtomic_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")

	// Create a file where we want a directory to be
	// creating a file named "blocked"
	if err := os.WriteFile(blocked, []byte("block"), 0644); err != nil {
		t.Fatalf("failed to create blocking file: %v", err)
	}

	// Try to create a file inside "blocked", which is a file, not a directory.
	// WriteAtomic calls MkdirAll(dir) -> MkdirAll(".../blocked")
	// Since "blocked" exists and is a file, MkdirAll should fail (on Linux/Unix).
	// On some systems it might return a specific error.
	target := filepath.Join(blocked, "file")
	err := WriteAtomic(target, []byte("data"), 0644)
	if err == nil {
		t.Error("expected error when directory path is blocked by a file")
	}
}

func TestWriteAtomic_WriteError(t *testing.T) {
	dir := t.TempDir()

	// Create a read-only directory inside the temp dir
	targetDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(targetDir, 0500); err != nil {
		t.Fatalf("failed to create readonly dir: %v", err)
	}
	defer os.Chmod(targetDir, 0755) // Ensure cleanup works

	target := filepath.Join(targetDir, "file")
	err := WriteAtomic(target, []byte("data"), 0644)
	if err == nil {
		t.Error("expected error when writing to read-only directory")
	}
}

func TestWriteAtomic_BackupError(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "readonly_with_file")

	// Create directory with write permission first to add file
	if err := os.Mkdir(targetDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	target := filepath.Join(targetDir, "file")
	if err := os.WriteFile(target, []byte("original"), 0644); err != nil {
		t.Fatalf("failed to create original file: %v", err)
	}

	// Change directory to read-only so creating backup fails
	if err := os.Chmod(targetDir, 0500); err != nil {
		t.Fatalf("failed to make dir readonly: %v", err)
	}
	defer os.Chmod(targetDir, 0755) // Ensure cleanup works

	err := WriteAtomic(target, []byte("new"), 0644)
	if err == nil {
		t.Error("expected error when writing backup in read-only directory")
	} else if !strings.Contains(err.Error(), "write backup") {
		t.Errorf("expected 'write backup' error, got: %v", err)
	}
}
