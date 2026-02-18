package secrets

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openclawssy/internal/config"
)

func TestGenerateAndWriteMasterKey(t *testing.T) {
	tempDir := t.TempDir()
	keyFile := filepath.Join(tempDir, "master.key")

	key, err := GenerateAndWriteMasterKey(keyFile)
	if err != nil {
		t.Fatalf("GenerateAndWriteMasterKey failed: %v", err)
	}

	if key == "" {
		t.Error("GenerateAndWriteMasterKey returned empty key")
	}

	// Verify file content
	content, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("Failed to read key file: %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(string(content))
	if err != nil {
		// It might have a newline
		decoded, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(content)))
		if err != nil {
			t.Fatalf("Failed to decode key file content: %v", err)
		}
	}

	if len(decoded) != 32 {
		t.Errorf("Expected key length 32, got %d", len(decoded))
	}
}

func TestNewStore(t *testing.T) {
	t.Run("ValidKeyFile", func(t *testing.T) {
		tempDir := t.TempDir()
		keyFile := filepath.Join(tempDir, "master.key")
		storeFile := filepath.Join(tempDir, "secrets.enc")

		_, err := GenerateAndWriteMasterKey(keyFile)
		if err != nil {
			t.Fatalf("Failed to generate key: %v", err)
		}

		cfg := config.Config{}
		cfg.Secrets.MasterKeyFile = keyFile
		cfg.Secrets.StoreFile = storeFile

		store, err := NewStore(cfg)
		if err != nil {
			t.Fatalf("NewStore failed: %v", err)
		}
		if store == nil {
			t.Fatal("NewStore returned nil store")
		}
	})

	t.Run("ValidEnvVar", func(t *testing.T) {
		tempDir := t.TempDir()
		storeFile := filepath.Join(tempDir, "secrets.enc")

		// Generate a key but don't write it to file (or point to a non-existent file)
		b := make([]byte, 32)
		// fill with some data
		for i := range b {
			b[i] = byte(i)
		}
		encodedKey := base64.StdEncoding.EncodeToString(b)

		t.Setenv("OPENCLAWSSY_MASTER_KEY", encodedKey)

		cfg := config.Config{}
		cfg.Secrets.MasterKeyFile = filepath.Join(tempDir, "non-existent.key")
		cfg.Secrets.StoreFile = storeFile

		store, err := NewStore(cfg)
		if err != nil {
			t.Fatalf("NewStore with env var failed: %v", err)
		}
		if store == nil {
			t.Fatal("NewStore returned nil store")
		}
	})

	t.Run("MissingKey", func(t *testing.T) {
		tempDir := t.TempDir()
		keyFile := filepath.Join(tempDir, "missing.key")
		storeFile := filepath.Join(tempDir, "secrets.enc")

		// Ensure env var is unset
		t.Setenv("OPENCLAWSSY_MASTER_KEY", "")

		cfg := config.Config{}
		cfg.Secrets.MasterKeyFile = keyFile
		cfg.Secrets.StoreFile = storeFile

		_, err := NewStore(cfg)
		if err == nil {
			t.Fatal("NewStore should fail when key is missing")
		}
	})

	t.Run("InvalidKeyLength", func(t *testing.T) {
		tempDir := t.TempDir()
		keyFile := filepath.Join(tempDir, "invalid.key")
		storeFile := filepath.Join(tempDir, "secrets.enc")

		// Create a key that is too short
		invalidKey := base64.StdEncoding.EncodeToString([]byte("short"))
		if err := os.WriteFile(keyFile, []byte(invalidKey), 0600); err != nil {
			t.Fatalf("Failed to write invalid key: %v", err)
		}

		t.Setenv("OPENCLAWSSY_MASTER_KEY", "")

		cfg := config.Config{}
		cfg.Secrets.MasterKeyFile = keyFile
		cfg.Secrets.StoreFile = storeFile

		_, err := NewStore(cfg)
		if err == nil {
			t.Fatal("NewStore should fail with invalid key length")
		}
	})
}

func TestStore_SetGet(t *testing.T) {
	tempDir := t.TempDir()
	keyFile := filepath.Join(tempDir, "master.key")
	storeFile := filepath.Join(tempDir, "secrets.enc")

	_, err := GenerateAndWriteMasterKey(keyFile)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	cfg := config.Config{}
	cfg.Secrets.MasterKeyFile = keyFile
	cfg.Secrets.StoreFile = storeFile

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Test Set
	key := "test_key"
	value := "test_value"
	if err := store.Set(key, value); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Test Get
	got, ok, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok {
		t.Error("Get returned !ok for existing key")
	}
	if got != value {
		t.Errorf("Get returned %q, want %q", got, value)
	}

	// Test Get Missing
	_, ok, err = store.Get("missing_key")
	if err != nil {
		t.Fatalf("Get missing key failed: %v", err)
	}
	if ok {
		t.Error("Get returned ok for missing key")
	}
}

func TestStore_Persistence(t *testing.T) {
	tempDir := t.TempDir()
	keyFile := filepath.Join(tempDir, "master.key")
	storeFile := filepath.Join(tempDir, "secrets.enc")

	_, err := GenerateAndWriteMasterKey(keyFile)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	cfg := config.Config{}
	cfg.Secrets.MasterKeyFile = keyFile
	cfg.Secrets.StoreFile = storeFile

	// Store 1: Write
	store1, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore 1 failed: %v", err)
	}

	key := "persist_key"
	value := "persist_value"
	if err := store1.Set(key, value); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Store 2: Read
	store2, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore 2 failed: %v", err)
	}

	got, ok, err := store2.Get(key)
	if err != nil {
		t.Fatalf("Get failed on new store instance: %v", err)
	}
	if !ok {
		t.Error("Get returned !ok for persisted key")
	}
	if got != value {
		t.Errorf("Get returned %q, want %q", got, value)
	}
}

func TestStore_ListKeys(t *testing.T) {
	tempDir := t.TempDir()
	keyFile := filepath.Join(tempDir, "master.key")
	storeFile := filepath.Join(tempDir, "secrets.enc")

	_, err := GenerateAndWriteMasterKey(keyFile)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	cfg := config.Config{}
	cfg.Secrets.MasterKeyFile = keyFile
	cfg.Secrets.StoreFile = storeFile

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	keysToSet := []string{"k1", "k2", "k3"}
	for _, k := range keysToSet {
		if err := store.Set(k, "v"); err != nil {
			t.Fatalf("Set failed for %s: %v", k, err)
		}
	}

	keys, err := store.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys failed: %v", err)
	}

	if len(keys) != len(keysToSet) {
		t.Errorf("ListKeys returned %d keys, want %d", len(keys), len(keysToSet))
	}

	// Verify all keys are present
	keyMap := make(map[string]bool)
	for _, k := range keys {
		keyMap[k] = true
	}
	for _, k := range keysToSet {
		if !keyMap[k] {
			t.Errorf("ListKeys missing key %s", k)
		}
	}
}

func TestStore_Encryption(t *testing.T) {
	tempDir := t.TempDir()
	keyFile := filepath.Join(tempDir, "master.key")
	storeFile := filepath.Join(tempDir, "secrets.enc")

	_, err := GenerateAndWriteMasterKey(keyFile)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	cfg := config.Config{}
	cfg.Secrets.MasterKeyFile = keyFile
	cfg.Secrets.StoreFile = storeFile

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	key := "secret_key"
	value := "super_secret_value"
	if err := store.Set(key, value); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Read file content directly
	content, err := os.ReadFile(storeFile)
	if err != nil {
		t.Fatalf("Failed to read store file: %v", err)
	}

	// Verify it is NOT plaintext
	if strings.Contains(string(content), value) {
		t.Error("Store file contains plaintext value")
	}

	// Verify JSON structure
	var doc struct {
		Nonce      string `json:"nonce"`
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		t.Fatalf("Failed to unmarshal store file: %v", err)
	}

	if doc.Nonce == "" {
		t.Error("Nonce is empty")
	}
	if doc.Ciphertext == "" {
		t.Error("Ciphertext is empty")
	}
}

func TestStore_CorruptedFile(t *testing.T) {
	tempDir := t.TempDir()
	keyFile := filepath.Join(tempDir, "master.key")
	storeFile := filepath.Join(tempDir, "secrets.enc")

	_, err := GenerateAndWriteMasterKey(keyFile)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	cfg := config.Config{}
	cfg.Secrets.MasterKeyFile = keyFile
	cfg.Secrets.StoreFile = storeFile

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Case 1: Invalid JSON
	if err := os.WriteFile(storeFile, []byte("invalid json"), 0600); err != nil {
		t.Fatalf("Failed to write corrupted file: %v", err)
	}

	_, _, err = store.Get("any")
	if err == nil {
		t.Error("Get should fail with invalid JSON")
	}

	// Case 2: Invalid Base64 Ciphertext
	doc := map[string]string{
		"nonce":      "valid_nonce",
		"ciphertext": "invalid_base64!",
	}
	b, _ := json.Marshal(doc)
	if err := os.WriteFile(storeFile, b, 0600); err != nil {
		t.Fatalf("Failed to write corrupted file: %v", err)
	}

	_, _, err = store.Get("any")
	if err == nil {
		t.Error("Get should fail with invalid ciphertext")
	}
}

func TestStore_EmptyFile(t *testing.T) {
	tempDir := t.TempDir()
	keyFile := filepath.Join(tempDir, "master.key")
	storeFile := filepath.Join(tempDir, "secrets.enc")

	_, err := GenerateAndWriteMasterKey(keyFile)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	cfg := config.Config{}
	cfg.Secrets.MasterKeyFile = keyFile
	cfg.Secrets.StoreFile = storeFile

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Create empty file
	if err := os.WriteFile(storeFile, []byte{}, 0600); err != nil {
		t.Fatalf("Failed to write empty file: %v", err)
	}

	// Should not fail, just return empty
	keys, err := store.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys failed on empty file: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("Expected 0 keys from empty file, got %d", len(keys))
	}
}
