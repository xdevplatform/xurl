package config

import (
	"fmt"
	"os"

	"github.com/xdevplatform/xurl/store"
)

const DefaultRedirectURI = "http://localhost:8080/callback"

// Config holds the application configuration
type Config struct {
	// OAuth2 client tokens (may come from env vars or the active app in .xurl)
	ClientID     string
	ClientSecret string
	// OAuth2 PKCE flow urls
	RedirectURI string
	// RedirectURIFromEnv tracks whether REDIRECT_URI came from the environment.
	RedirectURIFromEnv bool
	AuthURL            string
	TokenURL           string
	// API base url
	APIBaseURL string
	// API user info url
	InfoURL string
	// AppName is the explicit --app override; empty means "use default".
	AppName string
}

// NewConfig creates a new Config from environment variables
func NewConfig() *Config {
	return NewConfigForApp("")
}

// NewConfigForApp creates a Config for the given app name.
func NewConfigForApp(appName string) *Config {
	clientID := getEnvOrDefault("CLIENT_ID", "")
	clientSecret := getEnvOrDefault("CLIENT_SECRET", "")
	redirectURI, redirectURIFromEnv, _ := ResolveRedirectURI(appName)
	authURL := getEnvOrDefault("AUTH_URL", "https://x.com/i/oauth2/authorize")
	tokenURL := getEnvOrDefault("TOKEN_URL", "https://api.x.com/2/oauth2/token")
	apiBaseURL := getEnvOrDefault("API_BASE_URL", "https://api.x.com")
	infoURL := getEnvOrDefault("INFO_URL", fmt.Sprintf("%s/2/users/me", apiBaseURL))

	return &Config{
		ClientID:           clientID,
		ClientSecret:       clientSecret,
		RedirectURI:        redirectURI,
		RedirectURIFromEnv: redirectURIFromEnv,
		AuthURL:            authURL,
		TokenURL:           tokenURL,
		APIBaseURL:         apiBaseURL,
		InfoURL:            infoURL,
		AppName:            appName,
	}
}

// ResolveRedirectURI resolves the effective redirect URI for an app.
// Precedence: REDIRECT_URI env var, then stored app config, then built-in default.
func ResolveRedirectURI(appName string) (value string, fromEnv bool, source string) {
	if value, ok := os.LookupEnv("REDIRECT_URI"); ok {
		return value, true, "REDIRECT_URI environment variable"
	}

	ts := store.NewTokenStore()
	app := ts.ResolveApp(appName)
	if app != nil && app.RedirectURI != "" {
		return app.RedirectURI, false, "app config"
	}

	return DefaultRedirectURI, false, "built-in default"
}

// Helper function to get environment variable with default value
func getEnvOrDefault(key, defaultValue string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	return value
}
