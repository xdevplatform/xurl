package api

import (
	"encoding/json"
	"fmt"
	"xurl/utils"
)

// ExecuteRequest handles the execution of a regular API request
func ExecuteRequest(method, url string, headers []string, data, authType, username string, verbose bool, client Client) error {
	options := RequestOptions{
		Method:   method,
		Endpoint: url,
		Headers:  headers,
		Data:     data,
		AuthType: authType,
		Username: username,
		Verbose:  verbose,
	}

	response, clientErr := client.SendRequest(options)
	if clientErr != nil {
		return handleRequestError(clientErr)
	}

	return utils.FormatAndPrintResponse(response)
}

// ExecuteStreamRequest handles the execution of a streaming API request
func ExecuteStreamRequest(method, url string, headers []string, data, authType, username string, verbose bool, client Client) error {
	options := RequestOptions{
		Method:   method,
		Endpoint: url,
		Headers:  headers,
		Data:     data,
		AuthType: authType,
		Username: username,
		Verbose:  verbose,
	}

	clientErr := client.StreamRequest(options)
	if clientErr != nil {
		return handleRequestError(clientErr)
	}

	return nil
}

// handleRequestError processes API client errors in a consistent way
func handleRequestError(clientErr error) error {
	var rawJSON json.RawMessage
	json.Unmarshal([]byte(clientErr.Error()), &rawJSON)
	utils.FormatAndPrintResponse(rawJSON)
	return fmt.Errorf("request failed")
}

// formatAndPrintResponse formats and prints API responses

// HandleRequest determines the type of request and executes it accordingly
func HandleRequest(method, url string, headers []string, data, authType, username string, verbose, forceStream bool, mediaFile string, client Client) error {
	if IsMediaAppendRequest(url, mediaFile) {
		response, err := HandleMediaAppendRequest(url, mediaFile, method, headers, data, authType, username, verbose, client)
		if err != nil {
			return err
		}

		return utils.FormatAndPrintResponse(response)
	}

	shouldStream := forceStream || IsStreamingEndpoint(url)

	if shouldStream {
		return ExecuteStreamRequest(method, url, headers, data, authType, username, verbose, client)
	} else {
		return ExecuteRequest(method, url, headers, data, authType, username, verbose, client)
	}
}
