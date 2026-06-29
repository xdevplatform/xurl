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

// handleRequestError processes API client errors in a consistent way. When the
// error carries a JSON body (an API error response) it is pretty-printed and a
// generic failure is returned; otherwise the original error (e.g. a network or
// auth failure) is returned unchanged so its real message reaches the user.
func handleRequestError(clientErr error) error {
	var rawJSON json.RawMessage
	if json.Unmarshal([]byte(clientErr.Error()), &rawJSON) == nil {
		utils.FormatAndPrintResponse(rawJSON)
		return fmt.Errorf("request failed")
	}
	return clientErr
}

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
