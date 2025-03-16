package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockApiClient is a mock implementation of the ApiClient for testing
type MockApiClient struct {
	mock.Mock
}

func (m *MockApiClient) SendRequest(method, endpoint string, headers []string, data string, authType string, username string, verbose bool) (json.RawMessage, error) {
	args := m.Called(method, endpoint, headers, data, authType, username, verbose)
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockApiClient) SendMultipartRequestWithBuffer(method, endpoint string, headers []string, formFields map[string]string, fileField, fileName string, fileData []byte, authType string, username string, verbose bool) (json.RawMessage, error) {
	args := m.Called(method, endpoint, headers, formFields, fileField, fileName, fileData, authType, username, verbose)
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockApiClient) SendMultipartRequest(method, endpoint string, headers []string, formFields map[string]string, fileField, filePath string, authType string, username string, verbose bool) (json.RawMessage, error) {
	args := m.Called(method, endpoint, headers, formFields, fileField, filePath, authType, username, verbose)
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func (m *MockApiClient) BuildRequest(method, endpoint string, headers []string, data io.Reader, contentType string, authType string, username string) (*http.Request, error) {
	args := m.Called(method, endpoint, headers, data, contentType, authType, username)
	return args.Get(0).(*http.Request), args.Error(1)
}

func (m *MockApiClient) GetAuthHeader(method, endpoint string, authType string, username string) (string, error) {
	args := m.Called(method, endpoint, authType, username)
	return args.String(0), args.Error(1)
}

func (m *MockApiClient) StreamRequest(method, endpoint string, headers []string, data string, authType string, username string, verbose bool) error {
	args := m.Called(method, endpoint, headers, data, authType, username, verbose)
	return args.Error(0)
}

// Helper function to create a temporary test file
func createTempTestFile(t *testing.T, size int) string {
	tempFile, err := os.CreateTemp("", "media_test_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	
	if _, err := tempFile.Write(data); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	return tempFile.Name()
}

func TestNewMediaUploader(t *testing.T) {
	mockClient := new(MockApiClient)
	
	tempFile := createTempTestFile(t, 1024)
	defer os.Remove(tempFile)
	
	uploader, err := NewMediaUploader(mockClient, tempFile, true, "oauth2", "testuser", []string{})
	assert.NoError(t, err)
	assert.NotNil(t, uploader)
	assert.Equal(t, tempFile, uploader.filePath)
	assert.Equal(t, int64(1024), uploader.fileSize)
	assert.Equal(t, true, uploader.verbose)
	assert.Equal(t, "oauth2", uploader.authType)
	assert.Equal(t, "testuser", uploader.username)
	
	uploader, err = NewMediaUploader(mockClient, "nonexistent.txt", false, "oauth2", "testuser", []string{})
	assert.Error(t, err)
	assert.Nil(t, uploader)
	
	tempDir, err := os.MkdirTemp("", "media_test_dir")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)
	
	uploader, err = NewMediaUploader(mockClient, tempDir, false, "oauth2", "testuser", []string{})
	assert.Error(t, err)
	assert.Nil(t, uploader)
}

func TestMediaUploader_Init(t *testing.T) {
	mockClient := new(MockApiClient)
	
	tempFile := createTempTestFile(t, 1024)
	defer os.Remove(tempFile)
	
	uploader, err := NewMediaUploader(mockClient, tempFile, false, "oauth2", "testuser", []string{})
	assert.NoError(t, err)
	
	initResponse := json.RawMessage(`{
		"data": {
			"id": "test_media_id",
			"expires_after_secs": 3600,
			"media_key": "test_media_key"
		}
	}`)
	
	expectedUrl := MediaEndpoint + "?command=INIT&total_bytes=1024&media_type=image/jpeg&media_category=tweet_image"
	mockClient.On("SendRequest", "POST", expectedUrl, []string{}, "", "oauth2", "testuser", false).Return(initResponse, nil)
	
	err = uploader.Init("image/jpeg", "tweet_image")
	assert.NoError(t, err)
	assert.Equal(t, "test_media_id", uploader.GetMediaID())
	
	mockClient.AssertExpectations(t)
	
	mockClient = new(MockApiClient)
	uploader, err = NewMediaUploader(mockClient, tempFile, false, "oauth2", "testuser", []string{})
	assert.NoError(t, err)
	
	mockClient.On("SendRequest", "POST", expectedUrl, []string{}, "", "oauth2", "testuser", false).Return(json.RawMessage("{}"), assert.AnError)
	
	err = uploader.Init("image/jpeg", "tweet_image")
	assert.Error(t, err)
	
	mockClient.AssertExpectations(t)
}

func TestMediaUploader_Append(t *testing.T) {
	mockClient := new(MockApiClient)
	
	fileSize := 8 * 1024 * 1024
	tempFile := createTempTestFile(t, fileSize)
	defer os.Remove(tempFile)
	
	uploader, err := NewMediaUploader(mockClient, tempFile, false, "oauth2", "testuser", []string{})
	assert.NoError(t, err)
	
	uploader.SetMediaID("test_media_id")
	
	mockClient.On(
		"SendMultipartRequestWithBuffer",
		"POST",
		MediaEndpoint,
		[]string{},
		map[string]string{"command": "APPEND", "media_id": "test_media_id", "segment_index": "0"},
		"media",
		filepath.Base(tempFile),
		mock.Anything,
		"oauth2",
		"testuser",
		false,
	).Return(json.RawMessage("{}"), nil)
	
	mockClient.On(
		"SendMultipartRequestWithBuffer",
		"POST",
		MediaEndpoint,
		[]string{},
		map[string]string{"command": "APPEND", "media_id": "test_media_id", "segment_index": "1"},
		"media",
		filepath.Base(tempFile),
		mock.Anything,
		"oauth2",
		"testuser",
		false,
	).Return(json.RawMessage("{}"), nil)
	
	err = uploader.Append()
	assert.NoError(t, err)
	
	mockClient.AssertExpectations(t)
	
	uploader.SetMediaID("")
	err = uploader.Append()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "media ID not set")
}

func TestMediaUploader_Finalize(t *testing.T) {
	mockClient := new(MockApiClient)
	
	tempFile := createTempTestFile(t, 1024)
	defer os.Remove(tempFile)
	
	uploader, err := NewMediaUploader(mockClient, tempFile, false, "oauth2", "testuser", []string{})
	assert.NoError(t, err)
	
	uploader.SetMediaID("test_media_id")
	
	finalizeResponse := json.RawMessage(`{
		"data": {
			"id": "test_media_id",
			"media_key": "test_media_key"
		}
	}`)
	
	expectedUrl := MediaEndpoint + "?command=FINALIZE&media_id=test_media_id"
	mockClient.On("SendRequest", "POST", expectedUrl, []string{}, "", "oauth2", "testuser", false).Return(finalizeResponse, nil)
	
	response, err := uploader.Finalize()
	assert.NoError(t, err)
	assert.Equal(t, finalizeResponse, response)
	
	mockClient.AssertExpectations(t)
	
	uploader.SetMediaID("")
	response, err = uploader.Finalize()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "media ID not set")
	assert.Nil(t, response)
}

func TestMediaUploader_CheckStatus(t *testing.T) {
	mockClient := new(MockApiClient)
	
	tempFile := createTempTestFile(t, 1024)
	defer os.Remove(tempFile)
	
	uploader, err := NewMediaUploader(mockClient, tempFile, false, "oauth2", "testuser", []string{})
	assert.NoError(t, err)
	
	uploader.SetMediaID("test_media_id")
	
	statusResponse := json.RawMessage(`{
		"data": {
			"id": "test_media_id",
			"media_key": "test_media_key",
			"processing_info": {
				"state": "succeeded",
				"progress_percent": 100
			}
		}
	}`)
	
	expectedUrl := MediaEndpoint + "?command=STATUS&media_id=test_media_id"
	mockClient.On("SendRequest", "GET", expectedUrl, []string{}, "", "oauth2", "testuser", false).Return(statusResponse, nil)
	
	response, err := uploader.CheckStatus()
	assert.NoError(t, err)
	assert.Equal(t, statusResponse, response)
	
	mockClient.AssertExpectations(t)
	
	uploader.SetMediaID("")
	response, err = uploader.CheckStatus()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "media ID not set")
	assert.Nil(t, response)
}

func TestMediaUploader_WaitForProcessing(t *testing.T) {
	mockClient := new(MockApiClient)
	
	tempFile := createTempTestFile(t, 1024)
	defer os.Remove(tempFile)
	
	uploader, err := NewMediaUploader(mockClient, tempFile, false, "oauth2", "testuser", []string{})
	assert.NoError(t, err)
	
	uploader.SetMediaID("test_media_id")
	
	inProgressResponse := json.RawMessage(`{
		"data": {
			"id": "test_media_id",
			"media_key": "test_media_key",
			"processing_info": {
				"state": "in_progress",
				"check_after_secs": 1,
				"progress_percent": 50
			}
		}
	}`)
	
	successResponse := json.RawMessage(`{
		"data": {
			"id": "test_media_id",
			"media_key": "test_media_key",
			"processing_info": {
				"state": "succeeded",
				"progress_percent": 100
			}
		}
	}`)
	
	expectedUrl := MediaEndpoint + "?command=STATUS&media_id=test_media_id"
	
	mockClient.On("SendRequest", "GET", expectedUrl, []string{}, "", "oauth2", "testuser", false).Return(inProgressResponse, nil).Once()
	mockClient.On("SendRequest", "GET", expectedUrl, []string{}, "", "oauth2", "testuser", false).Return(successResponse, nil).Once()
	
	response, err := uploader.WaitForProcessing()
	assert.NoError(t, err)
	assert.Equal(t, successResponse, response)
	
	mockClient.AssertExpectations(t)
	
	failedResponse := json.RawMessage(`{
		"data": {
			"id": "test_media_id",
			"media_key": "test_media_key",
			"processing_info": {
				"state": "failed",
				"progress_percent": 0
			}
		}
	}`)
	
	mockClient.On("SendRequest", "GET", expectedUrl, []string{}, "", "oauth2", "testuser", false).Return(failedResponse, nil).Once()
	
	response, err = uploader.WaitForProcessing()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "media processing failed")
	assert.Nil(t, response)
	
	uploader.SetMediaID("")
	response, err = uploader.WaitForProcessing()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "media ID not set")
	assert.Nil(t, response)
}

func TestExecuteMediaUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == MediaEndpoint {
			command := r.FormValue("command")

			switch command {
			case "INIT":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				w.Write([]byte(`{
					"data": {
						"id": "test_media_id",
						"expires_after_secs": 3600,
						"media_key": "test_media_key"
					}
				}`))
			case "APPEND":
				w.WriteHeader(http.StatusNoContent)
				w.Write([]byte(`{}`))
			case "FINALIZE":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"data": {
						"id": "test_media_id",
						"media_key": "test_media_key"
					}
				}`))
			case "STATUS":
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"data": {
						"id": "test_media_id",
						"media_key": "test_media_key",
						"processing_info": {
							"state": "succeeded",
							"progress_percent": 100
						}
					}
				}`))
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	
	client := &ApiClient{
		url:    server.URL,
		client: &http.Client{Timeout: 30 * time.Second},
	}
	
	tempFile := createTempTestFile(t, 1024)
	defer os.Remove(tempFile)
	
	err := ExecuteMediaUpload(tempFile, "image/jpeg", "tweet_image", "oauth2", "testuser", false, false, []string{}, client)
	assert.NoError(t, err)
	
	err = ExecuteMediaUpload("nonexistent.txt", "image/jpeg", "tweet_image", "oauth2", "testuser", false, false, []string{}, client)
	assert.Error(t, err)
}

func TestExecuteMediaStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == MediaEndpoint && r.URL.Query().Get("command") == "STATUS" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"data": {
					"id": "test_media_id",
					"media_key": "test_media_key",
					"processing_info": {
						"state": "succeeded",
						"progress_percent": 100
					}
				}
			}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	
	client := &ApiClient{
		url:    server.URL,
		client: &http.Client{Timeout: 30 * time.Second},
	}
	
	err := ExecuteMediaStatus("test_media_id", "oauth2", "testuser", false, false, []string{}, client)
	assert.NoError(t, err)
}

func TestExtractMediaID(t *testing.T) {
	testCases := []struct {
		url      string
		data     string
		expected string
	}{
		{"/2/media/upload?command=APPEND&media_id=123456", "", "123456"},
		{"/2/media/upload?media_id=123456&command=APPEND", "", "123456"},
		{"", "media_id=123456&segment_index=0", "123456"},
		{"/2/media/upload", "command=APPEND&media_id=123456", "123456"},
		{"/2/media/upload", "", ""},
		{"", "", ""},
	}
	
	for _, tc := range testCases {
		result := ExtractMediaID(tc.url, tc.data)
		assert.Equal(t, tc.expected, result)
	}
}

func TestExtractSegmentIndex(t *testing.T) {
	testCases := []struct {
		url      string
		data     string
		expected string
	}{
		{"/2/media/upload?command=APPEND&segment_index=1", "", "1"},
		{"/2/media/upload?segment_index=1&command=APPEND", "", "1"},
		{"", "segment_index=1&media_id=123456", "1"},
		{"/2/media/upload", "command=APPEND&segment_index=1", "1"},
		{"/2/media/upload", "", ""},
		{"", "", ""},
	}
	
	for _, tc := range testCases {
		result := ExtractSegmentIndex(tc.url, tc.data)
		assert.Equal(t, tc.expected, result)
	}
}

func TestIsMediaAppendRequest(t *testing.T) {
	testCases := []struct {
		url       string
		mediaFile string
		expected  bool
	}{
		{"/2/media/upload?command=APPEND", "file.jpg", true},
		{"/2/media/upload?command=INIT", "file.jpg", false},
		{"/2/media/upload?command=APPEND", "", false},
		{"/2/users/me", "file.jpg", false},
		{"", "", false},
	}
	
	for _, tc := range testCases {
		result := IsMediaAppendRequest(tc.url, tc.mediaFile)
		assert.Equal(t, tc.expected, result)
	}
}

func TestHandleMediaAppendRequest(t *testing.T) {
	mockClient := new(MockApiClient)
	
	tempFile := createTempTestFile(t, 1024)
	defer os.Remove(tempFile)
	
	mockResponse := json.RawMessage(`{}`)
	
	url := "/2/media/upload?command=APPEND&media_id=123456"
	mockClient.On(
		"SendMultipartRequest",
		"POST",
		url,
		[]string{},
		map[string]string{"command": "APPEND", "media_id": "123456", "segment_index": "0"},
		"media",
		tempFile,
		"oauth2",
		"testuser",
		false,
	).Return(mockResponse, nil)
	
	response, err := HandleMediaAppendRequest(url, tempFile, "POST", []string{}, "", "oauth2", "testuser", false, mockClient)
	assert.NoError(t, err)
	assert.Equal(t, mockResponse, response)
	
	url = "/2/media/upload?command=APPEND"
	data := "media_id=123456&segment_index=1"
	mockClient.On(
		"SendMultipartRequest",
		"POST",
		url,
		[]string{},
		map[string]string{"command": "APPEND", "media_id": "123456", "segment_index": "1"},
		"media",
		tempFile,
		"oauth2",
		"testuser",
		false,
	).Return(mockResponse, nil)
	
	response, err = HandleMediaAppendRequest(url, tempFile, "POST", []string{}, data, "oauth2", "testuser", false, mockClient)
	assert.NoError(t, err)
	assert.Equal(t, mockResponse, response)
	
	url = "/2/media/upload?command=APPEND"
	data = ""
	response, err = HandleMediaAppendRequest(url, tempFile, "POST", []string{}, data, "oauth2", "testuser", false, mockClient)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "media_id is required")
	assert.Nil(t, response)
	
	mockClient.AssertExpectations(t)
} 