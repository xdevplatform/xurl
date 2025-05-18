package api

import (
	"encoding/json"
	"fmt"

	"github.com/xdevplatform/xurl/utils"
)

// ExecuteRequest handles the execution of a regular API request
func ExecuteRequest(options RequestOptions, client Client) error {

	response, clientErr := client.SendRequest(options)
	if clientErr != nil {
		return handleRequestError(clientErr)
	}

	return utils.FormatAndPrintResponse(response)
}

// ExecuteStreamRequest handles the execution of a streaming API request
func ExecuteStreamRequest(options RequestOptions, client Client) error {

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
func HandleRequest(options RequestOptions, forceStream bool, mediaFile string, client Client) error {
	if IsMediaAppendRequest(options.Endpoint, mediaFile) {
		response, err := HandleMediaAppendRequest(options, mediaFile, client)
		if err != nil {
			return err
		}

		return utils.FormatAndPrintResponse(response)
	}

	shouldStream := forceStream || IsStreamingEndpoint(options.Endpoint)

	if shouldStream {
		return ExecuteStreamRequest(options, client)
	} else {
		return ExecuteRequest(options, client)
	}
}
