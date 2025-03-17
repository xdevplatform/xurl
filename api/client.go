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

	"bufio"
	"mime/multipart"
	"os"
	"path/filepath"
	"xurl/auth"
	"xurl/config"
	xurlErrors "xurl/errors"
	"xurl/version"
)

// RequestOptions contains common options for API requests
type RequestOptions struct {
	Method   string
	Endpoint string
	Headers  []string
	Data     string
	AuthType string
	Username string
	Verbose  bool
}

// MultipartOptions contains options specific to multipart requests
type MultipartOptions struct {
	RequestOptions
	FormFields map[string]string
	FileField  string
	FilePath   string
	FileName   string
	FileData   []byte
}

// Client is an interface for API clients
type Client interface {
	GetAuthHeader(method, url string, authType string, username string) (string, error)
	BuildRequest(method, endpoint string, headers []string, body io.Reader, contentType string, authType string, username string) (*http.Request, error)
	SendRequest(options RequestOptions) (json.RawMessage, error)
	StreamRequest(options RequestOptions) error
	SendMultipartRequest(options MultipartOptions) (json.RawMessage, error)
}

// ApiClient handles API requests
type ApiClient struct {
	url    string
	client *http.Client
	auth   *auth.Auth
}

// NewApiClient creates a new ApiClient
func NewApiClient(config *config.Config, auth *auth.Auth) *ApiClient {
	return &ApiClient{
		url:    config.APIBaseURL,
		client: &http.Client{Timeout: 30 * time.Second},
		auth:   auth,
	}
}

// GetAuthHeader gets the authorization header for a request
func (c *ApiClient) GetAuthHeader(method, url string, authType string, username string) (string, error) {
	if c.auth == nil {
		return "", xurlErrors.NewAuthError("AuthNotSet", errors.New("auth not set"))
	}

	if authType != "" {
		switch strings.ToLower(authType) {
		case "oauth1":
			return c.auth.GetOAuth1Header(method, url, nil)
		case "oauth2":
			return c.auth.GetOAuth2Header(username)
		case "app":
			return c.auth.GetBearerTokenHeader()
		default:
			return "", xurlErrors.NewAuthError("InvalidAuthType", fmt.Errorf("invalid auth type: %s", authType))
		}
	}

	// If no auth type is specified, try to use the first OAuth2 token
	token := c.auth.TokenStore.GetFirstOAuth2Token()
	if token != nil {
		accessToken, err := c.auth.GetOAuth2Header(username)
		if err == nil {
			return accessToken, nil
		}
	}

	// If no OAuth2 token is available, try to use the first OAuth1 token
	token = c.auth.TokenStore.GetOAuth1Tokens()
	if token != nil {
		authHeader, err := c.auth.GetOAuth1Header(method, url, nil)
		if err == nil {
			return authHeader, nil
		}
	}

	// If no OAuth1 token is available, try to use the bearer token
	bearerToken, err := c.auth.GetBearerTokenHeader()
	if err == nil {
		return bearerToken, nil
	}

	// If no authentication method is available, return an error
	return "", xurlErrors.NewAuthError("NoAuthMethod", errors.New("no authentication method available"))
}

// BuildRequest builds an HTTP request
func (c *ApiClient) BuildRequest(method, endpoint string, headers []string, body io.Reader, contentType string, authType string, username string) (*http.Request, error) {
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

	req, err := http.NewRequest(httpMethod, url, body)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}

	for _, header := range headers {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	if req.Header.Get("Authorization") == "" {
		authHeader, err := c.GetAuthHeader(httpMethod, url, authType, username)
		if err == nil {
			req.Header.Add("Authorization", authHeader)
		}
	}

	req.Header.Add("User-Agent", "xurl/"+version.Version)

	return req, nil
}

// processResponse handles common response processing logic
func (c *ApiClient) processResponse(resp *http.Response, verbose bool) (json.RawMessage, error) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, xurlErrors.NewIOError(err)
	}

	if verbose {
		fmt.Printf("\033[1;31m< %s\033[0m\n", resp.Status)
		for key, values := range resp.Header {
			for _, value := range values {
				fmt.Printf("\033[1;32m< %s\033[0m: %s\n", key, value)
			}
		}
		fmt.Println()
	}

	var js json.RawMessage
	if len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, &js); err != nil {
			if resp.StatusCode >= 400 {
				return nil, xurlErrors.NewHTTPError(fmt.Errorf("HTTP error: %s", resp.Status))
			}
			js = json.RawMessage("{}")
		}
	} else {
		js = json.RawMessage("{}")
	}

	if resp.StatusCode >= 400 {
		return nil, xurlErrors.NewAPIError(js)
	}

	return js, nil
}

// logRequest logs request details if verbose mode is enabled
func (c *ApiClient) logRequest(req *http.Request, verbose bool) {
	if verbose {
		fmt.Printf("\033[1;34m> %s\033[0m %s\n", req.Method, req.URL)
		for key, values := range req.Header {
			for _, value := range values {
				fmt.Printf("\033[1;36m> %s\033[0m: %s\n", key, value)
			}
		}
		fmt.Println()
	}
}

// SendRequest sends an HTTP request
func (c *ApiClient) SendRequest(options RequestOptions) (json.RawMessage, error) {
	var body io.Reader
	contentType := ""

	if options.Data != "" && (strings.ToUpper(options.Method) == "POST" || strings.ToUpper(options.Method) == "PUT" || strings.ToUpper(options.Method) == "PATCH") {
		body = bytes.NewBufferString(options.Data)

		var js json.RawMessage
		if json.Unmarshal([]byte(options.Data), &js) == nil {
			contentType = "application/json"
		}
	}

	req, err := c.BuildRequest(options.Method, options.Endpoint, options.Headers, body, contentType, options.AuthType, options.Username)

	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}

	c.logRequest(req, options.Verbose)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	defer resp.Body.Close()

	return c.processResponse(resp, options.Verbose)
}

// SendMultipartRequest sends an HTTP request with multipart form data
func (c *ApiClient) SendMultipartRequest(options MultipartOptions) (json.RawMessage, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Handle file from path
	if options.FileField != "" && options.FilePath != "" {
		file, err := os.Open(options.FilePath)
		if err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error opening file: %v", err))
		}
		defer file.Close()

		part, err := writer.CreateFormFile(options.FileField, filepath.Base(options.FilePath))
		if err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error creating form file: %v", err))
		}

		if _, err := io.Copy(part, file); err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error copying file content: %v", err))
		}
	} else if options.FileField != "" && len(options.FileData) > 0 { // Handle file from buffer
		part, err := writer.CreateFormFile(options.FileField, options.FileName)
		if err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error creating form file: %v", err))
		}

		if _, err := part.Write(options.FileData); err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error writing file data: %v", err))
		}
	}

	for key, value := range options.FormFields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error writing form field: %v", err))
		}
	}

	if err := writer.Close(); err != nil {
		return nil, xurlErrors.NewIOError(fmt.Errorf("error closing multipart writer: %v", err))
	}

	req, err := c.BuildRequest(options.Method, options.Endpoint, options.Headers, body, writer.FormDataContentType(), options.AuthType, options.Username)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}

	c.logRequest(req, options.Verbose)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	defer resp.Body.Close()

	return c.processResponse(resp, options.Verbose)
}

// StreamRequest sends an HTTP request and streams the response
func (c *ApiClient) StreamRequest(options RequestOptions) error {
	var body io.Reader
	contentType := ""

	if options.Data != "" && (strings.ToUpper(options.Method) == "POST" || strings.ToUpper(options.Method) == "PUT" || strings.ToUpper(options.Method) == "PATCH") {
		body = bytes.NewBufferString(options.Data)

		var js json.RawMessage
		if json.Unmarshal([]byte(options.Data), &js) == nil {
			contentType = "application/json"
		} else {
			contentType = "application/x-www-form-urlencoded"
		}
	}

	req, err := c.BuildRequest(options.Method, options.Endpoint, options.Headers, body, contentType, options.AuthType, options.Username)
	if err != nil {
		return xurlErrors.NewHTTPError(err)
	}

	if options.Verbose {
		fmt.Printf("\033[1;34m> %s\033[0m %s\n", req.Method, req.URL)
		for key, values := range req.Header {
			for _, value := range values {
				fmt.Printf("\033[1;36m> %s\033[0m: %s\n", key, value)
			}
		}
		fmt.Println()
	}

	client := &http.Client{
		Timeout: 0,
	}

	fmt.Printf("\033[1;32mConnecting to streaming endpoint: %s\033[0m\n", options.Endpoint)

	resp, err := client.Do(req)
	if err != nil {
		return xurlErrors.NewHTTPError(err)
	}
	defer resp.Body.Close()

	if options.Verbose {
		fmt.Printf("\033[1;31m< %s\033[0m\n", resp.Status)
		for key, values := range resp.Header {
			for _, value := range values {
				fmt.Printf("\033[1;32m< %s\033[0m: %s\n", key, value)
			}
		}
		fmt.Println()
	}

	if resp.StatusCode >= 400 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return xurlErrors.NewIOError(err)
		}

		var js json.RawMessage
		if err := json.Unmarshal(body, &js); err != nil {
			return xurlErrors.NewJSONError(err)
		}

		return xurlErrors.NewAPIError(js)
	}

	scanner := bufio.NewScanner(resp.Body)

	const maxScanTokenSize = 1024 * 1024
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)

	fmt.Println("\033[1;32m--- Streaming response started ---\033[0m")
	fmt.Println("\033[1;32m--- Press Ctrl+C to stop ---\033[0m")

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}
		// We can't pretty-print streaming responses
		fmt.Println(line)
	}

	if err := scanner.Err(); err != nil {
		if err == bufio.ErrTooLong {
			return xurlErrors.NewIOError(fmt.Errorf("line too long: increase buffer size"))
		}
		return xurlErrors.NewIOError(err)
	}

	fmt.Println("\033[1;32m--- End of stream ---\033[0m")
	return nil
}
