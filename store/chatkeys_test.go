package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempChatKeyStore(t *testing.T) *ChatKeyStore {
	t.Helper()
	return NewChatKeyStoreWithPath(filepath.Join(t.TempDir(), "chat-keys.yaml"))
}

func TestChatKeyStoreRoundTrip(t *testing.T) {
	s := tempChatKeyStore(t)

	assert.Nil(t, s.GetKeys("42"))

	keys := &ChatKeys{
		PrivateKeysB64: "c2VjcmV0LWtleS1tYXRlcmlhbA==",
		KeyVersion:     "1700000000",
	}
	require.NoError(t, s.SaveKeys("42", keys))

	// Reload from disk into a fresh store.
	reloaded := NewChatKeyStoreWithPath(s.FilePath())
	got := reloaded.GetKeys("42")
	require.NotNil(t, got)
	assert.Equal(t, keys.PrivateKeysB64, got.PrivateKeysB64)
	assert.Equal(t, keys.KeyVersion, got.KeyVersion)
}

func TestChatKeyStoreFilePermissions(t *testing.T) {
	s := tempChatKeyStore(t)
	require.NoError(t, s.SaveKeys("42", &ChatKeys{PrivateKeysB64: "cw=="}))

	info, err := os.Stat(s.FilePath())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestChatKeyStoreMultipleUsers(t *testing.T) {
	s := tempChatKeyStore(t)
	require.NoError(t, s.SaveKeys("1", &ChatKeys{PrivateKeysB64: "YQ==", KeyVersion: "1"}))
	require.NoError(t, s.SaveKeys("2", &ChatKeys{PrivateKeysB64: "Yg==", KeyVersion: "2"}))

	reloaded := NewChatKeyStoreWithPath(s.FilePath())
	assert.Equal(t, "YQ==", reloaded.GetKeys("1").PrivateKeysB64)
	assert.Equal(t, "Yg==", reloaded.GetKeys("2").PrivateKeysB64)
}

func TestChatKeyStoreDelete(t *testing.T) {
	s := tempChatKeyStore(t)
	require.NoError(t, s.SaveKeys("1", &ChatKeys{PrivateKeysB64: "YQ=="}))
	require.NoError(t, s.DeleteKeys("1"))
	// Deleting a missing user is a no-op.
	require.NoError(t, s.DeleteKeys("missing"))

	reloaded := NewChatKeyStoreWithPath(s.FilePath())
	assert.Nil(t, reloaded.GetKeys("1"))
}

func TestChatKeyStoreRejectsEmptyUserID(t *testing.T) {
	s := tempChatKeyStore(t)
	assert.Error(t, s.SaveKeys("", &ChatKeys{PrivateKeysB64: "YQ=="}))
}

func TestChatKeyStoreMissingFileIsEmpty(t *testing.T) {
	s := NewChatKeyStoreWithPath(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	assert.Empty(t, s.Users)
	assert.NoError(t, s.LoadErr())
}

func TestChatKeyStoreCorruptFileRefusesSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.yml")
	corrupt := "users: {invalid yaml ["
	require.NoError(t, os.WriteFile(path, []byte(corrupt), 0600))

	s := NewChatKeyStoreWithPath(path)
	require.Error(t, s.LoadErr(), "a corrupt file must not look like an empty store")

	// Saving must refuse rather than overwrite whatever remains of the file.
	err := s.SaveKeys("42", &ChatKeys{PrivateKeysB64: "YQ=="})
	require.Error(t, err)
	data, rerr := os.ReadFile(path)
	require.NoError(t, rerr)
	assert.Equal(t, corrupt, string(data), "corrupt file must be left untouched")
}
