package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xdevplatform/xurl/errors"

	"gopkg.in/yaml.v3"
)

// Represents OAuth1 authentication tokens
type OAuth1Token struct {
	AccessToken    string `json:"access_token"`
	TokenSecret    string `json:"token_secret"`
	ConsumerKey    string `json:"consumer_key"`
	ConsumerSecret string `json:"consumer_secret"`
}

// Represents OAuth2 authentication tokens
type OAuth2Token struct {
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token"`
	ExpirationTime uint64 `json:"expiration_time"`
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
	Type   TokenType    `json:"type"`
	Bearer string       `json:"bearer,omitempty"`
	OAuth2 *OAuth2Token `json:"oauth2,omitempty"`
	OAuth1 *OAuth1Token `json:"oauth1,omitempty"`
}

// Manages authentication tokens
type TokenStore struct {
	OAuth2Tokens map[string]Token `json:"oauth2_tokens"`
	OAuth1Token  *Token           `json:"oauth1_tokens,omitempty"`
	BearerToken  *Token           `json:"bearer_token,omitempty"`
	FilePath     string           `json:"file_path"`
}

// Creates a new TokenStore
func NewTokenStore() *TokenStore {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error getting home directory:", err)
		homeDir = "."
	}

	filePath := filepath.Join(homeDir, ".xurl")

	store := &TokenStore{
		OAuth2Tokens: make(map[string]Token),
		FilePath:     filePath,
	}

	if _, err := os.Stat(filePath); err == nil {
		data, err := os.ReadFile(filePath)
		if err == nil {
			var loadedStore TokenStore
			if err := json.Unmarshal(data, &loadedStore); err == nil {
				store.OAuth2Tokens = loadedStore.OAuth2Tokens
				store.OAuth1Token = loadedStore.OAuth1Token
				store.BearerToken = loadedStore.BearerToken
			}
		}
	}

	// Either .xurl doesn't exist or we don't have OAuth1 tokens
	if store.OAuth1Token == nil || store.BearerToken == nil {
		twurlPath := filepath.Join(homeDir, ".twurlrc")
		if _, err := os.Stat(twurlPath); err == nil {
			if err := store.importFromTwurlrc(twurlPath); err != nil {
				fmt.Println("Error importing from .twurlrc:", err)
			}
		}
	}

	return store
}

// Imports tokens from a twurlrc file
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

	// Import the first OAuth1 tokens from twurlrc
	for _, consumerKeys := range twurlConfig.Profiles {
		for consumerKey, profile := range consumerKeys {
			if s.OAuth1Token == nil {
				s.OAuth1Token = &Token{
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
			s.BearerToken = &Token{
				Type:   BearerTokenType,
				Bearer: bearerToken,
			}
			break
		}
	}

	return s.saveToFile()
}

// aves a bearer token
func (s *TokenStore) SaveBearerToken(token string) error {
	s.BearerToken = &Token{
		Type:   BearerTokenType,
		Bearer: token,
	}
	return s.saveToFile()
}

// Saves an OAuth2 token
func (s *TokenStore) SaveOAuth2Token(username, accessToken, refreshToken string, expirationTime uint64) error {
	s.OAuth2Tokens[username] = Token{
		Type: OAuth2TokenType,
		OAuth2: &OAuth2Token{
			AccessToken:    accessToken,
			RefreshToken:   refreshToken,
			ExpirationTime: expirationTime,
		},
	}
	return s.saveToFile()
}

// Saves OAuth1 tokens
func (s *TokenStore) SaveOAuth1Tokens(accessToken, tokenSecret, consumerKey, consumerSecret string) error {
	s.OAuth1Token = &Token{
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

// Gets an OAuth2 token for a username
func (s *TokenStore) GetOAuth2Token(username string) *Token {
	if token, ok := s.OAuth2Tokens[username]; ok {
		return &token
	}
	return nil
}

// Gets the first OAuth2 token
func (s *TokenStore) GetFirstOAuth2Token() *Token {
	for _, token := range s.OAuth2Tokens {
		return &token
	}
	return nil
}

// Gets the OAuth1 tokens
func (s *TokenStore) GetOAuth1Tokens() *Token {
	return s.OAuth1Token
}

// Gets the bearer token
func (s *TokenStore) GetBearerToken() *Token {
	return s.BearerToken
}

// Clears an OAuth2 token for a username
func (s *TokenStore) ClearOAuth2Token(username string) error {
	delete(s.OAuth2Tokens, username)
	return s.saveToFile()
}

// Clears the OAuth1 tokens
func (s *TokenStore) ClearOAuth1Tokens() error {
	s.OAuth1Token = nil
	return s.saveToFile()
}

// Clears the bearer token
func (s *TokenStore) ClearBearerToken() error {
	s.BearerToken = nil
	return s.saveToFile()
}

// Clears all tokens
func (s *TokenStore) ClearAll() error {
	s.OAuth2Tokens = make(map[string]Token)
	s.OAuth1Token = nil
	s.BearerToken = nil
	return s.saveToFile()
}

// Gets all OAuth2 usernames
func (s *TokenStore) GetOAuth2Usernames() []string {
	usernames := make([]string, 0, len(s.OAuth2Tokens))
	for username := range s.OAuth2Tokens {
		usernames = append(usernames, username)
	}
	return usernames
}

// Checks if OAuth1 tokens exist
func (s *TokenStore) HasOAuth1Tokens() bool {
	return s.OAuth1Token != nil
}

// Checks if a bearer token exists
func (s *TokenStore) HasBearerToken() bool {
	return s.BearerToken != nil
}

// Saves the token store to a file
func (s *TokenStore) saveToFile() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return errors.NewJSONError(err)
	}

	err = os.WriteFile(s.FilePath, data, 0600)
	if err != nil {
		return errors.NewIOError(err)
	}

	return nil
}
