package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	data := []byte("hello world")
	perm := os.FileMode(0o600)

	err := WriteFileAtomic(path, data, perm)
	if err != nil {
		t.Fatalf("WriteFileAtomic failed: %v", err)
	}

	// Verify content
	readData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(readData) != string(data) {
		t.Errorf("expected content %q, got %q", string(data), string(readData))
	}

	// Verify permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != perm {
		t.Errorf("expected permissions %o, got %o", perm, info.Mode().Perm())
	}

	// Verify overwrite
	newData := []byte("new data")
	err = WriteFileAtomic(path, newData, perm)
	if err != nil {
		t.Fatalf("WriteFileAtomic overwrite failed: %v", err)
	}
	readData, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(readData) != string(newData) {
		t.Errorf("expected content %q, got %q", string(newData), string(readData))
	}
}

func TestWriteFileAtomic_NewDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "subdir", "test.txt")
	data := []byte("hello world")
	perm := os.FileMode(0o644)

	err := WriteFileAtomic(path, data, perm)
	if err != nil {
		t.Fatalf("WriteFileAtomic failed: %v", err)
	}

	readData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(readData) != string(data) {
		t.Errorf("expected content %q, got %q", string(data), string(readData))
	}
}
