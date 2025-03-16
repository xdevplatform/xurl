package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	
	require.NotNil(t, auth, "Expected non-nil Auth")
	assert.NotNil(t, auth.TokenStore, "Expected non-nil TokenStore")
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
	
	require.NotNil(t, newAuth, "Expected non-nil Auth")
	assert.Equal(t, tokenStore, newAuth.TokenStore, "Expected TokenStore to be set to the provided TokenStore")
}

func TestBearerToken(t *testing.T) {
	cfg := &config.Config{}
	
	auth := NewAuth(cfg)
	tokenStore, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)
	
	auth = auth.WithTokenStore(tokenStore)
	
	// Test with no bearer token
	_, err := auth.GetBearerTokenHeader()
	assert.Error(t, err, "Expected error when no bearer token is set")
	
	// Test with bearer token
	err = tokenStore.SaveBearerToken("test-bearer-token")
	require.NoError(t, err, "Failed to save bearer token")
	
	token, err := auth.GetBearerTokenHeader()
	require.NoError(t, err, "Failed to get bearer token")
	assert.Equal(t, "Bearer test-bearer-token", token, "Expected correct bearer token format")
}

func TestGenerateNonce(t *testing.T) {
	nonce1 := generateNonce()
	nonce2 := generateNonce()
	
	assert.NotEmpty(t, nonce1, "Expected non-empty nonce")
	assert.NotEqual(t, nonce1, nonce2, "Expected different nonces")
}

func TestGenerateTimestamp(t *testing.T) {
	timestamp := generateTimestamp()
	
	assert.NotEmpty(t, timestamp, "Expected non-empty timestamp")
	
	for _, c := range timestamp {
		assert.True(t, c >= '0' && c <= '9', "Expected timestamp to contain only digits, got %s", timestamp)
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
			assert.Equal(t, tc.expected, result, "encode(%q) should return %q", tc.input, result)
		})
	}
}

func TestGenerateCodeVerifierAndChallenge(t *testing.T) {
	verifier, challenge := generateCodeVerifierAndChallenge()
	
	assert.NotEmpty(t, verifier, "Expected non-empty verifier")
	assert.NotEmpty(t, challenge, "Expected non-empty challenge")
	assert.NotEqual(t, verifier, challenge, "Expected verifier and challenge to be different")
}

func TestGetOAuth2Scopes(t *testing.T) {
	scopes := getOAuth2Scopes()
	
	assert.NotEmpty(t, scopes, "Expected non-empty scopes")
	
	// Check for some common scopes
	assert.Contains(t, scopes, "tweet.read", "Expected 'tweet.read' scope")
	assert.Contains(t, scopes, "users.read", "Expected 'users.read' scope")
} 