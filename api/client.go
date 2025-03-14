package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xurl/auth"
	"xurl/config"
	xurlErrors "xurl/errors"
	"xurl/store"
)

// ApiClient handles API requests
type ApiClient struct {
	url  string
	client *http.Client
	auth *auth.Auth
}

// NewApiClient creates a new ApiClient
func NewApiClient(config *config.Config, auth *auth.Auth) *ApiClient {
	return &ApiClient{
		url:    config.APIBaseURL,
		client: &http.Client{Timeout: 30 * time.Second},
		auth:   auth,
	}
}

// ValidateAndRefreshOAuth2Token validates and refreshes an OAuth2 token if needed
func (c *ApiClient) ValidateAndRefreshOAuth2Token(token *store.Token, username string) (string, error) {
	if token == nil || token.OAuth2 == nil {
		return "", xurlErrors.NewAuthError("TokenNotFound", errors.New("oauth2 token not found"))
	}

	currentTime := time.Now().Unix()
	if uint64(currentTime) > token.OAuth2.ExpirationTime {
		if c.auth == nil {
			return "", xurlErrors.NewAuthError("AuthNotSet", errors.New("auth not set"))
		}
		
		newToken, err := c.auth.OAuth2RefreshToken(username)
		if err != nil {
			return "", err
		}
		
		return newToken, nil
	}
	
	return token.OAuth2.AccessToken, nil
}

// GetOAuth2Token gets an OAuth2 token
func (c *ApiClient) GetOAuth2Token(username string) (string, error) {
	if c.auth == nil {
		return "", xurlErrors.NewAuthError("AuthNotSet", errors.New("auth not set"))
	}
	
	var token *store.Token
	
	if username != "" {
		token = c.auth.TokenStore.GetOAuth2Token(username)
	} else {
		token = c.auth.TokenStore.GetFirstOAuth2Token()
	}
	
	if token == nil {
		return c.auth.OAuth2(username)
	}
	
	return c.ValidateAndRefreshOAuth2Token(token, username)
}

// GetAuthHeader gets the authorization header for a request
func (c *ApiClient) GetAuthHeader(method, url string, authType string, username string) (string, error) {
	if c.auth == nil {
		return "", xurlErrors.NewAuthError("AuthNotSet", errors.New("auth not set"))
	}
	
	// If auth type is specified, use it
	if authType != "" {
		switch strings.ToLower(authType) {
		case "oauth1":
			return c.auth.OAuth1(method, url, nil)
		case "oauth2":
			token, err := c.GetOAuth2Token(username)
			if err != nil {
				return "", err
			}
			return "Bearer " + token, nil
		case "bearer":
			token := c.auth.BearerToken()
			if token == "" {
				return "", xurlErrors.NewAuthError("TokenNotFound", errors.New("bearer token not found"))
			}
			return "Bearer " + token, nil
		default:
			return "", xurlErrors.NewAuthError("InvalidAuthType", fmt.Errorf("invalid auth type: %s", authType))
		}
	}
	
	// If no auth type is specified, try to use the first OAuth2 token
	token := c.auth.TokenStore.GetFirstOAuth2Token()
	if token != nil {
		accessToken, err := c.ValidateAndRefreshOAuth2Token(token, username)
		if err == nil {
			return "Bearer " + accessToken, nil
		} else {
			fmt.Println("Error validating OAuth2 token, attempting to use OAuth1:", err)
		}
	}
	
	// If no OAuth2 token is available, try to use the first OAuth1 token
	token = c.auth.TokenStore.GetOAuth1Tokens()
	if token != nil {
		authHeader, err := c.auth.OAuth1(method, url, nil)
		if err == nil {
			return authHeader, nil
		} else {
			fmt.Println("Error using OAuth1 token, attempting to use bearer token:", err)
		}
	}

	// If no OAuth1 token is available, try to use the bearer token
	bearerToken := c.auth.BearerToken()
	if bearerToken != "" {
		return "Bearer " + bearerToken, nil
	} else {
		fmt.Println("Error using bearer token:", errors.New("bearer token not found"))
	}
	
	// If no authentication method is available, return an error
	return "", xurlErrors.NewAuthError("NoAuthMethod", errors.New("no authentication method available"))
}

// BuildRequest builds an HTTP request
func (c *ApiClient) BuildRequest(method, endpoint string, headers []string, data string, authType string, username string) (*http.Request, error) {
	httpMethod := strings.ToUpper(method)
	
	url := endpoint
	if !strings.HasPrefix(strings.ToLower(endpoint), "http") {
		url = c.url
		if !strings.HasSuffix(url, "/") {
			url += "/"
		}
		if strings.HasPrefix(endpoint, "/") {
			url += endpoint[1:]
		} else {
			url += endpoint
		}
	}
	
	var req *http.Request
	var err error
	
	if data != "" && (httpMethod == "POST" || httpMethod == "PUT" || httpMethod == "PATCH") {
		req, err = http.NewRequest(httpMethod, url, bytes.NewBufferString(data))
	} else {
		req, err = http.NewRequest(httpMethod, url, nil)
	}
	
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	
	// Add headers
	for _, header := range headers {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}
	
	// Add content-type header if not present and we have data
	if data != "" && req.Header.Get("Content-Type") == "" {
		// Try to parse as JSON
		var js json.RawMessage
		if json.Unmarshal([]byte(data), &js) == nil {
			req.Header.Add("Content-Type", "application/json")
		} else {
			req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	
	// Add authorization header if not present
	if req.Header.Get("Authorization") == "" {
		authHeader, err := c.GetAuthHeader(httpMethod, url, authType, username)
		if err == nil {
			req.Header.Add("Authorization", authHeader)
		}
	}
	
	return req, nil
}

// SendRequest sends an HTTP request
func (c *ApiClient) SendRequest(method, endpoint string, headers []string, data string, authType string, username string, verbose bool) (json.RawMessage, *xurlErrors.Error) {
	req, err := c.BuildRequest(method, endpoint, headers, data, authType, username)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	
	if verbose {
		fmt.Printf("\033[1;34m> %s\033[0m %s\n", req.Method, req.URL)
		for key, values := range req.Header {
			for _, value := range values {
				fmt.Printf("\033[1;36m> %s\033[0m: %s\n", key, value)
			}
		}
		fmt.Println()
	}
	
	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	defer resp.Body.Close()
	
	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, xurlErrors.NewIOError(err)
	}
	
	// Print verbose information
	if verbose {
		fmt.Printf("\033[1;31m< %s\033[0m\n", resp.Status)
		for key, values := range resp.Header {
			for _, value := range values {
				fmt.Printf("\033[1;32m< %s\033[0m: %s\n", key, value)
			}
		}
		fmt.Println()
	}
	
	// Check if response is JSON
	var js json.RawMessage
	if err := json.Unmarshal(body, &js); err != nil {
		return nil, xurlErrors.NewJSONError(err)
	}
	
	// Check if response is an error
	if resp.StatusCode >= 400 {
		return nil, xurlErrors.NewAPIError(js)
	}
	
	return js, nil
} 