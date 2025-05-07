package api

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"xurl/utils"
)

const (
	// MediaEndpoint is the endpoint for media uploads
	MediaEndpoint = "/2/media/upload"
)

// MediaUploader handles media upload operations
type MediaUploader struct {
	client   Client
	mediaID  string
	filePath string
	fileSize int64
	verbose  bool
	authType string
	username string
	headers  []string
	trace    bool
}

type InitRequest struct {
	TotalBytes    int64  `json:"total_bytes"`
	MediaType     string `json:"media_type"`
	MediaCategory string `json:"media_category"`
}

// NewMediaUploader creates a new MediaUploader
func NewMediaUploader(client Client, filePath string, verbose, trace bool, authType string, username string, headers []string) (*MediaUploader, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("error accessing file: %v", err)
	}

	// Check if it's a regular file
	if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filePath)
	}

	return &MediaUploader{
		client:   client,
		filePath: filePath,
		fileSize: fileInfo.Size(),
		verbose:  verbose,
		authType: authType,
		username: username,
		headers:  headers,
		trace:    trace,
	}, nil
}

func NewMediaUploaderWithoutFile(client Client, verbose, trace bool, authType string, username string, headers []string) *MediaUploader {
	return &MediaUploader{
		client:   client,
		verbose:  verbose,
		authType: authType,
		username: username,
		headers:  headers,
		trace:    trace,
	}
}

// Init initializes the media upload
func (m *MediaUploader) Init(mediaType string, mediaCategory string) error {
	if m.verbose {
		fmt.Printf("\033[32mInitializing media upload...\033[0m\n")
	}

	finalUrl := MediaEndpoint +
		"/initialize"

	body := InitRequest{
		TotalBytes:    m.fileSize,
		MediaType:     mediaType,
		MediaCategory: mediaCategory,
	}
	jsonData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("error marshalling body: %v", err)
	}

	requestOptions := RequestOptions{
		Method:   "POST",
		Endpoint: finalUrl,
		Headers:  m.headers,
		Data:     string(jsonData),
		AuthType: m.authType,
		Username: m.username,
		Verbose:  m.verbose,
		Trace:    m.trace,
	}

	response, clientErr := m.client.SendRequest(requestOptions)
	if clientErr != nil {
		return fmt.Errorf("init request failed: %v", clientErr)
	}

	var initResponse struct {
		Data struct {
			ID               string `json:"id"`
			ExpiresAfterSecs int    `json:"expires_after_secs"`
			MediaKey         string `json:"media_key"`
		} `json:"data"`
	}

	if err := json.Unmarshal(response, &initResponse); err != nil {
		return fmt.Errorf("failed to parse init response: %v", err)
	}

	m.mediaID = initResponse.Data.ID

	if m.verbose {
		utils.FormatAndPrintResponse(initResponse)
	}

	return nil
}

// Append uploads the media in chunks
func (m *MediaUploader) Append() error {
	if m.mediaID == "" {
		return fmt.Errorf("media ID not set, call Init first")
	}

	if m.verbose {
		fmt.Printf("\033[32mUploading media in chunks...\033[0m\n")
	}

	// Open the file
	file, err := os.Open(m.filePath)
	if err != nil {
		return fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	// Upload in chunks of 4MB
	chunkSize := 4 * 1024 * 1024
	buffer := make([]byte, chunkSize)
	segmentIndex := 0
	bytesUploaded := int64(0)

	for {
		bytesRead, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading file: %v", err)
		}

		finalUrl := MediaEndpoint + fmt.Sprintf("/%s/append", m.mediaID)

		// Prepare form fields
		formFields := map[string]string{
			"segment_index": strconv.Itoa(segmentIndex),
		}

		requestOptions := RequestOptions{
			Method:   "POST",
			Endpoint: finalUrl,
			Headers:  m.headers,
			Data:     "",
			AuthType: m.authType,
			Username: m.username,
			Verbose:  m.verbose,
			Trace:    m.trace,
		}
		multipartOptions := MultipartOptions{
			RequestOptions: requestOptions,
			FormFields:     formFields,
			FileField:      "media",
			FileName:       filepath.Base(m.filePath),
			FileData:       buffer[:bytesRead],
		}

		// Send multipart request with buffer
		_, clientErr := m.client.SendMultipartRequest(multipartOptions)

		if clientErr != nil {
			return fmt.Errorf("append request failed: %v", clientErr)
		}

		bytesUploaded += int64(bytesRead)
		segmentIndex++

		if m.verbose {
			fmt.Printf("\033[33mUploaded %d of %d bytes (%.2f%%)\033[0m\n", bytesUploaded, m.fileSize, float64(bytesUploaded)/float64(m.fileSize)*100)
		}
	}

	if m.verbose {
		fmt.Printf("\033[32mUpload complete!\033[0m\n")
	}

	return nil
}

// Finalize finalizes the media upload
func (m *MediaUploader) Finalize() (json.RawMessage, error) {
	if m.mediaID == "" {
		return nil, fmt.Errorf("media ID not set, call Init first")
	}

	if m.verbose {
		fmt.Printf("\033[32mFinalizing media upload...\033[0m\n")
	}

	finalUrl := MediaEndpoint + fmt.Sprintf("/%s/finalize", m.mediaID)
	requestOptions := RequestOptions{
		Method:   "POST",
		Endpoint: finalUrl,
		Headers:  m.headers,
		Data:     "",
		AuthType: m.authType,
		Username: m.username,
		Verbose:  m.verbose,
		Trace:    m.trace,
	}
	response, clientErr := m.client.SendRequest(requestOptions)
	if clientErr != nil {
		return nil, fmt.Errorf("finalize request failed: %v", clientErr)
	}

	return response, nil
}

// CheckStatus checks the status of the media upload
func (m *MediaUploader) CheckStatus() (json.RawMessage, error) {
	if m.mediaID == "" {
		return nil, fmt.Errorf("media ID not set, call Init first")
	}

	if m.verbose {
		fmt.Println("Checking media status...")
	}

	url := MediaEndpoint + "?command=STATUS&media_id=" + m.mediaID

	requestOptions := RequestOptions{
		Method:   "GET",
		Endpoint: url,
		Headers:  []string{},
		Data:     "",
		AuthType: m.authType,
		Username: m.username,
		Verbose:  m.verbose,
		Trace:    m.trace,
	}
	response, clientErr := m.client.SendRequest(requestOptions)
	if clientErr != nil {
		return nil, fmt.Errorf("status request failed: %v", clientErr)
	}

	if m.verbose {
		utils.FormatAndPrintResponse(response)
	}

	return response, nil
}

// WaitForProcessing waits for media processing to complete
func (m *MediaUploader) WaitForProcessing() (json.RawMessage, error) {
	if m.mediaID == "" {
		return nil, fmt.Errorf("media ID not set, call Init first")
	}

	if m.verbose {
		fmt.Printf("\033[32mWaiting for media processing to complete...\033[0m\n")
	}

	for {
		response, err := m.CheckStatus()
		if err != nil {
			return nil, err
		}

		var statusResponse struct {
			Data struct {
				ProcessingInfo struct {
					State           string `json:"state"`
					CheckAfterSecs  int    `json:"check_after_secs"`
					ProgressPercent int    `json:"progress_percent"`
				} `json:"processing_info"`
			} `json:"data"`
		}

		if err := json.Unmarshal(response, &statusResponse); err != nil {
			return nil, fmt.Errorf("failed to parse status response: %v", err)
		}

		state := statusResponse.Data.ProcessingInfo.State
		if state == "succeeded" {
			if m.verbose {
				fmt.Printf("\033[32mMedia processing complete!\033[0m\n")
			}
			return response, nil
		} else if state == "failed" {
			return nil, fmt.Errorf("media processing failed")
		}

		checkAfterSecs := statusResponse.Data.ProcessingInfo.CheckAfterSecs
		if checkAfterSecs <= 0 {
			checkAfterSecs = 1
		}

		if m.verbose {
			fmt.Printf("\033[33mMedia processing in progress (%d%%), checking again in %d seconds...\033[0m\n",
				statusResponse.Data.ProcessingInfo.ProgressPercent,
				checkAfterSecs)
		}

		time.Sleep(time.Duration(checkAfterSecs) * time.Second)
	}
}

// GetMediaID returns the media ID
func (m *MediaUploader) GetMediaID() string {
	return m.mediaID
}

// SetMediaID sets the media ID
func (m *MediaUploader) SetMediaID(mediaID string) {
	m.mediaID = mediaID
}

// ExecuteMediaUpload handles the media upload command execution
func ExecuteMediaUpload(filePath, mediaType, mediaCategory, authType, username string, verbose, waitForProcessing, trace bool, headers []string, client Client) error {
	uploader, err := NewMediaUploader(client, filePath, verbose, trace, authType, username, headers)
	if err != nil {
		return fmt.Errorf("error: %v", err)
	}

	if err := uploader.Init(mediaType, mediaCategory); err != nil {
		return fmt.Errorf("error initializing upload: %v", err)
	}

	if err := uploader.Append(); err != nil {
		return fmt.Errorf("error uploading media: %v", err)
	}

	finalizeResponse, err := uploader.Finalize()
	if err != nil {
		return fmt.Errorf("error finalizing upload: %v", err)
	}

	utils.FormatAndPrintResponse(finalizeResponse)

	// Wait for processing if requested
	if waitForProcessing && strings.Contains(mediaCategory, "video") {
		processingResponse, err := uploader.WaitForProcessing()
		if err != nil {
			return fmt.Errorf("error during media processing: %v", err)
		}

		utils.FormatAndPrintResponse(processingResponse)
	}

	fmt.Printf("\033[32mMedia uploaded successfully! Media ID: %s\033[0m\n", uploader.GetMediaID())
	return nil
}

// ExecuteMediaStatus handles the media status command execution
func ExecuteMediaStatus(mediaID, authType, username string, verbose, wait, trace bool, headers []string, client Client) error {
	uploader := NewMediaUploaderWithoutFile(client, verbose, trace, authType, username, headers)

	uploader.SetMediaID(mediaID)

	if wait {
		processingResponse, err := uploader.WaitForProcessing()
		if err != nil {
			return fmt.Errorf("error during media processing: %v", err)
		}

		prettyJSON, err := json.MarshalIndent(processingResponse, "", "  ")
		if err != nil {
			return fmt.Errorf("error formatting JSON: %v", err)
		}
		fmt.Println(string(prettyJSON))
	} else {
		statusResponse, err := uploader.CheckStatus()
		if err != nil {
			return fmt.Errorf("error checking status: %v", err)
		}

		prettyJSON, err := json.MarshalIndent(statusResponse, "", "  ")
		if err != nil {
			return fmt.Errorf("error formatting JSON: %v", err)
		}
		fmt.Println(string(prettyJSON))
	}

	return nil
}

// HandleMediaAppendRequest handles a media append request with a file
func HandleMediaAppendRequest(options RequestOptions, mediaFile string, client Client) (json.RawMessage, error) {
	// TODO: This function is in a weird state since append accepts either a multipart request or a json request
	// Right now, this function takes in segment_index from the json request and sends a multipart request
	// We should refactor this to handle both cases (by adding curl-like multipart request support)
	// example usage:
	// xurl -X POST "/2/media/upload/{id}/append" \
	//   -H "Content-Type: multipart/form-data" \
	//   -F "media=@/path/to/your/file.mp4" \
	//   -F "segment_index=0"
	mediaID := ExtractMediaID(options.Endpoint)
	if mediaID == "" {
		return nil, fmt.Errorf("media_id is required for append endpoint")
	}

	segmentIndex := ExtractSegmentIndex(options.Data)
	if segmentIndex == "" {
		segmentIndex = "0"
	}

	formFields := map[string]string{
		"segment_index": segmentIndex,
	}

	multipartOptions := MultipartOptions{
		RequestOptions: options,
		FormFields:     formFields,
		FileField:      "media",
		FilePath:       mediaFile,
		FileName:       filepath.Base(mediaFile),
		FileData:       []byte{},
	}

	response, clientErr := client.SendMultipartRequest(multipartOptions)

	if clientErr != nil {
		return nil, fmt.Errorf("append request failed: %v", clientErr)
	}

	return response, nil
}

// ExtractMediaID extracts media_id from URL or data
func ExtractMediaID(url string) string {
	if url == "" {
		return ""
	}

	if !strings.Contains(url, "/2/media/upload") {
		return ""
	}

	if strings.HasSuffix(url, "/2/media/upload/initialize") {
		return ""
	}

	// Extract media ID from path for append/finalize endpoints
	if strings.Contains(url, "/2/media/upload/") {
		parts := strings.Split(url, "/2/media/upload/")
		if len(parts) > 1 {
			path := parts[1]
			for _, suffix := range []string{"/append", "/finalize"} {
				if idx := strings.Index(path, suffix); idx != -1 {
					return path[:idx]
				}
			}
		}
	}

	if strings.Contains(url, "?") {
		queryParams := strings.Split(url, "?")
		if len(queryParams) > 1 {
			params := strings.Split(queryParams[1], "&")
			for _, param := range params {
				if strings.HasPrefix(param, "media_id=") {
					return strings.Split(param, "=")[1]
				}
			}
		}
	}

	return ""
}

// extracts command from URL
func ExtractCommand(url string) string {
	if strings.Contains(url, "/2/media/upload/") {
		parts := strings.Split(url, "/2/media/upload/")
		if len(parts) > 1 {
			path := parts[1]
			if strings.Contains(path, "/append") {
				return "append"
			}
			if strings.Contains(path, "/finalize") {
				return "finalize"
			}
			if path == "initialize" {
				return "initialize"
			}
		}
		return "status"
	}

	return ""
}

// ExtractSegmentIndex extracts segment_index from URL or data
func ExtractSegmentIndex(data string) string {
	var jsonData map[string]string
	if err := json.Unmarshal([]byte(data), &jsonData); err == nil {
		if segmentIndex, ok := jsonData["segment_index"]; ok {
			return segmentIndex
		}
	}
	return ""
}

// IsMediaAppendRequest checks if the request is a media append request
func IsMediaAppendRequest(url string, mediaFile string) bool {
	return strings.Contains(url, "/2/media/upload") &&
		strings.Contains(url, "append") &&
		mediaFile != ""
}
