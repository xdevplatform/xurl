package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/xdevplatform/xurl/errors"

	"gopkg.in/yaml.v3"
)

// ─── Token types ────────────────────────────────────────────────────

// Represents OAuth1 authentication tokens
type OAuth1Token struct {
	AccessToken    string `yaml:"access_token" json:"access_token"`
	TokenSecret    string `yaml:"token_secret" json:"token_secret"`
	ConsumerKey    string `yaml:"consumer_key" json:"consumer_key"`
	ConsumerSecret string `yaml:"consumer_secret" json:"consumer_secret"`
}

// Represents OAuth2 authentication tokens
type OAuth2Token struct {
	AccessToken    string `yaml:"access_token" json:"access_token"`
	RefreshToken   string `yaml:"refresh_token" json:"refresh_token"`
	ExpirationTime uint64 `yaml:"expiration_time" json:"expiration_time"`
}

// Represents the type of token
type TokenType string

const (
	BearerTokenType TokenType = "bearer"
	OAuth2TokenType TokenType = "oauth2"
	OAuth1TokenType TokenType = "oauth1"
)

// Token represents an authentication token
type Token struct {
	Type   TokenType    `yaml:"type" json:"type"`
	Bearer string       `yaml:"bearer,omitempty" json:"bearer,omitempty"`
	OAuth2 *OAuth2Token `yaml:"oauth2,omitempty" json:"oauth2,omitempty"`
	OAuth1 *OAuth1Token `yaml:"oauth1,omitempty" json:"oauth1,omitempty"`
}

// ─── App ────────────────────────────────────────────────────────────

// App holds the credentials and tokens for a single registered X API application.
type App struct {
	ClientID     string           `yaml:"client_id"`
	ClientSecret string           `yaml:"client_secret"`
	RedirectURI  string           `yaml:"redirect_uri,omitempty"`
	DefaultUser  string           `yaml:"default_user,omitempty"`
	OAuth2Tokens map[string]Token `yaml:"oauth2_tokens,omitempty"`
	OAuth1Token  *Token           `yaml:"oauth1_token,omitempty"`
	BearerToken  *Token           `yaml:"bearer_token,omitempty"`
}

// ─── On-disk YAML structure ─────────────────────────────────────────

// storeFile is the serialised YAML layout of ~/.xurl
type storeFile struct {
	Apps       map[string]*App `yaml:"apps"`
	DefaultApp string          `yaml:"default_app"`
}

// ─── Legacy JSON structure (for migration) ──────────────────────────

type legacyStore struct {
	OAuth2Tokens map[string]Token `json:"oauth2_tokens"`
	OAuth1Token  *Token           `json:"oauth1_tokens,omitempty"`
	BearerToken  *Token           `json:"bearer_token,omitempty"`
}

// ─── TokenStore ─────────────────────────────────────────────────────

// Manages authentication tokens across multiple apps.
type TokenStore struct {
	Apps       map[string]*App `yaml:"apps"`
	DefaultApp string          `yaml:"default_app"`
	FilePath   string          `yaml:"-"`
}

func resolveHomeDir() string {
	if homeDir := os.Getenv("HOME"); homeDir != "" {
		return homeDir
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error getting home directory:", err)
		return "."
	}

	return homeDir
}

// Creates a new TokenStore, loading from ~/.xurl (auto-migrating legacy JSON).
func NewTokenStore() *TokenStore {
	return NewTokenStoreWithCredentials("", "")
}

// NewTokenStoreWithCredentials creates a TokenStore and backfills the given
// client credentials into any app that was migrated without them (i.e. legacy
// JSON migration where CLIENT_ID / CLIENT_SECRET came from env vars).
func NewTokenStoreWithCredentials(clientID, clientSecret string) *TokenStore {
	homeDir := resolveHomeDir()
	filePath := filepath.Join(homeDir, ".xurl")

	store := &TokenStore{
		Apps:     make(map[string]*App),
		FilePath: filePath,
	}

	if _, err := os.Stat(filePath); err == nil {
		data, err := os.ReadFile(filePath)
		if err == nil {
			store.loadFromData(data)
		}
	}

	// Backfill credentials into any app that has tokens but no client ID/secret
	if clientID != "" || clientSecret != "" {
		dirty := false
		for _, app := range store.Apps {
			hasTokens := len(app.OAuth2Tokens) > 0 || app.OAuth1Token != nil || app.BearerToken != nil
			if hasTokens && app.ClientID == "" && clientID != "" {
				app.ClientID = clientID
				dirty = true
			}
			if hasTokens && app.ClientSecret == "" && clientSecret != "" {
				app.ClientSecret = clientSecret
				dirty = true
			}
		}
		if dirty {
			_ = store.saveToFile()
		}
	}

	// Import from .twurlrc if we have no apps or the default app is missing OAuth1/Bearer
	app := store.activeApp()
	if app == nil || app.OAuth1Token == nil || app.BearerToken == nil {
		twurlPath := filepath.Join(homeDir, ".twurlrc")
		if _, err := os.Stat(twurlPath); err == nil {
			if err := store.importFromTwurlrc(twurlPath); err != nil {
				fmt.Println("Error importing from .twurlrc:", err)
			}
		}
	}

	return store
}

// loadFromData tries YAML first, then falls back to legacy JSON migration.
func (s *TokenStore) loadFromData(data []byte) {
	// Try new YAML format first
	var sf storeFile
	if err := yaml.Unmarshal(data, &sf); err == nil && len(sf.Apps) > 0 {
		s.Apps = sf.Apps
		s.DefaultApp = sf.DefaultApp
		// Ensure all apps have initialised maps
		for _, app := range s.Apps {
			if app.OAuth2Tokens == nil {
				app.OAuth2Tokens = make(map[string]Token)
			}
		}
		return
	}

	// Fall back to legacy JSON
	var legacy legacyStore
	if err := json.Unmarshal(data, &legacy); err == nil {
		oauth2 := legacy.OAuth2Tokens
		if oauth2 == nil {
			oauth2 = make(map[string]Token)
		}
		s.Apps["default"] = &App{
			OAuth2Tokens: oauth2,
			OAuth1Token:  legacy.OAuth1Token,
			BearerToken:  legacy.BearerToken,
		}
		s.DefaultApp = "default"
		// Persist in new YAML format immediately
		_ = s.saveToFile()
	}
}

// ─── App management ─────────────────────────────────────────────────

// AddApp registers a new application. If it's the only app it becomes default.
func (s *TokenStore) AddApp(name, clientID, clientSecret string) error {
	if _, exists := s.Apps[name]; exists {
		return errors.NewTokenStoreError(fmt.Sprintf("app %q already exists", name))
	}
	s.Apps[name] = &App{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		OAuth2Tokens: make(map[string]Token),
	}
	if len(s.Apps) == 1 {
		s.DefaultApp = name
	}
	return s.saveToFile()
}

// UpdateApp updates the credentials of an existing application.
func (s *TokenStore) UpdateApp(name, clientID, clientSecret string) error {
	app, exists := s.Apps[name]
	if !exists {
		return errors.NewTokenStoreError(fmt.Sprintf("app %q not found", name))
	}
	if clientID != "" {
		app.ClientID = clientID
	}
	if clientSecret != "" {
		app.ClientSecret = clientSecret
	}
	return s.saveToFile()
}

// SetAppRedirectURI sets the redirect URI of an existing app.
func (s *TokenStore) SetAppRedirectURI(name, redirectURI string) error {
	app, exists := s.Apps[name]
	if !exists {
		return errors.NewTokenStoreError(fmt.Sprintf("app %q not found", name))
	}
	app.RedirectURI = redirectURI
	return s.saveToFile()
}

// GetAppRedirectURI gets the stored redirect URI for an existing app.
func (s *TokenStore) GetAppRedirectURI(name string) (string, error) {
	app, exists := s.Apps[name]
	if !exists {
		return "", errors.NewTokenStoreError(fmt.Sprintf("app %q not found", name))
	}
	return app.RedirectURI, nil
}

// RemoveApp removes a registered application and its tokens.
func (s *TokenStore) RemoveApp(name string) error {
	if _, exists := s.Apps[name]; !exists {
		return errors.NewTokenStoreError(fmt.Sprintf("app %q not found", name))
	}
	delete(s.Apps, name)
	if s.DefaultApp == name {
		s.DefaultApp = ""
		// Pick the first remaining app as default
		for n := range s.Apps {
			s.DefaultApp = n
			break
		}
	}
	return s.saveToFile()
}

// SetDefaultApp sets the default application by name.
func (s *TokenStore) SetDefaultApp(name string) error {
	if _, exists := s.Apps[name]; !exists {
		return errors.NewTokenStoreError(fmt.Sprintf("app %q not found", name))
	}
	s.DefaultApp = name
	return s.saveToFile()
}

// ListApps returns sorted app names.
func (s *TokenStore) ListApps() []string {
	names := make([]string, 0, len(s.Apps))
	for name := range s.Apps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetApp returns an app by name.
func (s *TokenStore) GetApp(name string) *App {
	return s.Apps[name]
}

// SetDefaultUser sets the default OAuth2 user for the named (or default) app.
func (s *TokenStore) SetDefaultUser(appName, username string) error {
	app := s.ResolveApp(appName)
	if _, ok := app.OAuth2Tokens[username]; !ok {
		return errors.NewTokenStoreError(fmt.Sprintf("user %q not found in app", username))
	}
	app.DefaultUser = username
	return s.saveToFile()
}

// GetDefaultUser returns the default OAuth2 user for the named (or default) app.
func (s *TokenStore) GetDefaultUser(appName string) string {
	app := s.ResolveApp(appName)
	return app.DefaultUser
}

// GetDefaultApp returns the default app name.
func (s *TokenStore) GetDefaultApp() string {
	return s.DefaultApp
}

// GetActiveAppName returns the name of the active app (explicit or default).
func (s *TokenStore) GetActiveAppName(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return s.DefaultApp
}

// activeApp returns the current default App, or nil.
func (s *TokenStore) activeApp() *App {
	return s.Apps[s.DefaultApp]
}

// activeAppOrCreate returns the active app; creates "default" if none exist.
func (s *TokenStore) activeAppOrCreate() *App {
	app := s.activeApp()
	if app != nil {
		return app
	}
	// Auto-create a "default" app so callers don't crash.
	s.Apps["default"] = &App{
		OAuth2Tokens: make(map[string]Token),
	}
	if s.DefaultApp == "" {
		s.DefaultApp = "default"
	}
	return s.Apps["default"]
}

// ResolveApp returns the app for the given name, or the default app.
func (s *TokenStore) ResolveApp(name string) *App {
	if name != "" {
		if app, ok := s.Apps[name]; ok {
			return app
		}
	}
	return s.activeAppOrCreate()
}

// ─── Twurlrc import ─────────────────────────────────────────────────

// Imports tokens from a twurlrc file into the active app.
func (s *TokenStore) importFromTwurlrc(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return errors.NewIOError(err)
	}

	var twurlConfig struct {
		Profiles map[string]map[string]struct {
			Username       string `yaml:"username"`
			ConsumerKey    string `yaml:"consumer_key"`
			ConsumerSecret string `yaml:"consumer_secret"`
			Token          string `yaml:"token"`
			Secret         string `yaml:"secret"`
		} `yaml:"profiles"`
		Configuration struct {
			DefaultProfile []string `yaml:"default_profile"`
		} `yaml:"configuration"`
		BearerTokens map[string]string `yaml:"bearer_tokens"`
	}

	if err := yaml.Unmarshal(data, &twurlConfig); err != nil {
		return errors.NewJSONError(err)
	}

	app := s.activeAppOrCreate()

	// Import the first OAuth1 tokens from twurlrc
	for _, consumerKeys := range twurlConfig.Profiles {
		for consumerKey, profile := range consumerKeys {
			if app.OAuth1Token == nil {
				app.OAuth1Token = &Token{
					Type: OAuth1TokenType,
					OAuth1: &OAuth1Token{
						AccessToken:    profile.Token,
						TokenSecret:    profile.Secret,
						ConsumerKey:    consumerKey,
						ConsumerSecret: profile.ConsumerSecret,
					},
				}
			}
			break
		}
		break
	}

	// Import the first bearer token from twurlrc
	if len(twurlConfig.BearerTokens) > 0 {
		for _, bearerToken := range twurlConfig.BearerTokens {
			app.BearerToken = &Token{
				Type:   BearerTokenType,
				Bearer: bearerToken,
			}
			break
		}
	}

	return s.saveToFile()
}

// ─── Token operations (delegate to active / named app) ──────────────

// SaveBearerToken saves a bearer token into the resolved app.
func (s *TokenStore) SaveBearerToken(token string) error {
	return s.SaveBearerTokenForApp("", token)
}

// SaveBearerTokenForApp saves a bearer token into the named app.
func (s *TokenStore) SaveBearerTokenForApp(appName, token string) error {
	app := s.ResolveApp(appName)
	app.BearerToken = &Token{
		Type:   BearerTokenType,
		Bearer: token,
	}
	return s.saveToFile()
}

// SaveOAuth2Token saves an OAuth2 token into the resolved app.
func (s *TokenStore) SaveOAuth2Token(username, accessToken, refreshToken string, expirationTime uint64) error {
	return s.SaveOAuth2TokenForApp("", username, accessToken, refreshToken, expirationTime)
}

// SaveOAuth2TokenForApp saves an OAuth2 token into the named app.
func (s *TokenStore) SaveOAuth2TokenForApp(appName, username, accessToken, refreshToken string, expirationTime uint64) error {
	app := s.ResolveApp(appName)
	if app.OAuth2Tokens == nil {
		app.OAuth2Tokens = make(map[string]Token)
	}
	app.OAuth2Tokens[username] = Token{
		Type: OAuth2TokenType,
		OAuth2: &OAuth2Token{
			AccessToken:    accessToken,
			RefreshToken:   refreshToken,
			ExpirationTime: expirationTime,
		},
	}
	return s.saveToFile()
}

// SaveOAuth1Tokens saves OAuth1 tokens into the resolved app.
func (s *TokenStore) SaveOAuth1Tokens(accessToken, tokenSecret, consumerKey, consumerSecret string) error {
	return s.SaveOAuth1TokensForApp("", accessToken, tokenSecret, consumerKey, consumerSecret)
}

// SaveOAuth1TokensForApp saves OAuth1 tokens into the named app.
func (s *TokenStore) SaveOAuth1TokensForApp(appName, accessToken, tokenSecret, consumerKey, consumerSecret string) error {
	app := s.ResolveApp(appName)
	app.OAuth1Token = &Token{
		Type: OAuth1TokenType,
		OAuth1: &OAuth1Token{
			AccessToken:    accessToken,
			TokenSecret:    tokenSecret,
			ConsumerKey:    consumerKey,
			ConsumerSecret: consumerSecret,
		},
	}
	return s.saveToFile()
}

// GetOAuth2Token gets an OAuth2 token for a username from the resolved app.
func (s *TokenStore) GetOAuth2Token(username string) *Token {
	return s.GetOAuth2TokenForApp("", username)
}

// GetOAuth2TokenForApp gets an OAuth2 token for a username from the named app.
func (s *TokenStore) GetOAuth2TokenForApp(appName, username string) *Token {
	app := s.ResolveApp(appName)
	if token, ok := app.OAuth2Tokens[username]; ok {
		return &token
	}
	return nil
}

// GetFirstOAuth2Token gets the first OAuth2 token from the resolved app.
func (s *TokenStore) GetFirstOAuth2Token() *Token {
	return s.GetFirstOAuth2TokenForApp("")
}

// GetFirstOAuth2TokenRecordForApp gets the preferred OAuth2 token key and token from the named app.
func (s *TokenStore) GetFirstOAuth2TokenRecordForApp(appName string) (string, *Token) {
	app := s.ResolveApp(appName)
	if app.DefaultUser != "" {
		if token, ok := app.OAuth2Tokens[app.DefaultUser]; ok {
			return app.DefaultUser, &token
		}
	}

	usernames := s.GetOAuth2UsernamesForApp(appName)
	for _, username := range usernames {
		if username == "" {
			continue
		}
		if token, ok := app.OAuth2Tokens[username]; ok {
			return username, &token
		}
	}

	if token, ok := app.OAuth2Tokens[""]; ok {
		return "", &token
	}

	return "", nil
}

// GetFirstOAuth2TokenForApp gets the default user's token, or the first OAuth2 token from the named app.
func (s *TokenStore) GetFirstOAuth2TokenForApp(appName string) *Token {
	_, token := s.GetFirstOAuth2TokenRecordForApp(appName)
	return token
}

// GetOAuth1Tokens gets OAuth1 tokens from the resolved app.
func (s *TokenStore) GetOAuth1Tokens() *Token {
	return s.GetOAuth1TokensForApp("")
}

// GetOAuth1TokensForApp gets OAuth1 tokens from the named app.
func (s *TokenStore) GetOAuth1TokensForApp(appName string) *Token {
	app := s.ResolveApp(appName)
	return app.OAuth1Token
}

// GetBearerToken gets the bearer token from the resolved app.
func (s *TokenStore) GetBearerToken() *Token {
	return s.GetBearerTokenForApp("")
}

// GetBearerTokenForApp gets the bearer token from the named app.
func (s *TokenStore) GetBearerTokenForApp(appName string) *Token {
	app := s.ResolveApp(appName)
	return app.BearerToken
}

// ClearOAuth2Token clears an OAuth2 token for a username from the resolved app.
func (s *TokenStore) ClearOAuth2Token(username string) error {
	return s.ClearOAuth2TokenForApp("", username)
}

// ClearOAuth2TokenForApp clears an OAuth2 token for a username from the named app.
func (s *TokenStore) ClearOAuth2TokenForApp(appName, username string) error {
	app := s.ResolveApp(appName)
	delete(app.OAuth2Tokens, username)
	return s.saveToFile()
}

// ClearOAuth1Tokens clears OAuth1 tokens from the resolved app.
func (s *TokenStore) ClearOAuth1Tokens() error {
	return s.ClearOAuth1TokensForApp("")
}

// ClearOAuth1TokensForApp clears OAuth1 tokens from the named app.
func (s *TokenStore) ClearOAuth1TokensForApp(appName string) error {
	app := s.ResolveApp(appName)
	app.OAuth1Token = nil
	return s.saveToFile()
}

// ClearBearerToken clears the bearer token from the resolved app.
func (s *TokenStore) ClearBearerToken() error {
	return s.ClearBearerTokenForApp("")
}

// ClearBearerTokenForApp clears the bearer token from the named app.
func (s *TokenStore) ClearBearerTokenForApp(appName string) error {
	app := s.ResolveApp(appName)
	app.BearerToken = nil
	return s.saveToFile()
}

// ClearAll clears all tokens from the resolved app.
func (s *TokenStore) ClearAll() error {
	return s.ClearAllForApp("")
}

// ClearAllForApp clears all tokens from the named app.
func (s *TokenStore) ClearAllForApp(appName string) error {
	app := s.ResolveApp(appName)
	app.OAuth2Tokens = make(map[string]Token)
	app.OAuth1Token = nil
	app.BearerToken = nil
	return s.saveToFile()
}

// GetOAuth2Usernames gets all OAuth2 usernames from the resolved app.
func (s *TokenStore) GetOAuth2Usernames() []string {
	return s.GetOAuth2UsernamesForApp("")
}

// GetOAuth2UsernamesForApp gets all OAuth2 usernames from the named app.
func (s *TokenStore) GetOAuth2UsernamesForApp(appName string) []string {
	app := s.ResolveApp(appName)
	usernames := make([]string, 0, len(app.OAuth2Tokens))
	for username := range app.OAuth2Tokens {
		usernames = append(usernames, username)
	}
	sort.Strings(usernames)
	return usernames
}

// HasOAuth1Tokens checks if OAuth1 tokens exist in the resolved app.
func (s *TokenStore) HasOAuth1Tokens() bool {
	app := s.activeApp()
	return app != nil && app.OAuth1Token != nil
}

// HasBearerToken checks if a bearer token exists in the resolved app.
func (s *TokenStore) HasBearerToken() bool {
	app := s.activeApp()
	return app != nil && app.BearerToken != nil
}

// ─── Persistence ────────────────────────────────────────────────────

// Saves the token store to ~/.xurl in YAML format.
func (s *TokenStore) saveToFile() error {
	sf := storeFile{
		Apps:       s.Apps,
		DefaultApp: s.DefaultApp,
	}
	data, err := yaml.Marshal(&sf)
	if err != nil {
		return errors.NewJSONError(err)
	}

	err = os.WriteFile(s.FilePath, data, 0600)
	if err != nil {
		return errors.NewIOError(err)
	}

	return nil
}
