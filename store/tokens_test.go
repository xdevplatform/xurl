package store

import (
	"os"
	"path/filepath"
	"testing"
)

func createTempTokenStore(t *testing.T) (*TokenStore, string) {
	tempDir, err := os.MkdirTemp("", "xurl_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	
	tempFile := filepath.Join(tempDir, "tokens.json")
	store := &TokenStore{
		OAuth2Tokens: make(map[string]Token),
		FilePath:     tempFile,
	}
	
	return store, tempDir
}

func TestNewTokenStore(t *testing.T) {
	store := NewTokenStore()
	
	if store == nil {
		t.Fatal("Expected non-nil TokenStore")
	}
	
	if store.OAuth2Tokens == nil {
		t.Error("Expected non-nil OAuth2Tokens map")
	}
	
	if store.FilePath == "" {
		t.Error("Expected non-empty FilePath")
	}
}

func TestTokenOperations(t *testing.T) {
	store, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)
	
	t.Run("Bearer Token", func(t *testing.T) {
		err := store.SaveBearerToken("test-bearer-token")
		if err != nil {
			t.Fatalf("Failed to save bearer token: %v", err)
		}
		
		token := store.GetBearerToken()
		if token == nil {
			t.Fatal("Expected non-nil token")
		}
		
		if token.Type != BearerTokenType {
			t.Errorf("Expected token type %s, got %s", BearerTokenType, token.Type)
		}
		
		if token.Bearer != "test-bearer-token" {
			t.Errorf("Expected bearer token 'test-bearer-token', got '%s'", token.Bearer)
		}
		
		if !store.HasBearerToken() {
			t.Error("Expected HasBearerToken to return true")
		}
		
		err = store.ClearBearerToken()
		if err != nil {
			t.Fatalf("Failed to clear bearer token: %v", err)
		}
		
		if store.HasBearerToken() {
			t.Error("Expected HasBearerToken to return false after clearing")
		}
	})
	
	// Test OAuth2 Token operations
	t.Run("OAuth2 Token", func(t *testing.T) {
		err := store.SaveOAuth2Token("testuser", "access-token", "refresh-token", 1234567890)
		if err != nil {
			t.Fatalf("Failed to save OAuth2 token: %v", err)
		}
		
		token := store.GetOAuth2Token("testuser")
		if token == nil {
			t.Fatal("Expected non-nil token")
		}
		
		if token.Type != OAuth2TokenType {
			t.Errorf("Expected token type %s, got %s", OAuth2TokenType, token.Type)
		}
		
		if token.OAuth2 == nil {
			t.Fatal("Expected non-nil OAuth2 token")
		}
		
		if token.OAuth2.AccessToken != "access-token" {
			t.Errorf("Expected access token 'access-token', got '%s'", token.OAuth2.AccessToken)
		}
		
		if token.OAuth2.RefreshToken != "refresh-token" {
			t.Errorf("Expected refresh token 'refresh-token', got '%s'", token.OAuth2.RefreshToken)
		}
		
		if token.OAuth2.ExpirationTime != 1234567890 {
			t.Errorf("Expected expiration time 1234567890, got %d", token.OAuth2.ExpirationTime)
		}
		
		usernames := store.GetOAuth2Usernames()
		if len(usernames) != 1 || usernames[0] != "testuser" {
			t.Errorf("Expected usernames ['testuser'], got %v", usernames)
		}
		
		firstToken := store.GetFirstOAuth2Token()
		if firstToken == nil {
			t.Fatal("Expected non-nil first token")
		}
		
		err = store.ClearOAuth2Token("testuser")
		if err != nil {
			t.Fatalf("Failed to clear OAuth2 token: %v", err)
		}
		
		if store.GetOAuth2Token("testuser") != nil {
			t.Error("Expected nil token after clearing")
		}
	})
	
	// Test OAuth1 Token operations
	t.Run("OAuth1 Tokens", func(t *testing.T) {
		err := store.SaveOAuth1Tokens("access-token", "token-secret", "consumer-key", "consumer-secret")
		if err != nil {
			t.Fatalf("Failed to save OAuth1 tokens: %v", err)
		}
		
		token := store.GetOAuth1Tokens()
		if token == nil {
			t.Fatal("Expected non-nil token")
		}
		
		if token.Type != OAuth1TokenType {
			t.Errorf("Expected token type %s, got %s", OAuth1TokenType, token.Type)
		}
		
		if token.OAuth1 == nil {
			t.Fatal("Expected non-nil OAuth1 token")
		}
		
		if token.OAuth1.AccessToken != "access-token" {
			t.Errorf("Expected access token 'access-token', got '%s'", token.OAuth1.AccessToken)
		}
		
		if token.OAuth1.TokenSecret != "token-secret" {
			t.Errorf("Expected token secret 'token-secret', got '%s'", token.OAuth1.TokenSecret)
		}
		
		if token.OAuth1.ConsumerKey != "consumer-key" {
			t.Errorf("Expected consumer key 'consumer-key', got '%s'", token.OAuth1.ConsumerKey)
		}
		
		if token.OAuth1.ConsumerSecret != "consumer-secret" {
			t.Errorf("Expected consumer secret 'consumer-secret', got '%s'", token.OAuth1.ConsumerSecret)
		}
		
		if !store.HasOAuth1Tokens() {
			t.Error("Expected HasOAuth1Tokens to return true")
		}
		
		err = store.ClearOAuth1Tokens()
		if err != nil {
			t.Fatalf("Failed to clear OAuth1 tokens: %v", err)
		}
		
		if store.HasOAuth1Tokens() {
			t.Error("Expected HasOAuth1Tokens to return false after clearing")
		}
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
	if err != nil {
		t.Fatalf("Failed to clear all tokens: %v", err)
	}
	
	if store.HasBearerToken() {
		t.Error("Expected HasBearerToken to return false after clearing all")
	}
	
	if store.HasOAuth1Tokens() {
		t.Error("Expected HasOAuth1Tokens to return false after clearing all")
	}
	
	if len(store.GetOAuth2Usernames()) != 0 {
		t.Error("Expected empty OAuth2 usernames after clearing all")
	}
}

func TestTwurlrc(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xurl-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
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

	if err := os.WriteFile(twurlPath, []byte(twurlContent), 0600); err != nil {
		t.Fatalf("Failed to write test .twurlrc file: %v", err)
	}

	// Test 1: Direct import from .twurlrc
	t.Run("Direct import from twurlrc", func(t *testing.T) {
		store := &TokenStore{
			OAuth2Tokens: make(map[string]Token),
			FilePath:     xurlPath,
		}

		if err := store.importFromTwurlrc(twurlPath); err != nil {
			t.Fatalf("Failed to import from .twurlrc: %v", err)
		}

		// Verify the OAuth1 token was imported correctly
		if store.OAuth1Token == nil {
			t.Fatal("OAuth1Token is nil after import")
		}

		oauth1 := store.OAuth1Token.OAuth1
		if oauth1.AccessToken != "test_access_token" {
			t.Errorf("Expected access token 'test_access_token', got '%s'", oauth1.AccessToken)
		}
		if oauth1.TokenSecret != "test_token_secret" {
			t.Errorf("Expected token secret 'test_token_secret', got '%s'", oauth1.TokenSecret)
		}
		if oauth1.ConsumerKey != "test_consumer_key" {
			t.Errorf("Expected consumer key 'test_consumer_key', got '%s'", oauth1.ConsumerKey)
		}
		if oauth1.ConsumerSecret != "test_consumer_secret" {
			t.Errorf("Expected consumer secret 'test_consumer_secret', got '%s'", oauth1.ConsumerSecret)
		}

		if _, err := os.Stat(xurlPath); os.IsNotExist(err) {
			t.Fatal(".xurl file was not created")
		}

		os.Remove(xurlPath)
	})

	// Test 2: Auto-import when no .xurl file exists
	t.Run("Auto-import when no xurl file exists", func(t *testing.T) {
		os.Remove(xurlPath)

		store := NewTokenStore()

		if store.OAuth1Token == nil {
			t.Fatal("OAuth1Token is nil after auto-import")
		}

		oauth1 := store.OAuth1Token.OAuth1
		if oauth1.AccessToken != "test_access_token" {
			t.Errorf("Expected access token 'test_access_token', got '%s'", oauth1.AccessToken)
		}

		if _, err := os.Stat(xurlPath); os.IsNotExist(err) {
			t.Fatal(".xurl file was not created")
		}
	})

	// Test 3: Auto-import when .xurl exists but has no OAuth1 token
	t.Run("Auto-import when xurl exists but has no OAuth1 token", func(t *testing.T) {
		store := NewTokenStore()
		
		store.OAuth1Token = nil
		if err := store.saveToFile(); err != nil {
			t.Fatalf("Failed to save token store: %v", err)
		}

		store = NewTokenStore()

		if store.OAuth1Token == nil {
			t.Fatal("OAuth1Token is nil after re-import")
		}

		oauth1 := store.OAuth1Token.OAuth1
		if oauth1.AccessToken != "test_access_token" {
			t.Errorf("Expected access token 'test_access_token', got '%s'", oauth1.AccessToken)
		}
	})

	// Test 4: Error handling with malformed .twurlrc
	t.Run("Error handling with malformed twurlrc", func(t *testing.T) {
		malformedContent := `this is not valid yaml`
		malformedPath := filepath.Join(tempDir, ".malformed-twurlrc")
		
		if err := os.WriteFile(malformedPath, []byte(malformedContent), 0600); err != nil {
			t.Fatalf("Failed to write malformed .twurlrc file: %v", err)
		}

		store := &TokenStore{
			OAuth2Tokens: make(map[string]Token),
			FilePath:     xurlPath,
		}

		err := store.importFromTwurlrc(malformedPath)
		if err == nil {
			t.Fatal("Expected error when importing from malformed .twurlrc, but got nil")
		}
	})
} 