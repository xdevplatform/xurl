package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xdevplatform/xurl/store"
)

func TestResolveRedirectURI(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xurl-config-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	t.Setenv("HOME", tempDir)

	ts := store.NewTokenStore()
	err = ts.AddApp("my-app", "id", "secret")
	require.NoError(t, err)
	err = ts.SetDefaultApp("my-app")
	require.NoError(t, err)
	err = ts.SetAppRedirectURI("my-app", "http://localhost:9090/callback")
	require.NoError(t, err)

	t.Run("store redirect uri is used when env is absent", func(t *testing.T) {
		t.Setenv("REDIRECT_URI", "")
		_ = os.Unsetenv("REDIRECT_URI")
		redirectURI, fromEnv, source := ResolveRedirectURI("my-app")
		assert.Equal(t, "http://localhost:9090/callback", redirectURI)
		assert.False(t, fromEnv)
		assert.Equal(t, "app config", source)
	})

	t.Run("env redirect uri overrides stored value", func(t *testing.T) {
		t.Setenv("REDIRECT_URI", "http://127.0.0.1:8080/callback")
		redirectURI, fromEnv, source := ResolveRedirectURI("my-app")
		assert.Equal(t, "http://127.0.0.1:8080/callback", redirectURI)
		assert.True(t, fromEnv)
		assert.Equal(t, "REDIRECT_URI environment variable", source)
	})

	t.Run("default redirect uri is used when nothing is configured", func(t *testing.T) {
		t.Setenv("HOME", filepath.Join(tempDir, "other-home"))
		require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "other-home"), 0o755))
		_ = os.Unsetenv("REDIRECT_URI")
		redirectURI, fromEnv, source := ResolveRedirectURI("")
		assert.Equal(t, DefaultRedirectURI, redirectURI)
		assert.False(t, fromEnv)
		assert.Equal(t, "built-in default", source)
	})
}

func TestNewConfigForAppUsesRedirectURIResolution(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xurl-config-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	t.Setenv("HOME", tempDir)
	ts := store.NewTokenStore()
	err = ts.AddApp("my-app", "id", "secret")
	require.NoError(t, err)
	err = ts.SetAppRedirectURI("my-app", "http://localhost:9090/callback")
	require.NoError(t, err)

	cfg := NewConfigForApp("my-app")
	assert.Equal(t, "http://localhost:9090/callback", cfg.RedirectURI)
	assert.False(t, cfg.RedirectURIFromEnv)

	t.Setenv("REDIRECT_URI", "http://127.0.0.1:8080/callback")
	cfg = NewConfigForApp("my-app")
	assert.Equal(t, "http://127.0.0.1:8080/callback", cfg.RedirectURI)
	assert.True(t, cfg.RedirectURIFromEnv)
}
