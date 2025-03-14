package config

import (
	"fmt"
	"os"
)

// Config holds the application configuration
type Config struct {
	// OAuth2 client tokens
	ClientID     string
	ClientSecret string
	// OAuth2 PKCE flow urls
	RedirectURI string
	AuthURL     string
	TokenURL    string
	// API base url
	APIBaseURL string
	// API user info url
	InfoURL string
}

// NewConfig creates a new Config from environment variables
func NewConfig() *Config {
	clientID := getEnvOrDefault("CLIENT_ID", "")
	clientSecret := getEnvOrDefault("CLIENT_SECRET", "")
	redirectURI := getEnvOrDefault("REDIRECT_URI", "http://localhost:8080/callback")
	authURL := getEnvOrDefault("AUTH_URL", "https://x.com/i/oauth2/authorize")
	tokenURL := getEnvOrDefault("TOKEN_URL", "https://api.x.com/2/oauth2/token")
	apiBaseURL := getEnvOrDefault("API_BASE_URL", "https://api.x.com")
	infoURL := getEnvOrDefault("INFO_URL", fmt.Sprintf("%s/2/users/me", apiBaseURL))

	return &Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURI,
		AuthURL:      authURL,
		TokenURL:     tokenURL,
		APIBaseURL:   apiBaseURL,
		InfoURL:      infoURL,
	}
}

// Helper function to get environment variable with default value
func getEnvOrDefault(key, defaultValue string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	return value
} 