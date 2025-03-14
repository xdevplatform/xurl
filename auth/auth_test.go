package auth

import (
	"os"
	"path/filepath"
	"testing"

	"xurl/config"
	"xurl/store"
)

// Helper function to create a temporary token store for testing
func createTempTokenStore(t *testing.T) (*store.TokenStore, string) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "xurl_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	
	// Create a token store with a file in the temp directory
	tempFile := filepath.Join(tempDir, "tokens.json")
	store := &store.TokenStore{
		OAuth2Tokens: make(map[string]store.Token),
		FilePath:     tempFile,
	}
	
	return store, tempDir
}

func TestNewAuth(t *testing.T) {
	cfg := &config.Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURI:  "http://localhost:8080/callback",
		AuthURL:      "https://x.com/i/oauth2/authorize",
		TokenURL:     "https://api.x.com/2/oauth2/token",
		APIBaseURL:   "https://api.x.com",
		InfoURL:      "https://api.x.com/2/users/me",
	}
	
	auth := NewAuth(cfg)
	
	if auth == nil {
		t.Fatal("Expected non-nil Auth")
	}
	
	if auth.TokenStore == nil {
		t.Error("Expected non-nil TokenStore")
	}
}

func TestWithTokenStore(t *testing.T) {
	cfg := &config.Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURI:  "http://localhost:8080/callback",
		AuthURL:      "https://x.com/i/oauth2/authorize",
		TokenURL:     "https://api.x.com/2/oauth2/token",
		APIBaseURL:   "https://api.x.com",
		InfoURL:      "https://api.x.com/2/users/me",
	}
	
	auth := NewAuth(cfg)
	
	tokenStore, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)
	
	newAuth := auth.WithTokenStore(tokenStore)
	
	if newAuth == nil {
		t.Fatal("Expected non-nil Auth")
	}
	
	if newAuth.TokenStore != tokenStore {
		t.Error("Expected TokenStore to be set to the provided TokenStore")
	}
}

func TestBearerToken(t *testing.T) {
	cfg := &config.Config{}
	
	auth := NewAuth(cfg)
	tokenStore, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)
	
	auth = auth.WithTokenStore(tokenStore)
	
	// Test with no bearer token
	token := auth.GetBearerTokenHeader()
	if token != "" {
		t.Errorf("Expected empty token, got %s", token)
	}
	
	// Test with bearer token
	err := tokenStore.SaveBearerToken("test-bearer-token")
	if err != nil {
		t.Fatalf("Failed to save bearer token: %v", err)
	}
	
	token = auth.GetBearerTokenHeader()
	if token != "Bearer test-bearer-token" {
		t.Errorf("Expected 'Bearer test-bearer-token', got %s", token)
	}
}

func TestGenerateNonce(t *testing.T) {
	nonce1 := generateNonce()
	nonce2 := generateNonce()
	
	if nonce1 == "" {
		t.Error("Expected non-empty nonce")
	}
	
	if nonce1 == nonce2 {
		t.Error("Expected different nonces")
	}
}

func TestGenerateTimestamp(t *testing.T) {
	timestamp := generateTimestamp()
	
	if timestamp == "" {
		t.Error("Expected non-empty timestamp")
	}
	
	for _, c := range timestamp {
		if c < '0' || c > '9' {
			t.Errorf("Expected timestamp to contain only digits, got %s", timestamp)
			break
		}
	}
}

func TestEncode(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"abc", "abc"},
		{"a b c", "a+b+c"},
		{"a+b+c", "a%2Bb%2Bc"},
		{"a/b/c", "a%2Fb%2Fc"},
		{"a?b=c", "a%3Fb%3Dc"},
		{"a&b=c", "a%26b%3Dc"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := encode(tc.input)
			if result != tc.expected {
				t.Errorf("encode(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestGenerateCodeVerifierAndChallenge(t *testing.T) {
	verifier, challenge := generateCodeVerifierAndChallenge()
	
	if verifier == "" {
		t.Error("Expected non-empty verifier")
	}
	
	if challenge == "" {
		t.Error("Expected non-empty challenge")
	}
	
	if verifier == challenge {
		t.Error("Expected verifier and challenge to be different")
	}
}

func TestGetOAuth2Scopes(t *testing.T) {
	scopes := getOAuth2Scopes()
	
	if len(scopes) == 0 {
		t.Error("Expected non-empty scopes")
	}
	
	// Check for some common scopes
	foundTweetRead := false
	foundUsersRead := false
	
	for _, scope := range scopes {
		if scope == "tweet.read" {
			foundTweetRead = true
		}
		if scope == "users.read" {
			foundUsersRead = true
		}
	}
	
	if !foundTweetRead {
		t.Error("Expected 'tweet.read' scope")
	}
	
	if !foundUsersRead {
		t.Error("Expected 'users.read' scope")
	}
} 