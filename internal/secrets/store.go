package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"openclawssy/internal/config"
	"openclawssy/internal/fsutil"
)

const envMasterKey = "OPENCLAWSSY_MASTER_KEY"

type Store struct {
	path string
	key  []byte
	mu   sync.Mutex
}

type encryptedDoc struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	UpdatedAt  string `json:"updated_at"`
}

func NewStore(cfg config.Config) (*Store, error) {
	key, err := loadMasterKey(cfg.Secrets.MasterKeyFile)
	if err != nil {
		return nil, err
	}
	return &Store{path: cfg.Secrets.StoreFile, key: key}, nil
}

func (s *Store) Set(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.readAllLocked()
	if err != nil {
		return err
	}
	data[name] = value
	return s.writeAllLocked(data)
}

func (s *Store) Get(name string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readAllLocked()
	if err != nil {
		return "", false, err
	}
	v, ok := data[name]
	return v, ok, nil
}

func (s *Store) ListKeys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readAllLocked()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys, nil
}

func (s *Store) readAllLocked() (map[string]string, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return map[string]string{}, nil
	}
	var doc encryptedDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	plaintext, err := decrypt(s.key, doc.Nonce, doc.Ciphertext)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if len(plaintext) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(plaintext, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) writeAllLocked(data map[string]string) error {
	plaintext, err := json.Marshal(data)
	if err != nil {
		return err
	}
	nonce, ciphertext, err := encrypt(s.key, plaintext)
	if err != nil {
		return err
	}
	doc := encryptedDoc{Nonce: nonce, Ciphertext: ciphertext, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	// Ensure directory exists with restricted permissions before atomic write
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}

	return fsutil.WriteFileAtomic(s.path, out, 0o600)
}

func loadMasterKey(masterKeyFile string) ([]byte, error) {
	raw := os.Getenv(envMasterKey)
	if raw == "" {
		data, err := os.ReadFile(masterKeyFile)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("missing master key: set %s or create %s", envMasterKey, masterKeyFile)
			}
			return nil, err
		}
		raw = string(data)
	}
	raw = stringTrimSpace(raw)
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid master key encoding: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("master key must decode to 32 bytes")
	}
	return key, nil
}

func GenerateAndWriteMasterKey(path string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(b)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(encoded+"\n"), 0o600); err != nil {
		return "", err
	}
	return encoded, nil
}

func encrypt(key []byte, plaintext []byte) (nonce string, ciphertext string, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	n := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return "", "", err
	}
	ct := gcm.Seal(nil, n, plaintext, nil)
	return base64.StdEncoding.EncodeToString(n), base64.StdEncoding.EncodeToString(ct), nil
}

func decrypt(key []byte, nonce string, ciphertext string) ([]byte, error) {
	n, err := base64.StdEncoding.DecodeString(nonce)
	if err != nil {
		return nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, n, ct, nil)
}

func stringTrimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		l := len(s) - 1
		if s[l] == ' ' || s[l] == '\n' || s[l] == '\t' || s[l] == '\r' {
			s = s[:l]
			continue
		}
		break
	}
	return s
}
