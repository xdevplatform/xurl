package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xurl/auth"
	"xurl/config"
	xurlErrors "xurl/errors"
	"xurl/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a temporary token store for testing
func createTempTokenStore(t *testing.T) (*store.TokenStore, string) {
	tempDir, err := os.MkdirTemp("", "xurl_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	tempFile := filepath.Join(tempDir, "tokens.json")
	tokenStore := &store.TokenStore{
		OAuth2Tokens: make(map[string]store.Token),
		FilePath:     tempFile,
	}

	return tokenStore, tempDir
}

// Create a mock Auth for testing
func createMockAuth(t *testing.T) (*auth.Auth, string) {
	cfg := &config.Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURI:  "http://localhost:8080/callback",
		AuthURL:      "https://x.com/i/oauth2/authorize",
		TokenURL:     "https://api.x.com/2/oauth2/token",
		APIBaseURL:   "https://api.x.com",
		InfoURL:      "https://api.x.com/2/users/me",
	}

	mockAuth := auth.NewAuth(cfg)
	tokenStore, tempDir := createTempTokenStore(t)

	err := tokenStore.SaveBearerToken("test-bearer-token")
	if err != nil {
		t.Fatalf("Failed to save bearer token: %v", err)
	}

	mockAuth.WithTokenStore(tokenStore)
	return mockAuth, tempDir
}

func TestNewApiClient(t *testing.T) {
	cfg := &config.Config{
		APIBaseURL: "https://api.x.com",
	}
	auth, tempDir := createMockAuth(t)
	defer os.RemoveAll(tempDir)

	client := NewApiClient(cfg, auth)

	assert.Equal(t, cfg.APIBaseURL, client.url, "URL should match config")
	assert.Equal(t, auth, client.auth, "Auth should be set correctly")
	assert.NotNil(t, client.client, "HTTP client should not be nil")
}

func TestBuildRequest(t *testing.T) {
	// Setup
	cfg := &config.Config{
		APIBaseURL: "https://api.x.com",
	}
	authMock, tempDir := createMockAuth(t)
	defer os.RemoveAll(tempDir)

	client := NewApiClient(cfg, authMock)

	tests := []struct {
		name       string
		method     string
		endpoint   string
		headers    []string
		data       string
		authType   string
		username   string
		wantMethod string
		wantURL    string
		wantErr    bool
	}{
		{
			name:       "GET user profile",
			method:     "GET",
			endpoint:   "/2/users/me",
			headers:    []string{"Accept: application/json"},
			data:       "",
			authType:   "",
			username:   "",
			wantMethod: "GET",
			wantURL:    "https://api.x.com/2/users/me",
			wantErr:    false,
		},
		{
			name:       "POST tweet",
			method:     "POST",
			endpoint:   "/2/tweets",
			headers:    []string{"Accept: application/json", "Authorization: Bearer test-token"},
			data:       `{"text":"Hello world!"}`,
			authType:   "oauth1",
			username:   "",
			wantMethod: "POST",
			wantURL:    "https://api.x.com/2/tweets",
			wantErr:    false,
		},
		{
			name:       "Absolute URL",
			method:     "GET",
			endpoint:   "https://api.x.com/2/tweets/search/stream",
			headers:    []string{"Authorization: Bearer test-token"},
			data:       "",
			authType:   "app",
			username:   "",
			wantMethod: "GET",
			wantURL:    "https://api.x.com/2/tweets/search/stream",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestOptions := RequestOptions{
				Method:   tt.method,
				Endpoint: tt.endpoint,
				Headers:  tt.headers,
				AuthType: tt.authType,
				Username: tt.username,
				Data:     tt.data,
			}

			req, err := client.BuildRequest(requestOptions)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantMethod, req.Method)
			assert.Equal(t, tt.wantURL, req.URL.String())

			for _, header := range tt.headers {
				parts := strings.Split(header, ": ")
				require.Len(t, parts, 2, "Invalid header format: %s", header)

				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])

				assert.Equal(t, value, req.Header.Get(key))
			}

			assert.Equal(t, "xurl/dev", req.Header.Get("User-Agent"))

			if tt.method == "POST" && tt.data != "" {
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
			}
		})
	}
}

func TestSendRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2/users/me" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"id":"12345","name":"Test User","username":"testuser"}}`))
			return
		}

		if r.URL.Path == "/2/tweets" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"data":{"id":"67890","text":"Hello world!"}}`))
			return
		}

		if r.URL.Path == "/2/tweets/search/recent" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errors":[{"message":"Invalid query","code":400}]}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.Config{
		APIBaseURL: server.URL,
	}
	authMock, tempDir := createMockAuth(t)
	defer os.RemoveAll(tempDir)
	client := NewApiClient(cfg, authMock)

	// Test successful GET request
	t.Run("Get user profile", func(t *testing.T) {
		options := RequestOptions{
			Method:   "GET",
			Endpoint: "/2/users/me",
			Headers:  []string{"Authorization: Bearer test-token"},
			Data:     "",
			AuthType: "",
			Username: "",
			Verbose:  false,
		}

		resp, err := client.SendRequest(options)

		require.NoError(t, err)

		var result map[string]any
		err = json.Unmarshal(resp, &result)
		require.NoError(t, err, "Failed to parse response")

		data, ok := result["data"].(map[string]any)
		require.True(t, ok, "Expected data object in response")

		assert.Equal(t, "testuser", data["username"], "Username should match")
	})

	// Test successful POST request
	t.Run("Post tweet", func(t *testing.T) {
		options := RequestOptions{
			Method:   "POST",
			Endpoint: "/2/tweets",
			Headers:  []string{"Authorization: Bearer test-token"},
			Data:     `{"text":"Hello world!"}`,
			AuthType: "",
			Username: "",
			Verbose:  false,
		}

		resp, err := client.SendRequest(options)

		require.NoError(t, err)

		var result map[string]any
		err = json.Unmarshal(resp, &result)
		require.NoError(t, err, "Failed to parse response")

		data, ok := result["data"].(map[string]any)
		require.True(t, ok, "Expected data object in response")

		assert.Equal(t, "Hello world!", data["text"], "Tweet text should match")
	})

	t.Run("Error response", func(t *testing.T) {
		options := RequestOptions{
			Method:   "GET",
			Endpoint: "/2/tweets/search/recent",
			Headers:  []string{"Authorization: Bearer test-token"},
			Data:     "",
			AuthType: "",
			Username: "",
			Verbose:  false,
		}

		resp, err := client.SendRequest(options)

		assert.Error(t, err, "Expected an error")
		assert.Nil(t, resp, "Response should be nil")
		assert.True(t, xurlErrors.IsAPIError(err), "Expected API error")
	})
}

func TestGetAuthHeader(t *testing.T) {
	cfg := &config.Config{
		APIBaseURL: "https://api.x.com",
	}

	t.Run("No auth set", func(t *testing.T) {
		client := NewApiClient(cfg, nil)

		_, err := client.getAuthHeader("GET", "https://api.x.com/2/users/me", "", "")

		assert.Error(t, err, "Expected an error")
		assert.True(t, xurlErrors.IsAuthError(err), "Expected auth error")
	})

	t.Run("Invalid auth type", func(t *testing.T) {
		authMock, tempDir := createMockAuth(t)
		defer os.RemoveAll(tempDir)
		client := NewApiClient(cfg, authMock)

		_, err := client.getAuthHeader("GET", "https://api.x.com/2/users/me", "invalid", "")

		assert.Error(t, err, "Expected an error")
		assert.True(t, xurlErrors.IsAuthError(err), "Expected auth error")
	})
}

func TestStreamRequest(t *testing.T) {
	// This is a basic test for the StreamRequest method
	// A more comprehensive test would require mocking the streaming response

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2/tweets/search/stream" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// In a real test, we would write multiple JSON objects with flushing
			// but for this simple test, we'll just close the connection
			return
		}

		if r.URL.Path == "/2/tweets/search/stream/error" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errors":[{"message":"Invalid rule","code":400}]}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.Config{
		APIBaseURL: server.URL,
	}
	authMock, tempDir := createMockAuth(t)
	defer os.RemoveAll(tempDir)
	client := NewApiClient(cfg, authMock)

	t.Run("Stream error response", func(t *testing.T) {
		options := RequestOptions{
			Method:   "GET",
			Endpoint: "/2/tweets/search/stream/error",
			Headers:  []string{"Authorization: Bearer test-token"},
			Data:     "",
			AuthType: "",
			Username: "",
			Verbose:  false,
		}

		err := client.StreamRequest(options)

		assert.Error(t, err, "Expected an error")
		assert.True(t, xurlErrors.IsAPIError(err), "Expected API error")
	})
}
