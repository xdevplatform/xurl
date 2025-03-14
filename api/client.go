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
	
	// If auth type is specified, use it
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

// SendMultipartRequest sends an HTTP request with multipart form data
func (c *ApiClient) SendMultipartRequest(method, endpoint string, headers []string, formFields map[string]string, fileField, filePath string, authType string, username string, verbose bool) (json.RawMessage, *xurlErrors.Error) {
	// Create a new multipart writer
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	
	// Add form fields
	for key, value := range formFields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error writing form field: %v", err))
		}
	}
	
	// Add file if provided
	if fileField != "" && filePath != "" {
		file, err := os.Open(filePath)
		if err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error opening file: %v", err))
		}
		defer file.Close()
		
		// Create form file
		part, err := writer.CreateFormFile(fileField, filepath.Base(filePath))
		if err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error creating form file: %v", err))
		}
		
		// Copy file content to form file
		if _, err := io.Copy(part, file); err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error copying file content: %v", err))
		}
	}
	
	// Close the writer
	if err := writer.Close(); err != nil {
		return nil, xurlErrors.NewIOError(fmt.Errorf("error closing multipart writer: %v", err))
	}
	
	// Create request
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	
	// Add content type header for multipart form data
	req.Header.Set("Content-Type", writer.FormDataContentType())
	
	// Add other headers
	for _, header := range headers {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}
	
	// Add authorization header if not present
	if req.Header.Get("Authorization") == "" {
		authHeader, err := c.GetAuthHeader(method, endpoint, authType, username)
		if err == nil {
			req.Header.Add("Authorization", authHeader)
		}
	}
	
	// Print verbose information
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
	responseBody, err := io.ReadAll(resp.Body)
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
	if len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, &js); err != nil {
			// If the response is not JSON, return an error
			if resp.StatusCode >= 400 {
				return nil, xurlErrors.NewHTTPError(fmt.Errorf("HTTP error: %s", resp.Status))
			}
			// For successful responses that are not JSON, return an empty JSON object
			js = json.RawMessage("{}")
		}
	} else {
		// Empty response, return an empty JSON object
		js = json.RawMessage("{}")
	}
	
	// Check if response is an error
	if resp.StatusCode >= 400 {
		return nil, xurlErrors.NewAPIError(js)
	}
	
	return js, nil
}

// SendMultipartRequestWithBuffer sends an HTTP request with multipart form data using a buffer for file data
func (c *ApiClient) SendMultipartRequestWithBuffer(method, endpoint string, headers []string, formFields map[string]string, fileField, fileName string, fileData []byte, authType string, username string, verbose bool) (json.RawMessage, *xurlErrors.Error) {
	// Create a new multipart writer
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	
	// Add form fields
	for key, value := range formFields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error writing form field: %v", err))
		}
	}
	
	// Add file data if provided
	if fileField != "" && len(fileData) > 0 {
		// Create form file
		part, err := writer.CreateFormFile(fileField, fileName)
		if err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error creating form file: %v", err))
		}
		
		// Write file data to form file
		if _, err := part.Write(fileData); err != nil {
			return nil, xurlErrors.NewIOError(fmt.Errorf("error writing file data: %v", err))
		}
	}
	
	// Close the writer
	if err := writer.Close(); err != nil {
		return nil, xurlErrors.NewIOError(fmt.Errorf("error closing multipart writer: %v", err))
	}
	
	// Create request
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, xurlErrors.NewHTTPError(err)
	}
	
	// Add content type header for multipart form data
	req.Header.Set("Content-Type", writer.FormDataContentType())
	
	// Add other headers
	for _, header := range headers {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}
	
	// Add authorization header if not present
	if req.Header.Get("Authorization") == "" {
		authHeader, err := c.GetAuthHeader(method, endpoint, authType, username)
		if err == nil {
			req.Header.Add("Authorization", authHeader)
		}
	}
	
	// Print verbose information
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
	responseBody, err := io.ReadAll(resp.Body)
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
	if len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, &js); err != nil {
			// If the response is not JSON, return an error
			if resp.StatusCode >= 400 {
				return nil, xurlErrors.NewHTTPError(fmt.Errorf("HTTP error: %s", resp.Status))
			}
			// For successful responses that are not JSON, return an empty JSON object
			js = json.RawMessage("{}")
		}
	} else {
		// Empty response, return an empty JSON object
		js = json.RawMessage("{}")
	}
	
	// Check if response is an error
	if resp.StatusCode >= 400 {
		return nil, xurlErrors.NewAPIError(js)
	}
	
	return js, nil
}

// StreamRequest sends an HTTP request and streams the response
func (c *ApiClient) StreamRequest(method, endpoint string, headers []string, data string, authType string, username string, verbose bool) *xurlErrors.Error {
	req, err := c.BuildRequest(method, endpoint, headers, data, authType, username)
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
		
		// Check if response is JSON
		var js json.RawMessage
		if err := json.Unmarshal(body, &js); err != nil {
			return xurlErrors.NewJSONError(err)
		}
		
		return xurlErrors.NewAPIError(js)
	}
	
	contentType := resp.Header.Get("Content-Type")
	isJSON := strings.Contains(contentType, "application/json") || 
		strings.Contains(contentType, "application/x-ndjson") ||
		strings.Contains(contentType, "application/stream+json")
	
	scanner := bufio.NewScanner(resp.Body)
	
	const maxScanTokenSize = 1024 * 1024 // 1MB
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)
	
	fmt.Println("\033[1;32m--- Streaming response started ---\033[0m")
	fmt.Println("\033[1;32m--- Press Ctrl+C to stop ---\033[0m")
	
	for scanner.Scan() {
		line := scanner.Text()
		
		if line == "" {
			continue
		}
		
		if isJSON {
			var js json.RawMessage
			if err := json.Unmarshal([]byte(line), &js); err != nil {
				fmt.Println(line)
			} else {
				prettyJSON, err := json.MarshalIndent(js, "", "  ")
				if err != nil {
					fmt.Println(line)
				} else {
					fmt.Println(string(prettyJSON))
				}
			}
		} else {
			fmt.Println(line)
		}
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