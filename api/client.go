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
func (c *ApiClient) processResponse(resp *http.Response, verbose bool) (json.RawMessage, *xurlErrors.Error) {
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
func (c *ApiClient) SendRequest(method, endpoint string, headers []string, data string, authType string, username string, verbose bool) (json.RawMessage, *xurlErrors.Error) {
	var body io.Reader
	contentType := ""
	
	if data != "" && (strings.ToUpper(method) == "POST" || strings.ToUpper(method) == "PUT" || strings.ToUpper(method) == "PATCH") {
		body = bytes.NewBufferString(data)

		var js json.RawMessage
		if json.Unmarshal([]byte(data), &js) == nil {
			contentType = "application/json"
		}
	}
	
	req, err := c.BuildRequest(method, endpoint, headers, body, contentType, authType, username)
	
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	
	c.logRequest(req, verbose)
	
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	defer resp.Body.Close()
	
	return c.processResponse(resp, verbose)
}

// prepareMultipartRequest prepares a multipart request with common setup
func (c *ApiClient) prepareMultipartRequest(method, endpoint string, headers []string, formFields map[string]string, 
	writer *multipart.Writer, body *bytes.Buffer, authType string, username string) (*http.Request, *xurlErrors.Error) {
	
	for key, value := range formFields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error writing form field: %v", err))
		}
	}
	
	if err := writer.Close(); err != nil {
		return nil, xurlErrors.NewIOError(fmt.Errorf("error closing multipart writer: %v", err))
	}
	
	req, err := c.BuildRequest(method, endpoint, headers, body, writer.FormDataContentType(), authType, username)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	
	return req, nil
}

// SendMultipartRequest sends an HTTP request with multipart form data
func (c *ApiClient) SendMultipartRequest(method, endpoint string, headers []string, formFields map[string]string, fileField, filePath string, authType string, username string, verbose bool) (json.RawMessage, *xurlErrors.Error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	
	if fileField != "" && filePath != "" {
		file, err := os.Open(filePath)
		if err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error opening file: %v", err))
		}
		defer file.Close()
		
		part, err := writer.CreateFormFile(fileField, filepath.Base(filePath))
		if err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error creating form file: %v", err))
		}
		
		if _, err := io.Copy(part, file); err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error copying file content: %v", err))
		}
	}
	
	req, xerr := c.prepareMultipartRequest(method, endpoint, headers, formFields, writer, body, authType, username)
	if xerr != nil {
		return nil, xerr
	}
	
	c.logRequest(req, verbose)
	
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	defer resp.Body.Close()
	
	return c.processResponse(resp, verbose)
}

// SendMultipartRequestWithBuffer sends an HTTP request with multipart form data using a buffer for file data
func (c *ApiClient) SendMultipartRequestWithBuffer(method, endpoint string, headers []string, formFields map[string]string, fileField, fileName string, fileData []byte, authType string, username string, verbose bool) (json.RawMessage, *xurlErrors.Error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	
	if fileField != "" && len(fileData) > 0 {
		part, err := writer.CreateFormFile(fileField, fileName)
		if err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error creating form file: %v", err))
		}
		
		if _, err := part.Write(fileData); err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error writing file data: %v", err))
		}
	}
	
	req, xerr := c.prepareMultipartRequest(method, endpoint, headers, formFields, writer, body, authType, username)
	if xerr != nil {
		return nil, xerr
	}
	
	c.logRequest(req, verbose)
	
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	defer resp.Body.Close()
	
	return c.processResponse(resp, verbose)
}

// StreamRequest sends an HTTP request and streams the response
func (c *ApiClient) StreamRequest(method, endpoint string, headers []string, data string, authType string, username string, verbose bool) *xurlErrors.Error {
	var body io.Reader
	contentType := ""
	
	if data != "" && (strings.ToUpper(method) == "POST" || strings.ToUpper(method) == "PUT" || strings.ToUpper(method) == "PATCH") {
		body = bytes.NewBufferString(data)
		
		var js json.RawMessage
		if json.Unmarshal([]byte(data), &js) == nil {
			contentType = "application/json"
		} else {
			contentType = "application/x-www-form-urlencoded"
		}
	}
	
	req, err := c.BuildRequest(method, endpoint, headers, body, contentType, authType, username)
	if err != nil {
		return xurlErrors.NewHTTPError(err)
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
	
	client := &http.Client{
		Timeout: 0,
	}
	
	fmt.Printf("\033[1;32mConnecting to streaming endpoint: %s\033[0m\n", endpoint)
	
	resp, err := client.Do(req)
	if err != nil {
		return xurlErrors.NewHTTPError(err)
	}
	defer resp.Body.Close()
	
	if verbose {
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