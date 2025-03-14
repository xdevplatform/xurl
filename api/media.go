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
	client *ApiClient
	mediaID string
	filePath string
	fileSize int64
	verbose bool
	authType string
	username string
	headers []string
}

// NewMediaUploader creates a new MediaUploader
func NewMediaUploader(client *ApiClient, filePath string, verbose bool, authType string, username string, headers []string) (*MediaUploader, error) {
	// Check if file exists
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("error accessing file: %v", err)
	}

	// Check if it's a regular file
	if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filePath)
	}

	return &MediaUploader{
		client: client,
		filePath: filePath,
		fileSize: fileInfo.Size(),
		verbose: verbose,
		authType: authType,
		username: username,
		headers: headers,
	}, nil
}

// Init initializes the media upload
func (m *MediaUploader) Init(mediaType string, mediaCategory string) error {
	if m.verbose {
		fmt.Printf("\033[32mInitializing media upload...\033[0m\n")
	}

	finalUrl := MediaEndpoint + 
		"?command=INIT" + 
		"&total_bytes=" + strconv.FormatInt(m.fileSize, 10) + 
		"&media_type=" + mediaType + 
		"&media_category=" + mediaCategory

	response, clientErr := m.client.SendRequest("POST", finalUrl, m.headers, "", m.authType, m.username, m.verbose)
	if clientErr != nil {
		return fmt.Errorf("init request failed: %v", clientErr)
	}

	var initResponse struct {
		Data struct {
			ID string `json:"id"`
			ExpiresAfterSecs int `json:"expires_after_secs"`
			MediaKey string `json:"media_key"`
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

		// Prepare form fields
		formFields := map[string]string{
			"command": "APPEND",
			"media_id": m.mediaID,
			"segment_index": strconv.Itoa(segmentIndex),
		}

		// Send multipart request with buffer
		_, clientErr := m.client.SendMultipartRequestWithBuffer(
			"POST",
			MediaEndpoint,
			m.headers,
			formFields,
			"media",
			filepath.Base(m.filePath),
			buffer[:bytesRead],
			m.authType,
			m.username,
			m.verbose,
		)

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

	finalUrl := MediaEndpoint + "?command=FINALIZE&media_id=" + m.mediaID
	response, clientErr := m.client.SendRequest("POST", finalUrl, m.headers, "", m.authType, m.username, m.verbose)
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

	response, clientErr := m.client.SendRequest("GET", url, []string{}, "", m.authType, m.username, m.verbose)
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
					State string `json:"state"`
					CheckAfterSecs int `json:"check_after_secs"`
					ProgressPercent int `json:"progress_percent"`
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
func ExecuteMediaUpload(filePath, mediaType, mediaCategory, authType, username string, verbose, waitForProcessing bool, headers []string, client *ApiClient) error {
	uploader, err := NewMediaUploader(client, filePath, verbose, authType, username, headers)
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
func ExecuteMediaStatus(mediaID, authType, username string, verbose, wait bool, headers []string, client *ApiClient) error {
	// Create media uploader
	uploader, err := NewMediaUploader(client, "", verbose, authType, username, headers)
	if err != nil {
		return fmt.Errorf("error: %v", err)
	}
	
	// Set media ID
	uploader.SetMediaID(mediaID)
	
	if wait {
		// Wait for processing
		processingResponse, err := uploader.WaitForProcessing()
		if err != nil {
			return fmt.Errorf("error during media processing: %v", err)
		}
		
		// Pretty print the processing response
		prettyJSON, err := json.MarshalIndent(processingResponse, "", "  ")
		if err != nil {
			return fmt.Errorf("error formatting JSON: %v", err)
		}
		fmt.Println(string(prettyJSON))
	} else {
		// Just check status once
		statusResponse, err := uploader.CheckStatus()
		if err != nil {
			return fmt.Errorf("error checking status: %v", err)
		}
		
		// Pretty print the status response
		prettyJSON, err := json.MarshalIndent(statusResponse, "", "  ")
		if err != nil {
			return fmt.Errorf("error formatting JSON: %v", err)
		}
		fmt.Println(string(prettyJSON))
	}
	
	return nil
}

// HandleMediaAppendRequest handles a media append request with a file
func HandleMediaAppendRequest(url, mediaFile, method string, headers []string, data, authType, username string, verbose bool, client *ApiClient) (json.RawMessage, error) {
	mediaID := ExtractMediaID(url, data)
	if mediaID == "" {
		return nil, fmt.Errorf("media_id is required for APPEND command")
	}
	
	segmentIndex := ExtractSegmentIndex(url, data)
	if segmentIndex == "" {
		segmentIndex = "0" // Default to 0 if not specified
	}
	
	formFields := map[string]string{
		"command": "APPEND",
		"media_id": mediaID,
		"segment_index": segmentIndex,
	}
	
	response, clientErr := client.SendMultipartRequest(
		method,
		url,
		headers,
		formFields,
		"media",
		mediaFile,
		authType,
		username,
		verbose,
	)
	
	if clientErr != nil {
		return nil, fmt.Errorf("append request failed: %v", clientErr)
	}
	
	return response, nil
}

// ExtractMediaID extracts media_id from URL or data
func ExtractMediaID(url string, data string) string {
	if strings.Contains(url, "media_id=") {
		parts := strings.Split(url, "media_id=")
		if len(parts) > 1 {
			mediaID := parts[1]
			if idx := strings.Index(mediaID, "&"); idx != -1 {
				mediaID = mediaID[:idx]
			}
			return mediaID
		}
	}
	
	if strings.Contains(data, "media_id=") {
		parts := strings.Split(data, "media_id=")
		if len(parts) > 1 {
			mediaID := parts[1]
			if idx := strings.Index(mediaID, "&"); idx != -1 {
				mediaID = mediaID[:idx]
			}
			return mediaID
		}
	}
	
	return ""
}

// ExtractSegmentIndex extracts segment_index from URL or data
func ExtractSegmentIndex(url string, data string) string {
	if strings.Contains(url, "segment_index=") {
		parts := strings.Split(url, "segment_index=")
		if len(parts) > 1 {
			segmentIndex := parts[1]
			if idx := strings.Index(segmentIndex, "&"); idx != -1 {
				segmentIndex = segmentIndex[:idx]
			}
			return segmentIndex
		}
	}
	
	if strings.Contains(data, "segment_index=") {
		parts := strings.Split(data, "segment_index=")
		if len(parts) > 1 {
			segmentIndex := parts[1]
			if idx := strings.Index(segmentIndex, "&"); idx != -1 {
				segmentIndex = segmentIndex[:idx]
			}
			return segmentIndex
		}
	}
	
	return ""
}

// IsMediaAppendRequest checks if the request is a media append request
func IsMediaAppendRequest(url string, mediaFile string) bool {
	return strings.Contains(url, "/2/media/upload") && 
		strings.Contains(url, "command=APPEND") && 
		mediaFile != ""
} 