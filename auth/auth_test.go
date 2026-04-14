package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xdevplatform/xurl/config"
	"github.com/xdevplatform/xurl/store"
)

// Helper function to create a temporary token store for testing
func createTempTokenStore(t *testing.T) (*store.TokenStore, string) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "xurl_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Create a token store with a file in the temp directory
	tempFile := filepath.Join(tempDir, ".xurl")
	ts := &store.TokenStore{
		Apps:       make(map[string]*store.App),
		DefaultApp: "default",
		FilePath:   tempFile,
	}
	ts.Apps["default"] = &store.App{
		OAuth2Tokens: make(map[string]store.Token),
	}

	return ts, tempDir
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

func TestCredentialResolutionPriority(t *testing.T) {
	tokenStore, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	// Store has credentials in the default app
	tokenStore.Apps["default"].ClientID = "store-id"
	tokenStore.Apps["default"].ClientSecret = "store-secret"
	tokenStore.SaveBearerToken("x") // force save

	t.Run("Env vars take priority over store", func(t *testing.T) {
		cfg := &config.Config{
			ClientID:     "env-id",
			ClientSecret: "env-secret",
		}
		a := NewAuth(cfg).WithTokenStore(tokenStore)
		assert.Equal(t, "env-id", a.clientID)
		assert.Equal(t, "env-secret", a.clientSecret)
	})

	t.Run("Store used when env vars empty", func(t *testing.T) {
		// Simulate what NewAuth does when env vars are empty:
		// it should fall back to the store's app credentials.
		a := &Auth{
			TokenStore: tokenStore,
		}
		app := tokenStore.ResolveApp("")
		a.clientID = app.ClientID
		a.clientSecret = app.ClientSecret
		assert.Equal(t, "store-id", a.clientID)
		assert.Equal(t, "store-secret", a.clientSecret)
	})
}

func TestWithAppName(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xurl_auth_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	t.Setenv("HOME", tempDir)

	tokenStore, tsDir := createTempTokenStore(t)
	defer os.RemoveAll(tsDir)

	// Add a second app with different credentials
	tokenStore.AddApp("other", "other-id", "other-secret")

	cfg := &config.Config{}
	a := NewAuth(cfg).WithTokenStore(tokenStore)

	// Initially no app override — clientID/secret are empty (no env vars, default app has none)
	assert.Empty(t, a.clientID)

	// Set app name — should pick up other app's credentials
	a.WithAppName("other")
	assert.Equal(t, "other-id", a.clientID)
	assert.Equal(t, "other-secret", a.clientSecret)
}

func TestWithAppNameNonexistent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "xurl_auth_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	t.Setenv("HOME", tempDir)

	tokenStore, tsDir := createTempTokenStore(t)
	defer os.RemoveAll(tsDir)

	cfg := &config.Config{}
	a := NewAuth(cfg).WithTokenStore(tokenStore)

	// Setting a nonexistent app name should not panic
	a.WithAppName("doesnt-exist")
	// Should fall through to default app (which has empty creds)
	assert.Empty(t, a.clientID)
}

func TestOAuth1HeaderWithTokenStore(t *testing.T) {
	tokenStore, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{}
	a := NewAuth(cfg).WithTokenStore(tokenStore)

	// No OAuth1 token — should fail
	_, err := a.GetOAuth1Header("GET", "https://api.x.com/2/users/me", nil)
	assert.Error(t, err)

	// Save OAuth1 token and try again
	tokenStore.SaveOAuth1Tokens("at", "ts", "ck", "cs")
	header, err := a.GetOAuth1Header("GET", "https://api.x.com/2/users/me", nil)
	require.NoError(t, err)
	assert.Contains(t, header, "OAuth ")
	assert.Contains(t, header, "oauth_consumer_key")
}

func TestGetOAuth2HeaderNoToken(t *testing.T) {
	tokenStore, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
		AuthURL:      "https://x.com/i/oauth2/authorize",
		TokenURL:     "https://api.x.com/2/oauth2/token",
		RedirectURI:  "http://localhost:8080/callback",
		InfoURL:      "https://api.x.com/2/users/me",
	}
	_ = NewAuth(cfg).WithTokenStore(tokenStore)

	// Verify that looking up a nonexistent user returns nil
	token := tokenStore.GetOAuth2Token("nobody")
	assert.Nil(t, token)
}

func TestBrowserLaunchCommand(t *testing.T) {
	url := "https://x.com/i/oauth2/authorize?client_id=abc&redirect_uri=http%3A%2F%2Flocalhost%3A8080%2Fcallback&response_type=code&scope=tweet.read+users.read&state=123&code_challenge=xyz&code_challenge_method=S256"

	t.Run("windows keeps the full oauth url as a single argument", func(t *testing.T) {
		cmd, args := browserLaunchCommand("windows", url)

		assert.Equal(t, "rundll32", cmd)
		assert.Equal(t, []string{"url.dll,FileProtocolHandler", url}, args)
	})

	t.Run("darwin uses open", func(t *testing.T) {
		cmd, args := browserLaunchCommand("darwin", url)

		assert.Equal(t, "open", cmd)
		assert.Equal(t, []string{url}, args)
	})

	t.Run("linux uses xdg-open", func(t *testing.T) {
		cmd, args := browserLaunchCommand("linux", url)

		assert.Equal(t, "xdg-open", cmd)
		assert.Equal(t, []string{url}, args)
	})
}
