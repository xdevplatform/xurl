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
	
	// Add a test bearer token
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
	
	if client.url != cfg.APIBaseURL {
		t.Errorf("Expected URL to be %s, got %s", cfg.APIBaseURL, client.url)
	}
	
	if client.auth != auth {
		t.Errorf("Expected auth to be set correctly")
	}
	
	if client.client == nil {
		t.Errorf("HTTP client should not be nil")
	}
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
			req, err := client.BuildRequest(tt.method, tt.endpoint, tt.headers, tt.data, tt.authType, tt.username)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if err != nil {
				return
			}
			
			if req.Method != tt.wantMethod {
				t.Errorf("BuildRequest() method = %v, want %v", req.Method, tt.wantMethod)
			}
			
			if req.URL.String() != tt.wantURL {
				t.Errorf("BuildRequest() URL = %v, want %v", req.URL.String(), tt.wantURL)
			}
			
			for _, header := range tt.headers {
				parts := strings.Split(header, ": ")
				if len(parts) != 2 {
					t.Errorf("Invalid header format: %s", header)
					continue
				}
				
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				
				if req.Header.Get(key) != value {
					t.Errorf("BuildRequest() header %s = %s, want %s", key, req.Header.Get(key), value)
				}
			}	
			
			if tt.method == "POST" && tt.data != "" {
				contentType := req.Header.Get("Content-Type")
				if contentType != "application/json" {
					t.Errorf("Expected Content-Type header to be application/json, got %s", contentType)
				}
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
	
	// Setup client
	cfg := &config.Config{
		APIBaseURL: server.URL,
	}
	authMock, tempDir := createMockAuth(t)
	defer os.RemoveAll(tempDir)
	client := NewApiClient(cfg, authMock)
	
	// Test successful GET request
	t.Run("Get user profile", func(t *testing.T) {
		resp, err := client.SendRequest("GET", "/2/users/me", []string{"Authorization: Bearer test-token"}, "", "", "", false)
		
		if err != nil {
			t.Errorf("SendRequest() error = %v", err)
			return
		}
		
		var result map[string]interface{}
		if e := json.Unmarshal(resp, &result); e != nil {
			t.Errorf("Failed to parse response: %v", e)
			return
		}
		
		data, ok := result["data"].(map[string]interface{})
		if !ok {
			t.Errorf("Expected data object in response")
			return
		}
		
		if username, ok := data["username"]; !ok || username != "testuser" {
			t.Errorf("Expected username 'testuser', got %v", username)
		}
	})
	
	// Test successful POST request
	t.Run("Post tweet", func(t *testing.T) {
		resp, err := client.SendRequest("POST", "/2/tweets", []string{"Authorization: Bearer test-token"}, `{"text":"Hello world!"}`, "", "", false)
		
		if err != nil {
			t.Errorf("SendRequest() error = %v", err)
			return
		}
		
		var result map[string]interface{}
		if e := json.Unmarshal(resp, &result); e != nil {
			t.Errorf("Failed to parse response: %v", e)
			return
		}
		
		data, ok := result["data"].(map[string]interface{})
		if !ok {
			t.Errorf("Expected data object in response")
			return
		}
		
		if text, ok := data["text"]; !ok || text != "Hello world!" {
			t.Errorf("Expected text 'Hello world!', got %v", text)
		}
	})
	
	// Test error response
	t.Run("Error response", func(t *testing.T) {
		resp, err := client.SendRequest("GET", "/2/tweets/search/recent", []string{"Authorization: Bearer test-token"}, "", "", "", false)
		
		if err == nil {
			t.Errorf("SendRequest() expected error, got nil")
			return
		}
		
		if resp != nil {
			t.Errorf("SendRequest() expected nil response, got %v", resp)
		}
		
		if !xurlErrors.IsAPIError(err) {
			t.Errorf("Expected API error, got %v", err)
		}
	})
}

func TestGetAuthHeader(t *testing.T) {
	cfg := &config.Config{
		APIBaseURL: "https://api.x.com",
	}
	
	t.Run("No auth set", func(t *testing.T) {
		client := NewApiClient(cfg, nil)
		
		_, err := client.GetAuthHeader("GET", "https://api.x.com/2/users/me", "", "")
		
		if err == nil {
			t.Errorf("GetAuthHeader() expected error, got nil")
		}
		
		if !xurlErrors.IsAuthError(err) {
			t.Errorf("Expected auth error, got %v", err)
		}
	})
	
	t.Run("Invalid auth type", func(t *testing.T) {
		authMock, tempDir := createMockAuth(t)
		defer os.RemoveAll(tempDir)
		client := NewApiClient(cfg, authMock)
		
		_, err := client.GetAuthHeader("GET", "https://api.x.com/2/users/me", "invalid", "")
		
		if err == nil {
			t.Errorf("GetAuthHeader() expected error, got nil")
		}
		
		if !xurlErrors.IsAuthError(err) {
			t.Errorf("Expected auth error, got %v", err)
		}
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
		err := client.StreamRequest("GET", "/2/tweets/search/stream/error", []string{"Authorization: Bearer test-token"}, "", "", "", false)
		
		if err == nil {
			t.Errorf("StreamRequest() expected error, got nil")
			return
		}
		
		// Check if it's an API error
		if !xurlErrors.IsAPIError(err) {
			t.Errorf("Expected API error, got %v", err)
		}
	})
}
