package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveStoreDirFreshHome(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	assert.Equal(t, filepath.Join(tempDir, ".xurl", "auth.yml"), AuthFilePath())
	assert.Equal(t, filepath.Join(tempDir, ".xurl", "keys.yml"), KeysFilePath())

	info, err := os.Stat(filepath.Join(tempDir, ".xurl"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, os.FileMode(0700), info.Mode().Perm())
}

func TestResolveStoreDirMigratesLegacyTokenFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	legacy := filepath.Join(tempDir, ".xurl")
	content := "apps:\n  my-app:\n    client_id: cid\n"
	require.NoError(t, os.WriteFile(legacy, []byte(content), 0600))

	authPath := AuthFilePath()
	assert.Equal(t, filepath.Join(legacy, "auth.yml"), authPath)

	// The legacy file's bytes moved into the directory untouched.
	migrated, err := os.ReadFile(authPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(migrated))

	info, err := os.Stat(legacy)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// The token store loads the migrated file.
	ts := NewTokenStore()
	assert.Equal(t, authPath, ts.FilePath)
	require.NotNil(t, ts.Apps["my-app"])
	assert.Equal(t, "cid", ts.Apps["my-app"].ClientID)
}

func TestKeysFilePathAlongsideMigratedTokenFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	// A legacy token file migrates into the directory; the chat-key store
	// then lands next to it.
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, ".xurl"), []byte("apps: {}\n"), 0600))
	assert.Equal(t, filepath.Join(tempDir, ".xurl", "keys.yml"), KeysFilePath())

	cs := NewChatKeyStore()
	require.NoError(t, cs.SaveKeys("42", &ChatKeys{PrivateKeysB64: "c2VjcmV0", KeyVersion: "7"}))
	reloaded := NewChatKeyStore()
	require.NotNil(t, reloaded.GetKeys("42"))
	assert.Equal(t, "7", reloaded.GetKeys("42").KeyVersion)
}

func TestResolveStoreDirRecoversInterruptedMigration(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	// A crash between the migration's two renames leaves the legacy file at
	// the temp path and no ~/.xurl at all.
	content := "apps:\n  my-app:\n    client_id: cid\n"
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, ".xurl.migrating"), []byte(content), 0600))

	authPath := AuthFilePath()
	assert.Equal(t, filepath.Join(tempDir, ".xurl", "auth.yml"), authPath)
	migrated, err := os.ReadFile(authPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(migrated))
	_, err = os.Stat(filepath.Join(tempDir, ".xurl.migrating"))
	assert.True(t, os.IsNotExist(err), "temp file must be consumed by the recovery")
}

func TestResolveStoreDirIdempotent(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	first := resolveStoreDir()
	require.NoError(t, os.WriteFile(filepath.Join(first, "auth.yml"), []byte("apps: {}\n"), 0600))
	second := resolveStoreDir()
	assert.Equal(t, first, second)

	data, err := os.ReadFile(filepath.Join(first, "auth.yml"))
	require.NoError(t, err)
	assert.Equal(t, "apps: {}\n", string(data))
}
