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

// ─── Helpers ────────────────────────────────────────────────────────

// createTempTokenStore creates a temporary token store for basic tests (single default app).
func createTempTokenStore(t *testing.T) (*store.TokenStore, string) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "xurl_test")
	require.NoError(t, err)

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

// futureExpiry returns a Unix timestamp 1 hour in the future.
func futureExpiry() uint64 {
	return uint64(time.Now().Add(time.Hour).Unix())
}

// setupMultiAppAuth creates a two-app token store and an Auth pre-configured with app-a's credentials.
//
//	app-a (default): OAuth2("alice-a"), OAuth1, Bearer, clientID:"id-a", clientSecret:"secret-a"
//	app-b:           OAuth2("alice-b"), OAuth1, Bearer, clientID:"id-b", clientSecret:"secret-b"
func setupMultiAppAuth(t *testing.T) (*Auth, *store.TokenStore, string) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "xurl_multiapp_test")
	require.NoError(t, err)

	tempFile := filepath.Join(tempDir, ".xurl")
	ts := &store.TokenStore{
		Apps:       make(map[string]*store.App),
		DefaultApp: "app-a",
		FilePath:   tempFile,
	}

	// app-a
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

	// app-b
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

	// Auth starts with app-a credentials (simulating NewAuth with app-a as default)
	a := &Auth{
		TokenStore:   ts,
		clientID:     "id-a",
		clientSecret: "secret-a",
		appName:      "app-a",
		authURL:      "https://x.com/i/oauth2/authorize",
		tokenURL:     "https://api.x.com/2/oauth2/token",
		redirectURI:  "http://localhost:8080/callback",
		infoURL:      "https://api.x.com/2/users/me",
	}

	return a, ts, tempDir
}

// ─── Existing tests (preserved) ─────────────────────────────────────

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
			assert.Equal(t, tc.expected, result, "encode(%q) should return %q", tc.input, tc.expected)
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
	assert.Contains(t, scopes, "tweet.read", "Expected 'tweet.read' scope")
	assert.Contains(t, scopes, "users.read", "Expected 'users.read' scope")
}

func TestCredentialResolutionPriority(t *testing.T) {
	tokenStore, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	tokenStore.Apps["default"].ClientID = "store-id"
	tokenStore.Apps["default"].ClientSecret = "store-secret"
	tokenStore.SaveBearerToken("x")

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

	tokenStore.AddApp("other", "other-id", "other-secret")

	cfg := &config.Config{}
	a := NewAuth(cfg).WithTokenStore(tokenStore)

	assert.Empty(t, a.clientID)

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

	a.WithAppName("doesnt-exist")
	assert.Empty(t, a.clientID)
}

func TestOAuth1HeaderWithTokenStore(t *testing.T) {
	tokenStore, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{}
	a := NewAuth(cfg).WithTokenStore(tokenStore)

	_, err := a.GetOAuth1Header("GET", "https://api.x.com/2/users/me", nil)
	assert.Error(t, err)

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

	token := tokenStore.GetOAuth2Token("nobody")
	assert.Nil(t, token)
}

// ─── 1. Happy Path ───────────────────────────────────────────────────

// TC 1.1: WithAppName("app-b") → GetOAuth1Header() returns app-b's consumer key
func TestTC1_1_WithAppBGetOAuth1Header(t *testing.T) {
	a, _, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	a.WithAppName("app-b")
	header, err := a.GetOAuth1Header("GET", "https://api.x.com/2/users/me", nil)
	require.NoError(t, err)
	assert.Contains(t, header, "OAuth ")
	// The OAuth1 header format wraps values in literal double-quotes
	assert.Contains(t, header, `oauth_consumer_key="ck-b"`)
}

// TC 1.2: WithAppName("app-b") → GetOAuth2Header("") returns app-b's access token
func TestTC1_2_WithAppBGetOAuth2Header(t *testing.T) {
	a, _, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	a.WithAppName("app-b")
	header, err := a.GetOAuth2Header("")
	require.NoError(t, err)
	assert.Equal(t, "Bearer oauth2-token-alice-b", header)
}

// TC 1.3: WithAppName("app-b") → GetBearerTokenHeader() returns app-b's bearer
func TestTC1_3_WithAppBGetBearerTokenHeader(t *testing.T) {
	a, _, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	a.WithAppName("app-b")
	header, err := a.GetBearerTokenHeader()
	require.NoError(t, err)
	assert.Equal(t, "Bearer bearer-b", header)
}

// ─── 2. Edge Cases ───────────────────────────────────────────────────

// TC 2.1: WithAppName("") → returns default app's tokens
func TestTC2_1_WithAppNameEmptyReturnsDefault(t *testing.T) {
	a, _, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	// Start with app-b, then clear to empty to reset to default
	a.WithAppName("")
	header, err := a.GetBearerTokenHeader()
	require.NoError(t, err)
	// Empty string resolves to the store's default app (app-a)
	assert.Equal(t, "Bearer bearer-a", header)
}

// TC 2.2: WithAppName("app-a") (same as default) → works correctly
func TestTC2_2_WithAppNameSameAsDefault(t *testing.T) {
	a, _, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	a.WithAppName("app-a")
	header, err := a.GetBearerTokenHeader()
	require.NoError(t, err)
	assert.Equal(t, "Bearer bearer-a", header)

	oauth1, err := a.GetOAuth1Header("GET", "https://api.x.com/2/users/me", nil)
	require.NoError(t, err)
	assert.Contains(t, oauth1, `oauth_consumer_key="ck-a"`)
}

// TC 2.3: Sequential switching: app-a → get token → app-b → get token → verify each correct
func TestTC2_3_SequentialSwitching(t *testing.T) {
	a, _, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	// Check app-a (already set)
	headerA, err := a.GetBearerTokenHeader()
	require.NoError(t, err)
	assert.Equal(t, "Bearer bearer-a", headerA)

	// Switch to app-b
	a.WithAppName("app-b")
	headerB, err := a.GetBearerTokenHeader()
	require.NoError(t, err)
	assert.Equal(t, "Bearer bearer-b", headerB)

	// Switch back to app-a
	a.WithAppName("app-a")
	headerA2, err := a.GetBearerTokenHeader()
	require.NoError(t, err)
	assert.Equal(t, "Bearer bearer-a", headerA2)
}

// ─── 3. Error Conditions ────────────────────────────────────────────

// TC 3.1: WithAppName("ghost-app") → falls back to default (current ResolveApp behavior)
func TestTC3_1_NonexistentAppFallsBackToDefault(t *testing.T) {
	a, _, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	a.WithAppName("ghost-app")
	// ResolveApp("ghost-app") returns default app (app-a) since "ghost-app" doesn't exist
	header, err := a.GetBearerTokenHeader()
	require.NoError(t, err)
	assert.Equal(t, "Bearer bearer-a", header)
}

// TC 3.2: app-b exists but has NO OAuth1 → GetOAuth1Header() returns error, NOT default's OAuth1
func TestTC3_2_AppBNoOAuth1ReturnsError(t *testing.T) {
	a, ts, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	// Remove OAuth1 from app-b
	ts.Apps["app-b"].OAuth1Token = nil

	a.WithAppName("app-b")
	_, err := a.GetOAuth1Header("GET", "https://api.x.com/2/users/me", nil)
	// Must return an error — should NOT silently fall back to app-a's OAuth1
	require.Error(t, err, "Expected error when app-b has no OAuth1 token")

	// Confirm app-a still has its OAuth1 token (wasn't cleared)
	a.WithAppName("app-a")
	headerA, err2 := a.GetOAuth1Header("GET", "https://api.x.com/2/users/me", nil)
	require.NoError(t, err2)
	assert.Contains(t, headerA, `oauth_consumer_key="ck-a"`)
}

// TC 3.3: app-b has OAuth2 for "alice-b" not "bob" → GetOAuth2TokenForApp returns nil for "bob",
// and does NOT return app-a's "bob" token either. Verifies the store lookup is app-scoped.
func TestTC3_3_AppBNoOAuth2ForBob(t *testing.T) {
	a, ts, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	// Give app-a a "bob" token to confirm it is NOT returned for app-b
	ts.Apps["app-a"].OAuth2Tokens["bob"] = store.Token{
		Type: store.OAuth2TokenType,
		OAuth2: &store.OAuth2Token{
			AccessToken:    "oauth2-token-bob-a",
			RefreshToken:   "refresh-bob-a",
			ExpirationTime: futureExpiry(),
		},
	}

	a.WithAppName("app-b")

	// Store-level check: GetOAuth2TokenForApp("app-b", "bob") must return nil
	tok := ts.GetOAuth2TokenForApp(a.appName, "bob")
	assert.Nil(t, tok, "app-b must not have a token for 'bob'")

	// app-a has one; confirm it is NOT leaked via appName scoping
	tokA := ts.GetOAuth2TokenForApp("app-a", "bob")
	require.NotNil(t, tokA)
	assert.Equal(t, "oauth2-token-bob-a", tokA.OAuth2.AccessToken)

	// The Auth.appName is correctly pointing at app-b, so RefreshOAuth2Token would error.
	// We test that directly (without triggering the full OAuth2Flow/browser redirect):
	_, err := a.RefreshOAuth2Token("bob")
	require.Error(t, err, "RefreshOAuth2Token must error when app-b has no 'bob' token")
}

// ─── 4. Boundary ────────────────────────────────────────────────────

// TC 4.1: Single app store → WithAppName("default") works normally
func TestTC4_1_SingleAppStore(t *testing.T) {
	tokenStore, tempDir := createTempTokenStore(t)
	defer os.RemoveAll(tempDir)

	// Set up the single default app with a bearer token
	tokenStore.Apps["default"].ClientID = "single-id"
	tokenStore.Apps["default"].ClientSecret = "single-secret"
	err := tokenStore.SaveBearerToken("single-bearer")
	require.NoError(t, err)

	a := &Auth{
		TokenStore:   tokenStore,
		clientID:     "single-id",
		clientSecret: "single-secret",
		appName:      "default",
	}

	a.WithAppName("default")
	assert.Equal(t, "single-id", a.clientID)
	assert.Equal(t, "single-secret", a.clientSecret)

	header, err := a.GetBearerTokenHeader()
	require.NoError(t, err)
	assert.Equal(t, "Bearer single-bearer", header)
}

// TC 4.2: Rapid back-and-forth switching (5+ times) → correct tokens each time
func TestTC4_2_RapidSwitching(t *testing.T) {
	a, _, tempDir := setupMultiAppAuth(t)
	defer os.RemoveAll(tempDir)

	rounds := []struct {
		app    string
		bearer string
	}{
		{"app-b", "bearer-b"},
		{"app-a", "bearer-a"},
		{"app-b", "bearer-b"},
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

// ─── 5. Domain-Specific ─────────────────────────────────────────────

// TC 5.1: WithAppName overwrites non-empty clientID/clientSecret (Bug #1)
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
