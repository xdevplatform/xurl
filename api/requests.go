package api

import (
	"encoding/json"
	"fmt"
	"xurl/utils"
)

// ExecuteRequest handles the execution of a regular API request
func ExecuteRequest(method, url string, headers []string, data, authType, username string, verbose bool, client *ApiClient) error {
	response, clientErr := client.SendRequest(method, url, headers, data, authType, username, verbose)
	if clientErr != nil {
		var rawJSON json.RawMessage
		json.Unmarshal([]byte(clientErr.Message), &rawJSON)
		prettyJSON, _ := json.MarshalIndent(rawJSON, "", "  ")
		utils.ColorizeAndPrintJSON(string(prettyJSON))
		return fmt.Errorf("request failed")
	}

	prettyJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return fmt.Errorf("error formatting JSON: %v", err)
	}
	
	utils.ColorizeAndPrintJSON(string(prettyJSON))
	
	return nil
}

// ExecuteStreamRequest handles the execution of a streaming API request
func ExecuteStreamRequest(method, url string, headers []string, data, authType, username string, verbose bool, client *ApiClient) error {
	clientErr := client.StreamRequest(method, url, headers, data, authType, username, verbose)
	if clientErr != nil {
		var rawJSON json.RawMessage
		json.Unmarshal([]byte(clientErr.Message), &rawJSON)
		prettyJSON, _ := json.MarshalIndent(rawJSON, "", "  ")
		fmt.Println(string(prettyJSON))
		return fmt.Errorf("streaming request failed")
	}
	
	return nil
}

// HandleRequest determines the type of request and executes it accordingly
func HandleRequest(method, url string, headers []string, data, authType, username string, verbose, forceStream bool, mediaFile string, client *ApiClient) error {
	if IsMediaAppendRequest(url, mediaFile) {
		response, err := HandleMediaAppendRequest(url, mediaFile, method, headers, data, authType, username, verbose, client)
		if err != nil {
			return err
		}
		
		prettyJSON, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return fmt.Errorf("error formatting JSON: %v", err)
		}
		utils.ColorizeAndPrintJSON(string(prettyJSON))
		return nil
	}
	
	shouldStream := forceStream || IsStreamingEndpoint(url)

	if shouldStream {
		return ExecuteStreamRequest(method, url, headers, data, authType, username, verbose, client)
	} else {
		return ExecuteRequest(method, url, headers, data, authType, username, verbose, client)
	}
} 