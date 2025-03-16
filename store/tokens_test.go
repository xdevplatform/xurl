package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTempTokenStore(t *testing.T) (*TokenStore, string) {
	tempDir, err := os.MkdirTemp("", "xurl_test")
	require.NoError(t, err, "Failed to create temp directory")
	
	tempFile := filepath.Join(tempDir, "tokens.json")
	store := &TokenStore{
		OAuth2Tokens: make(map[string]Token),
		FilePath:     tempFile,
	}
	
	return store, tempDir
}

func TestNewTokenStore(t *testing.T) {
	store := NewTokenStore()
	
	assert.NotNil(t, store, "Expected non-nil TokenStore")
	assert.NotNil(t, store.OAuth2Tokens, "Expected non-nil OAuth2Tokens map")
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
			OAuth2Tokens: make(map[string]Token),
			FilePath:     xurlPath,
		}

		err := store.importFromTwurlrc(twurlPath)
		require.NoError(t, err, "Failed to import from .twurlrc")

		// Verify the OAuth1 token was imported correctly
		require.NotNil(t, store.OAuth1Token, "OAuth1Token is nil after import")

		oauth1 := store.OAuth1Token.OAuth1
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

		require.NotNil(t, store.OAuth1Token, "OAuth1Token is nil after auto-import")

		oauth1 := store.OAuth1Token.OAuth1
		assert.Equal(t, "test_access_token", oauth1.AccessToken, "Unexpected access token")

		_, err := os.Stat(xurlPath)
		assert.False(t, os.IsNotExist(err), ".xurl file was not created")
	})

	// Test 3: Auto-import when .xurl exists but has no OAuth1 token
	t.Run("Auto-import when xurl exists but has no OAuth1 token", func(t *testing.T) {
		store := NewTokenStore()
		
		store.OAuth1Token = nil
		err := store.saveToFile()
		require.NoError(t, err, "Failed to save token store")

		store = NewTokenStore()

		require.NotNil(t, store.OAuth1Token, "OAuth1Token is nil after re-import")

		oauth1 := store.OAuth1Token.OAuth1
		assert.Equal(t, "test_access_token", oauth1.AccessToken, "Unexpected access token")
	})

	// Test 4: Error handling with malformed .twurlrc
	t.Run("Error handling with malformed twurlrc", func(t *testing.T) {
		malformedContent := `this is not valid yaml`
		malformedPath := filepath.Join(tempDir, ".malformed-twurlrc")
		
		err := os.WriteFile(malformedPath, []byte(malformedContent), 0600)
		require.NoError(t, err, "Failed to write malformed .twurlrc file")

		store := &TokenStore{
			OAuth2Tokens: make(map[string]Token),
			FilePath:     xurlPath,
		}

		err = store.importFromTwurlrc(malformedPath)
		assert.Error(t, err, "Expected error when importing from malformed .twurlrc")
	})
} 