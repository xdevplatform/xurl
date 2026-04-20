package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTempTokenStore(t *testing.T) (*TokenStore, string) {
	tempDir, err := os.MkdirTemp("", "xurl_test")
	require.NoError(t, err, "Failed to create temp directory")

	tempFile := filepath.Join(tempDir, ".xurl")
	store := &TokenStore{
		Apps:       make(map[string]*App),
		DefaultApp: "default",
		FilePath:   tempFile,
	}
	store.Apps["default"] = &App{
		OAuth2Tokens: make(map[string]Token),
	}

	return store, tempDir
}

func TestNewTokenStore(t *testing.T) {
	store := NewTokenStore()

	assert.NotNil(t, store, "Expected non-nil TokenStore")
	assert.NotNil(t, store.Apps, "Expected non-nil Apps map")
	assert.NotEmpty(t, store.FilePath, "Expected non-empty FilePath")
}

func TestTokenOperations(t *testing.T) {
	store, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	t.Run("Bearer Token", func(t *testing.T) {
		err := store.SaveBearerToken("test-bearer-token")
		require.NoError(t, err, "Failed to save bearer token")

		token := store.GetBearerToken()
		require.NotNil(t, token, "Expected non-nil token")

		assert.Equal(t, BearerTokenType, token.Type, "Unexpected token type")
		assert.Equal(t, "test-bearer-token", token.Bearer, "Unexpected bearer token value")
		assert.True(t, store.HasBearerToken(), "Expected HasBearerToken to return true")

		err = store.ClearBearerToken()
		require.NoError(t, err, "Failed to clear bearer token")

		assert.False(t, store.HasBearerToken(), "Expected HasBearerToken to return false after clearing")
	})

	// Test OAuth2 Token operations
	t.Run("OAuth2 Token", func(t *testing.T) {
		err := store.SaveOAuth2Token("testuser", "access-token", "refresh-token", 1234567890)
		require.NoError(t, err, "Failed to save OAuth2 token")

		token := store.GetOAuth2Token("testuser")
		require.NotNil(t, token, "Expected non-nil token")

		assert.Equal(t, OAuth2TokenType, token.Type, "Unexpected token type")
		require.NotNil(t, token.OAuth2, "Expected non-nil OAuth2 token")
		assert.Equal(t, "access-token", token.OAuth2.AccessToken, "Unexpected access token")
		assert.Equal(t, "refresh-token", token.OAuth2.RefreshToken, "Unexpected refresh token")
		assert.Equal(t, uint64(1234567890), token.OAuth2.ExpirationTime, "Unexpected expiration time")

		usernames := store.GetOAuth2Usernames()
		assert.Equal(t, []string{"testuser"}, usernames, "Unexpected usernames")

		firstToken := store.GetFirstOAuth2Token()
		assert.NotNil(t, firstToken, "Expected non-nil first token")

		err = store.ClearOAuth2Token("testuser")
		require.NoError(t, err, "Failed to clear OAuth2 token")

		assert.Nil(t, store.GetOAuth2Token("testuser"), "Expected nil token after clearing")
	})

	// Test OAuth1 Token operations
	t.Run("OAuth1 Tokens", func(t *testing.T) {
		err := store.SaveOAuth1Tokens("access-token", "token-secret", "consumer-key", "consumer-secret")
		require.NoError(t, err, "Failed to save OAuth1 tokens")

		token := store.GetOAuth1Tokens()
		require.NotNil(t, token, "Expected non-nil token")

		assert.Equal(t, OAuth1TokenType, token.Type, "Unexpected token type")
		require.NotNil(t, token.OAuth1, "Expected non-nil OAuth1 token")
		assert.Equal(t, "access-token", token.OAuth1.AccessToken, "Unexpected access token")
		assert.Equal(t, "token-secret", token.OAuth1.TokenSecret, "Unexpected token secret")
		assert.Equal(t, "consumer-key", token.OAuth1.ConsumerKey, "Unexpected consumer key")
		assert.Equal(t, "consumer-secret", token.OAuth1.ConsumerSecret, "Unexpected consumer secret")

		assert.True(t, store.HasOAuth1Tokens(), "Expected HasOAuth1Tokens to return true")

		err = store.ClearOAuth1Tokens()
		require.NoError(t, err, "Failed to clear OAuth1 tokens")

		assert.False(t, store.HasOAuth1Tokens(), "Expected HasOAuth1Tokens to return false after clearing")
	})
}

func TestClearAll(t *testing.T) {
	store, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	// Save all types of tokens
	store.SaveBearerToken("bearer-token")
	store.SaveOAuth2Token("testuser", "access-token", "refresh-token", 1234567890)
	store.SaveOAuth1Tokens("access-token", "token-secret", "consumer-key", "consumer-secret")

	// Test clearing all tokens
	err := store.ClearAll()
	require.NoError(t, err, "Failed to clear all tokens")

	assert.False(t, store.HasBearerToken(), "Expected HasBearerToken to return false after clearing all")
	assert.False(t, store.HasOAuth1Tokens(), "Expected HasOAuth1Tokens to return false after clearing all")
	assert.Empty(t, store.GetOAuth2Usernames(), "Expected empty OAuth2 usernames after clearing all")
}

func TestMultiApp(t *testing.T) {
	store, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	t.Run("Add and list apps", func(t *testing.T) {
		err := store.AddApp("app1", "id1", "secret1")
		require.NoError(t, err)
		err = store.AddApp("app2", "id2", "secret2")
		require.NoError(t, err)

		names := store.ListApps()
		assert.Contains(t, names, "app1")
		assert.Contains(t, names, "app2")
		assert.Contains(t, names, "default")
	})

	t.Run("Duplicate app name rejected", func(t *testing.T) {
		err := store.AddApp("app1", "x", "y")
		assert.Error(t, err)
	})

	t.Run("Set and get default app", func(t *testing.T) {
		err := store.SetDefaultApp("app1")
		require.NoError(t, err)
		assert.Equal(t, "app1", store.GetDefaultApp())
	})

	t.Run("Per-app tokens are isolated", func(t *testing.T) {
		store.SetDefaultApp("app1")
		err := store.SaveOAuth2Token("alice", "a-tok", "a-ref", 111)
		require.NoError(t, err)

		store.SetDefaultApp("app2")
		err = store.SaveOAuth2Token("bob", "b-tok", "b-ref", 222)
		require.NoError(t, err)

		// app1 should only have alice
		assert.Equal(t, []string{"alice"}, store.GetOAuth2UsernamesForApp("app1"))
		// app2 should only have bob
		assert.Equal(t, []string{"bob"}, store.GetOAuth2UsernamesForApp("app2"))
	})

	t.Run("Per-app redirect uri is isolated", func(t *testing.T) {
		err := store.SetAppRedirectURI("app1", "http://localhost:8080/callback")
		require.NoError(t, err)
		err = store.SetAppRedirectURI("app2", "http://localhost:9090/callback")
		require.NoError(t, err)

		redirectURI1, err := store.GetAppRedirectURI("app1")
		require.NoError(t, err)
		redirectURI2, err := store.GetAppRedirectURI("app2")
		require.NoError(t, err)

		assert.Equal(t, "http://localhost:8080/callback", redirectURI1)
		assert.Equal(t, "http://localhost:9090/callback", redirectURI2)
	})

	t.Run("Remove app", func(t *testing.T) {
		err := store.RemoveApp("app2")
		require.NoError(t, err)
		assert.NotContains(t, store.ListApps(), "app2")
	})

	t.Run("Remove nonexistent app fails", func(t *testing.T) {
		err := store.RemoveApp("nope")
		assert.Error(t, err)
	})

	t.Run("GetApp returns correct app", func(t *testing.T) {
		app := store.GetApp("app1")
		require.NotNil(t, app)
		assert.Equal(t, "id1", app.ClientID)
		assert.Equal(t, "secret1", app.ClientSecret)
	})

	t.Run("Default user", func(t *testing.T) {
		// app1 already has alice from earlier
		store.SetDefaultApp("app1")

		// Setting default user to a user that doesn't exist should fail
		err := store.SetDefaultUser("app1", "nobody")
		assert.Error(t, err)

		// Setting default user to an existing user should work
		err = store.SetDefaultUser("app1", "alice")
		require.NoError(t, err)
		assert.Equal(t, "alice", store.GetDefaultUser("app1"))

		// GetFirstOAuth2Token should now return alice's token
		tok := store.GetFirstOAuth2Token()
		require.NotNil(t, tok)
		assert.Equal(t, "a-tok", tok.OAuth2.AccessToken)
	})

	t.Run("Default user persists via GetFirstOAuth2Token", func(t *testing.T) {
		// Add a second user to app1
		store.SetDefaultApp("app1")
		store.SaveOAuth2TokenForApp("app1", "zara", "z-tok", "z-ref", 333)

		// Default is still alice
		tok := store.GetFirstOAuth2Token()
		require.NotNil(t, tok)
		assert.Equal(t, "a-tok", tok.OAuth2.AccessToken)

		// Switch default to zara
		err := store.SetDefaultUser("app1", "zara")
		require.NoError(t, err)

		tok = store.GetFirstOAuth2Token()
		require.NotNil(t, tok)
		assert.Equal(t, "z-tok", tok.OAuth2.AccessToken)
	})

	t.Run("Unnamed tokens are lower priority than named tokens", func(t *testing.T) {
		store.SetDefaultApp("app1")
		err := store.SaveOAuth2TokenForApp("app1", "", "fallback-tok", "fallback-ref", 444)
		require.NoError(t, err)

		username, tok := store.GetFirstOAuth2TokenRecordForApp("app1")
		require.NotNil(t, tok)
		assert.NotEmpty(t, username)
		assert.NotEqual(t, "fallback-tok", tok.OAuth2.AccessToken)
	})
}

func TestUpdateApp(t *testing.T) {
	store, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	store.AddApp("myapp", "old-id", "old-secret")

	t.Run("Update both fields", func(t *testing.T) {
		err := store.UpdateApp("myapp", "new-id", "new-secret")
		require.NoError(t, err)
		app := store.GetApp("myapp")
		assert.Equal(t, "new-id", app.ClientID)
		assert.Equal(t, "new-secret", app.ClientSecret)
	})

	t.Run("Update only client ID", func(t *testing.T) {
		err := store.UpdateApp("myapp", "newer-id", "")
		require.NoError(t, err)
		app := store.GetApp("myapp")
		assert.Equal(t, "newer-id", app.ClientID)
		assert.Equal(t, "new-secret", app.ClientSecret) // unchanged
	})

	t.Run("Update only client secret", func(t *testing.T) {
		err := store.UpdateApp("myapp", "", "newer-secret")
		require.NoError(t, err)
		app := store.GetApp("myapp")
		assert.Equal(t, "newer-id", app.ClientID) // unchanged
		assert.Equal(t, "newer-secret", app.ClientSecret)
	})

	t.Run("Set and get redirect URI", func(t *testing.T) {
		err := store.SetAppRedirectURI("myapp", "http://localhost:8080/callback")
		require.NoError(t, err)
		redirectURI, err := store.GetAppRedirectURI("myapp")
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:8080/callback", redirectURI)
	})

	t.Run("Update nonexistent app fails", func(t *testing.T) {
		err := store.UpdateApp("nope", "x", "y")
		assert.Error(t, err)
	})

	t.Run("Get redirect URI for nonexistent app fails", func(t *testing.T) {
		_, err := store.GetAppRedirectURI("nope")
		assert.Error(t, err)
	})
}

func TestCredentialBackfill(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xurl-backfill-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	t.Setenv("HOME", tempDir)
	xurlPath := filepath.Join(tempDir, ".xurl")

	// Write a legacy JSON file (no credentials stored)
	legacy := map[string]interface{}{
		"oauth2_tokens": map[string]interface{}{
			"user1": map[string]interface{}{
				"type": "oauth2",
				"oauth2": map[string]interface{}{
					"access_token":    "at",
					"refresh_token":   "rt",
					"expiration_time": 9999,
				},
			},
		},
	}
	data, _ := json.MarshalIndent(legacy, "", "  ")
	os.WriteFile(xurlPath, data, 0600)

	// First load without credentials — migration happens, no backfill
	s1 := NewTokenStore()
	app1 := s1.GetApp("default")
	require.NotNil(t, app1)
	assert.Empty(t, app1.ClientID, "Should have no client ID without backfill")

	// Now load WITH credentials — should backfill the migrated app
	s2 := NewTokenStoreWithCredentials("env-id", "env-secret")
	app2 := s2.GetApp("default")
	require.NotNil(t, app2)
	assert.Equal(t, "env-id", app2.ClientID, "Should have backfilled client ID")
	assert.Equal(t, "env-secret", app2.ClientSecret, "Should have backfilled client secret")

	// App that already has credentials should NOT be overwritten
	s2.AddApp("has-creds", "existing-id", "existing-secret")
	s2.SaveOAuth2TokenForApp("has-creds", "u", "t", "r", 1)
	s3 := NewTokenStoreWithCredentials("other-id", "other-secret")
	app3 := s3.GetApp("has-creds")
	assert.Equal(t, "existing-id", app3.ClientID, "Should not overwrite existing credentials")
}

func TestForAppVariants(t *testing.T) {
	store, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	store.AddApp("a1", "id1", "s1")
	store.AddApp("a2", "id2", "s2")

	t.Run("SaveBearerTokenForApp", func(t *testing.T) {
		err := store.SaveBearerTokenForApp("a1", "bearer-a1")
		require.NoError(t, err)
		tok := store.GetBearerTokenForApp("a1")
		require.NotNil(t, tok)
		assert.Equal(t, "bearer-a1", tok.Bearer)
		// a2 should not have it
		assert.Nil(t, store.GetBearerTokenForApp("a2"))
	})

	t.Run("SaveOAuth1TokensForApp", func(t *testing.T) {
		err := store.SaveOAuth1TokensForApp("a2", "at", "ts", "ck", "cs")
		require.NoError(t, err)
		tok := store.GetOAuth1TokensForApp("a2")
		require.NotNil(t, tok)
		assert.Equal(t, "at", tok.OAuth1.AccessToken)
		// a1 should not have it
		assert.Nil(t, store.GetOAuth1TokensForApp("a1"))
	})

	t.Run("SaveOAuth2TokenForApp", func(t *testing.T) {
		err := store.SaveOAuth2TokenForApp("a1", "user1", "at1", "rt1", 100)
		require.NoError(t, err)
		tok := store.GetOAuth2TokenForApp("a1", "user1")
		require.NotNil(t, tok)
		assert.Equal(t, "at1", tok.OAuth2.AccessToken)
		// a2 should not have it
		assert.Nil(t, store.GetOAuth2TokenForApp("a2", "user1"))
	})

	t.Run("ClearOAuth2TokenForApp", func(t *testing.T) {
		store.SaveOAuth2TokenForApp("a1", "temp", "t", "r", 1)
		err := store.ClearOAuth2TokenForApp("a1", "temp")
		require.NoError(t, err)
		assert.Nil(t, store.GetOAuth2TokenForApp("a1", "temp"))
	})

	t.Run("ClearOAuth1TokensForApp", func(t *testing.T) {
		err := store.ClearOAuth1TokensForApp("a2")
		require.NoError(t, err)
		assert.Nil(t, store.GetOAuth1TokensForApp("a2"))
	})

	t.Run("ClearBearerTokenForApp", func(t *testing.T) {
		err := store.ClearBearerTokenForApp("a1")
		require.NoError(t, err)
		assert.Nil(t, store.GetBearerTokenForApp("a1"))
	})

	t.Run("ClearAllForApp", func(t *testing.T) {
		store.SaveOAuth2TokenForApp("a1", "x", "t", "r", 1)
		store.SaveBearerTokenForApp("a1", "b")
		store.SaveOAuth1TokensForApp("a1", "a", "t", "c", "s")

		err := store.ClearAllForApp("a1")
		require.NoError(t, err)

		assert.Empty(t, store.GetOAuth2UsernamesForApp("a1"))
		assert.Nil(t, store.GetOAuth1TokensForApp("a1"))
		assert.Nil(t, store.GetBearerTokenForApp("a1"))
	})

	t.Run("GetFirstOAuth2TokenForApp", func(t *testing.T) {
		// Empty app returns nil
		assert.Nil(t, store.GetFirstOAuth2TokenForApp("a1"))

		// Non-empty returns something
		store.SaveOAuth2TokenForApp("a1", "u", "t", "r", 1)
		assert.NotNil(t, store.GetFirstOAuth2TokenForApp("a1"))
	})
}

func TestResolveAppEdgeCases(t *testing.T) {
	store, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	t.Run("Explicit name that doesn't exist falls to default", func(t *testing.T) {
		app := store.ResolveApp("nonexistent")
		require.NotNil(t, app)
		// Should be the default app
		assert.Equal(t, store.GetApp("default"), app)
	})

	t.Run("Empty name returns default", func(t *testing.T) {
		app := store.ResolveApp("")
		require.NotNil(t, app)
	})

	t.Run("GetActiveAppName with explicit", func(t *testing.T) {
		assert.Equal(t, "explicit", store.GetActiveAppName("explicit"))
	})

	t.Run("GetActiveAppName without explicit returns default", func(t *testing.T) {
		assert.Equal(t, store.DefaultApp, store.GetActiveAppName(""))
	})

	t.Run("SetDefaultApp nonexistent fails", func(t *testing.T) {
		err := store.SetDefaultApp("nope")
		assert.Error(t, err)
	})
}

func TestRemoveDefaultAppReassigns(t *testing.T) {
	store, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	store.AddApp("app1", "id1", "s1")
	store.SetDefaultApp("app1")
	assert.Equal(t, "app1", store.GetDefaultApp())

	// Remove the default app — should reassign
	err := store.RemoveApp("app1")
	require.NoError(t, err)
	// Default should now be some remaining app (either "default" or empty)
	assert.NotEqual(t, "app1", store.GetDefaultApp())
	assert.Contains(t, store.ListApps(), store.GetDefaultApp())
}

func TestLegacyJSONMigration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xurl-migrate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	t.Setenv("HOME", tempDir)

	// Write a legacy JSON .xurl file
	legacy := map[string]interface{}{
		"oauth2_tokens": map[string]interface{}{
			"legacyuser": map[string]interface{}{
				"type": "oauth2",
				"oauth2": map[string]interface{}{
					"access_token":    "leg-at",
					"refresh_token":   "leg-rt",
					"expiration_time": 9999,
				},
			},
		},
		"bearer_token": map[string]interface{}{
			"type":   "bearer",
			"bearer": "leg-bearer",
		},
	}
	data, _ := json.MarshalIndent(legacy, "", "  ")
	xurlPath := filepath.Join(tempDir, ".xurl")
	err = os.WriteFile(xurlPath, data, 0600)
	require.NoError(t, err)

	store := NewTokenStore()

	// Should have migrated into a "default" app
	assert.Equal(t, "default", store.GetDefaultApp())
	app := store.GetApp("default")
	require.NotNil(t, app)

	// OAuth2 token should be preserved
	tok := store.GetOAuth2Token("legacyuser")
	require.NotNil(t, tok)
	assert.Equal(t, "leg-at", tok.OAuth2.AccessToken)

	// Bearer token should be preserved
	bearer := store.GetBearerToken()
	require.NotNil(t, bearer)
	assert.Equal(t, "leg-bearer", bearer.Bearer)

	// File should now be YAML
	raw, _ := os.ReadFile(xurlPath)
	assert.Contains(t, string(raw), "apps:")
	assert.Contains(t, string(raw), "default_app:")
}

func TestYAMLPersistence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xurl-yaml-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	xurlPath := filepath.Join(tempDir, ".xurl")

	// Create and save
	s1 := &TokenStore{
		Apps:       make(map[string]*App),
		DefaultApp: "myapp",
		FilePath:   xurlPath,
	}
	s1.Apps["myapp"] = &App{
		ClientID:     "cid",
		ClientSecret: "csec",
		OAuth2Tokens: make(map[string]Token),
	}
	s1.SaveBearerToken("yaml-bearer")

	// Reload
	s2 := &TokenStore{
		Apps:     make(map[string]*App),
		FilePath: xurlPath,
	}
	data, err := os.ReadFile(xurlPath)
	require.NoError(t, err)
	s2.loadFromData(data)

	assert.Equal(t, "myapp", s2.DefaultApp)
	app := s2.GetApp("myapp")
	require.NotNil(t, app)
	assert.Equal(t, "cid", app.ClientID)
	assert.Equal(t, "yaml-bearer", app.BearerToken.Bearer)
}

func TestTwurlrc(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xurl-test")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(tempDir)

	t.Setenv("HOME", tempDir)

	twurlContent := `profiles:
  testuser:
    test_consumer_key:
      username: testuser
      consumer_key: test_consumer_key
      consumer_secret: test_consumer_secret
      token: test_access_token
      secret: test_token_secret
configuration:
  default_profile:
  - testuser
  - test_consumer_key`

	twurlPath := filepath.Join(tempDir, ".twurlrc")
	xurlPath := filepath.Join(tempDir, ".xurl")

	err = os.WriteFile(twurlPath, []byte(twurlContent), 0600)
	require.NoError(t, err, "Failed to write test .twurlrc file")

	// Test 1: Direct import from .twurlrc
	t.Run("Direct import from twurlrc", func(t *testing.T) {
		store := &TokenStore{
			Apps:       make(map[string]*App),
			DefaultApp: "default",
			FilePath:   xurlPath,
		}
		store.Apps["default"] = &App{
			OAuth2Tokens: make(map[string]Token),
		}

		err := store.importFromTwurlrc(twurlPath)
		require.NoError(t, err, "Failed to import from .twurlrc")

		// Verify the OAuth1 token was imported into the default app
		app := store.GetApp("default")
		require.NotNil(t, app, "default app is nil after import")
		require.NotNil(t, app.OAuth1Token, "OAuth1Token is nil after import")

		oauth1 := app.OAuth1Token.OAuth1
		assert.Equal(t, "test_access_token", oauth1.AccessToken, "Unexpected access token")
		assert.Equal(t, "test_token_secret", oauth1.TokenSecret, "Unexpected token secret")
		assert.Equal(t, "test_consumer_key", oauth1.ConsumerKey, "Unexpected consumer key")
		assert.Equal(t, "test_consumer_secret", oauth1.ConsumerSecret, "Unexpected consumer secret")

		_, err = os.Stat(xurlPath)
		assert.False(t, os.IsNotExist(err), ".xurl file was not created")

		os.Remove(xurlPath)
	})

	// Test 2: Auto-import when no .xurl file exists
	t.Run("Auto-import when no xurl file exists", func(t *testing.T) {
		os.Remove(xurlPath)

		store := NewTokenStore()

		oauth1Token := store.GetOAuth1Tokens()
		require.NotNil(t, oauth1Token, "OAuth1Token is nil after auto-import")

		oauth1 := oauth1Token.OAuth1
		assert.Equal(t, "test_access_token", oauth1.AccessToken, "Unexpected access token")

		_, err := os.Stat(xurlPath)
		assert.False(t, os.IsNotExist(err), ".xurl file was not created")
	})

	// Test 3: Auto-import when .xurl exists but has no OAuth1 token
	t.Run("Auto-import when xurl exists but has no OAuth1 token", func(t *testing.T) {
		store := NewTokenStore()

		// Clear OAuth1 from the active app
		store.ClearOAuth1Tokens()

		store = NewTokenStore()

		oauth1Token := store.GetOAuth1Tokens()
		require.NotNil(t, oauth1Token, "OAuth1Token is nil after re-import")

		oauth1 := oauth1Token.OAuth1
		assert.Equal(t, "test_access_token", oauth1.AccessToken, "Unexpected access token")
	})

	// Test 4: Error handling with malformed .twurlrc
	t.Run("Error handling with malformed twurlrc", func(t *testing.T) {
		malformedContent := `this is not valid yaml`
		malformedPath := filepath.Join(tempDir, ".malformed-twurlrc")

		err := os.WriteFile(malformedPath, []byte(malformedContent), 0600)
		require.NoError(t, err, "Failed to write malformed .twurlrc file")

		store := &TokenStore{
			Apps:       make(map[string]*App),
			DefaultApp: "default",
			FilePath:   xurlPath,
		}
		store.Apps["default"] = &App{
			OAuth2Tokens: make(map[string]Token),
		}

		err = store.importFromTwurlrc(malformedPath)
		assert.Error(t, err, "Expected error when importing from malformed .twurlrc")
	})
}
