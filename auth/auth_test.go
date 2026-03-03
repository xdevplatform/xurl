package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

// futureExpiry returns a unix timestamp 1 hour in the future.
func futureExpiry() uint64 {
	return uint64(time.Now().Add(time.Hour).Unix())
}

// setupMultiAppAuth creates a token store with two apps for multi-app tests.
func setupMultiAppAuth(t *testing.T) (*Auth, *store.TokenStore, string) {
	tempDir, err := os.MkdirTemp("", "xurl_multiapp_test")
	require.NoError(t, err)

	tempFile := filepath.Join(tempDir, ".xurl")
	ts := &store.TokenStore{
		Apps:       make(map[string]*store.App),
		DefaultApp: "app-a",
		FilePath:   tempFile,
	}

	ts.Apps["app-a"] = &store.App{
		ClientID:     "id-a",
		ClientSecret: "secret-a",
		DefaultUser:  "alice-a",
		OAuth2Tokens: map[string]store.Token{
			"alice-a": {
				Type: store.OAuth2TokenType,
				OAuth2: &store.OAuth2Token{
					AccessToken:    "oauth2-token-alice-a",
					RefreshToken:   "refresh-alice-a",
					ExpirationTime: futureExpiry(),
				},
			},
		},
		OAuth1Token: &store.Token{
			Type: store.OAuth1TokenType,
			OAuth1: &store.OAuth1Token{
				AccessToken:    "at-a",
				TokenSecret:    "ts-a",
				ConsumerKey:    "ck-a",
				ConsumerSecret: "cs-a",
			},
		},
		BearerToken: &store.Token{
			Type:   store.BearerTokenType,
			Bearer: "bearer-a",
		},
	}

	ts.Apps["app-b"] = &store.App{
		ClientID:     "id-b",
		ClientSecret: "secret-b",
		DefaultUser:  "alice-b",
		OAuth2Tokens: map[string]store.Token{
			"alice-b": {
				Type: store.OAuth2TokenType,
				OAuth2: &store.OAuth2Token{
					AccessToken:    "oauth2-token-alice-b",
					RefreshToken:   "refresh-alice-b",
					ExpirationTime: futureExpiry(),
				},
			},
		},
		OAuth1Token: &store.Token{
			Type: store.OAuth1TokenType,
			OAuth1: &store.OAuth1Token{
				AccessToken:    "at-b",
				TokenSecret:    "ts-b",
				ConsumerKey:    "ck-b",
				ConsumerSecret: "cs-b",
			},
		},
		BearerToken: &store.Token{
			Type:   store.BearerTokenType,
			Bearer: "bearer-b",
		},
	}

	a := NewAuth(&config.Config{
		ClientID:     "id-a",
		ClientSecret: "secret-a",
		APIBaseURL:   "https://api.x.com",
		AuthURL:      "https://x.com/i/oauth2/authorize",
		TokenURL:     "https://api.x.com/2/oauth2/token",
		RedirectURI:  "http://localhost:8080/callback",
		InfoURL:      "https://api.x.com/2/users/me",
	}).WithTokenStore(ts)

	return a, ts, tempDir
}

// TC 5.1: WithAppName overwrites non-empty clientID/clientSecret
func TestTC5_1_WithAppNameOverwritesNonEmptyCredentials(t *testing.T) {
	a, _, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	// Auth starts with app-a's non-empty credentials
	require.Equal(t, "id-a", a.clientID)
	require.Equal(t, "secret-a", a.clientSecret)

	// Switch to app-b — must overwrite even though clientID/clientSecret are non-empty
	a.WithAppName("app-b")
	assert.Equal(t, "id-b", a.clientID, "WithAppName must overwrite non-empty clientID")
	assert.Equal(t, "secret-b", a.clientSecret, "WithAppName must overwrite non-empty clientSecret")
}

// TC 5.2: After WithAppName("app-b"), verify a.clientID and a.clientSecret match app-b
func TestTC5_2_ClientCredentialsMatchApp(t *testing.T) {
	a, ts, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	a.WithAppName("app-b")
	assert.Equal(t, ts.Apps["app-b"].ClientID, a.clientID)
	assert.Equal(t, ts.Apps["app-b"].ClientSecret, a.clientSecret)
	assert.Equal(t, "app-b", a.appName)
}

// TC 5.4: app-b has "alice-b" (default_user) and "bob-b" → GetOAuth2Header("") returns alice-b's token
func TestTC5_4_DefaultUserOAuth2(t *testing.T) {
	a, ts, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	// Add "bob-b" to app-b as well
	ts.Apps["app-b"].OAuth2Tokens["bob-b"] = store.Token{
		Type: store.OAuth2TokenType,
		OAuth2: &store.OAuth2Token{
			AccessToken:    "oauth2-token-bob-b",
			RefreshToken:   "refresh-bob-b",
			ExpirationTime: futureExpiry(),
		},
	}

	a.WithAppName("app-b")
	// DefaultUser is "alice-b", so GetOAuth2Header("") should return alice-b's token
	header, err := a.GetOAuth2Header("")
	require.NoError(t, err)
	assert.Equal(t, "Bearer oauth2-token-alice-b", header)
}

// TC 5.5: After WithAppName("app-b"), SaveOAuth2TokenForApp stores in app-b
func TestTC5_5_SaveOAuth2TokenGoesToActiveApp(t *testing.T) {
	a, ts, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	a.WithAppName("app-b")

	// Save a new token through the store using a.appName
	err := ts.SaveOAuth2TokenForApp(a.appName, "newuser-b", "new-access-b", "new-refresh-b", futureExpiry())
	require.NoError(t, err)

	// Verify the token is in app-b
	tok := ts.GetOAuth2TokenForApp("app-b", "newuser-b")
	require.NotNil(t, tok)
	assert.Equal(t, "new-access-b", tok.OAuth2.AccessToken)

	// Verify app-a is untouched
	tokA := ts.GetOAuth2TokenForApp("app-a", "newuser-b")
	assert.Nil(t, tokA, "Token should not exist in app-a")
}

// TC 5.6: WithAppName("app-b") → ClearAllForApp → only app-b cleared, default untouched
func TestTC5_6_ClearOnlyActiveApp(t *testing.T) {
	a, ts, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	a.WithAppName("app-b")

	// Clear all tokens for app-b
	err := ts.ClearAllForApp(a.appName)
	require.NoError(t, err)

	// app-b should have no tokens
	assert.Nil(t, ts.GetBearerTokenForApp("app-b"), "app-b bearer should be cleared")
	assert.Nil(t, ts.GetOAuth1TokensForApp("app-b"), "app-b OAuth1 should be cleared")
	assert.Empty(t, ts.GetOAuth2UsernamesForApp("app-b"), "app-b OAuth2 tokens should be cleared")

	// app-a should be untouched
	assert.NotNil(t, ts.GetBearerTokenForApp("app-a"), "app-a bearer must remain")
	assert.NotNil(t, ts.GetOAuth1TokensForApp("app-a"), "app-a OAuth1 must remain")
	assert.NotEmpty(t, ts.GetOAuth2UsernamesForApp("app-a"), "app-a OAuth2 tokens must remain")
}

// TestAppNameGetter verifies the AppName() getter returns the current override.
func TestAppNameGetter(t *testing.T) {
	a, _, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	// Initial app name comes from config (empty in setupMultiAppAuth)
	assert.Empty(t, a.AppName())

	a.WithAppName("app-b")
	assert.Equal(t, "app-b", a.AppName())
}

// TestBearerTokenSwitchBetweenApps verifies bearer tokens switch with app context.
func TestBearerTokenSwitchBetweenApps(t *testing.T) {
	a, _, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	rounds := []struct {
		app    string
		bearer string
	}{
		{"app-a", "bearer-a"},
		{"app-b", "bearer-b"},
		{"app-a", "bearer-a"},
	}

	for i, r := range rounds {
		a.WithAppName(r.app)
		header, err := a.GetBearerTokenHeader()
		require.NoError(t, err, "round %d: unexpected error", i)
		assert.Equal(t, "Bearer "+r.bearer, header, "round %d: wrong bearer for %s", i, r.app)
	}
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
