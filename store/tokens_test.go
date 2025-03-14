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

func TestSaveAndGetBearerToken(t *testing.T) {
	store, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)
	
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
		t.Errorf("Expected bearer token 'Bearer test-bearer-token', got '%s'", token.Bearer)
	}
	
	if !store.HasBearerToken() {
		t.Error("Expected HasBearerToken to return true")
	}
	
	// Test clearing bearer token
	err = store.ClearBearerToken()
	if err != nil {
		t.Fatalf("Failed to clear bearer token: %v", err)
	}
	
	if store.HasBearerToken() {
		t.Error("Expected HasBearerToken to return false after clearing")
	}
}

func TestSaveAndGetOAuth2Token(t *testing.T) {
	store, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)
	
	// Test saving OAuth2 token
	err := store.SaveOAuth2Token("testuser", "access-token", "refresh-token", 1234567890)
	if err != nil {
		t.Fatalf("Failed to save OAuth2 token: %v", err)
	}
	
	// Test getting OAuth2 token
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
	
	// Test GetOAuth2Usernames
	usernames := store.GetOAuth2Usernames()
	if len(usernames) != 1 || usernames[0] != "testuser" {
		t.Errorf("Expected usernames ['testuser'], got %v", usernames)
	}
	
	// Test GetFirstOAuth2Token
	firstToken := store.GetFirstOAuth2Token()
	if firstToken == nil {
		t.Fatal("Expected non-nil first token")
	}
	
	// Test clearing OAuth2 token
	err = store.ClearOAuth2Token("testuser")
	if err != nil {
		t.Fatalf("Failed to clear OAuth2 token: %v", err)
	}
	
	if store.GetOAuth2Token("testuser") != nil {
		t.Error("Expected nil token after clearing")
	}
}

func TestSaveAndGetOAuth1Tokens(t *testing.T) {
	store, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)
	
	// Test saving OAuth1 tokens
	err := store.SaveOAuth1Tokens("access-token", "token-secret", "consumer-key", "consumer-secret")
	if err != nil {
		t.Fatalf("Failed to save OAuth1 tokens: %v", err)
	}
	
	// Test getting OAuth1 tokens
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
	
	// Test HasOAuth1Tokens
	if !store.HasOAuth1Tokens() {
		t.Error("Expected HasOAuth1Tokens to return true")
	}
	
	// Test clearing OAuth1 tokens
	err = store.ClearOAuth1Tokens()
	if err != nil {
		t.Fatalf("Failed to clear OAuth1 tokens: %v", err)
	}
	
	if store.HasOAuth1Tokens() {
		t.Error("Expected HasOAuth1Tokens to return false after clearing")
	}
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