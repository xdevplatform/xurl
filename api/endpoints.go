package api

import (
	"strings"
)

// StreamingEndpoints is a map of endpoint prefixes that should be streamed
var StreamingEndpoints = map[string]bool{
	"/2/tweets/search/stream":           true,
	"/2/tweets/sample/stream":           true,
	"/2/tweets/sample10/stream":         true,
	"/2/tweets/firehose/stream":         true,
	"/2/tweets/firehose/stream/lang/en": true,
	"/2/tweets/firehose/stream/lang/ja": true,
	"/2/tweets/firehose/stream/lang/ko": true,
	"/2/tweets/firehose/stream/lang/pt": true,
}

// IsStreamingEndpoint checks if an endpoint should be streamed
func IsStreamingEndpoint(endpoint string) bool {
	path := endpoint
	if strings.HasPrefix(strings.ToLower(endpoint), "http") {
		parsedURL := strings.SplitN(endpoint, "/", 4)
		if len(parsedURL) >= 4 {
			path = "/" + parsedURL[3]
		}
	}

	normalizedEndpoint := strings.TrimSuffix(path, "/")

	return StreamingEndpoints[normalizedEndpoint]
}
