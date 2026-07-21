package store

import (
	"fmt"
	"os"

	"github.com/xdevplatform/xurl/errors"

	"gopkg.in/yaml.v3"
)

// ─── Chat key types ─────────────────────────────────────────────────

// ChatKeys holds one user's XChat private key material. PrivateKeysB64 is
// the base64 encoding of the chat-xdk ExportKeys blob (identity key, or
// identity||signing); KeyVersion is the version the matching public key is
// registered under on the account.
type ChatKeys struct {
	PrivateKeysB64 string `yaml:"private_keys_b64"`
	KeyVersion     string `yaml:"key_version"`
}

// ChatKeyStore persists XChat private keys per user id in a YAML file
// (~/.xurl/keys.yml by default), following the same conventions as the
// token store: 0600 permissions and $HOME resolution.
type ChatKeyStore struct {
	Users    map[string]*ChatKeys `yaml:"users"`
	filePath string
	// loadErr records a failed load of an existing file. It gates SaveKeys:
	// a corrupt key file must surface as an explicit error, never be
	// mistaken for "no keys stored" and silently overwritten.
	loadErr error
}

// NewChatKeyStore loads (or initializes) the chat key store at
// ~/.xurl/keys.yml.
func NewChatKeyStore() *ChatKeyStore {
	return NewChatKeyStoreWithPath(KeysFilePath())
}

// NewChatKeyStoreWithPath loads (or initializes) a chat key store at the given path.
func NewChatKeyStoreWithPath(path string) *ChatKeyStore {
	s := &ChatKeyStore{
		Users:    make(map[string]*ChatKeys),
		filePath: path,
	}
	s.loadErr = s.loadFromFile()
	return s
}

// FilePath returns the location of the backing file.
func (s *ChatKeyStore) FilePath() string {
	return s.filePath
}

// LoadErr reports whether an existing key file failed to load (e.g. corrupt
// YAML). Callers must treat this as fatal rather than as an empty store.
func (s *ChatKeyStore) LoadErr() error {
	return s.loadErr
}

// GetKeys returns the stored keys for a user id, or nil if absent.
func (s *ChatKeyStore) GetKeys(userID string) *ChatKeys {
	return s.Users[userID]
}

// SaveKeys stores keys for a user id and persists the store.
func (s *ChatKeyStore) SaveKeys(userID string, keys *ChatKeys) error {
	if s.loadErr != nil {
		return errors.NewTokenStoreError(fmt.Sprintf("refusing to overwrite %s, which exists but could not be loaded (fix or remove it first): %v", s.filePath, s.loadErr))
	}
	if userID == "" {
		return errors.NewTokenStoreError("cannot save chat keys without a user id")
	}
	if s.Users == nil {
		s.Users = make(map[string]*ChatKeys)
	}
	s.Users[userID] = keys
	return s.saveToFile()
}

// DeleteKeys removes keys for a user id and persists the store.
func (s *ChatKeyStore) DeleteKeys(userID string) error {
	if _, ok := s.Users[userID]; !ok {
		return nil
	}
	delete(s.Users, userID)
	return s.saveToFile()
}

// ─── Persistence ────────────────────────────────────────────────────

func (s *ChatKeyStore) loadFromFile() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.NewIOError(err)
	}
	var loaded ChatKeyStore
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		return errors.NewTokenStoreError(fmt.Sprintf("failed to parse chat key store %s: %v", s.filePath, err))
	}
	if loaded.Users != nil {
		s.Users = loaded.Users
	}
	return nil
}

func (s *ChatKeyStore) saveToFile() error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return errors.NewTokenStoreError(fmt.Sprintf("failed to serialize chat key store: %v", err))
	}
	// Write-then-rename so a crash mid-write can never leave a truncated
	// key file behind. Private key material: owner read/write only.
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return errors.NewIOError(err)
	}
	if err := os.Rename(tmp, s.filePath); err != nil {
		_ = os.Remove(tmp)
		return errors.NewIOError(err)
	}
	return nil
}
