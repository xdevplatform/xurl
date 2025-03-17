package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsStreamingEndpoint(t *testing.T) {
	testCases := []struct {
		endpoint string
		expected bool
	}{
		// Test exact matches
		{"/2/tweets/search/stream", true},
		{"/2/tweets/sample/stream", true},
		{"/2/tweets/sample10/stream", true},
		{"/2/tweets/firehose/stream", true},
		{"/2/tweets/firehose/stream/lang/en", true},
		{"/2/tweets/firehose/stream/lang/ja", true},
		{"/2/tweets/firehose/stream/lang/ko", true},
		{"/2/tweets/firehose/stream/lang/pt", true},

		// Test with trailing slash
		{"/2/tweets/search/stream/", true},

		// Test with query parameters
		{"/2/tweets/search/stream?query=test", true},

		// Test with full URL
		{"https://api.x.com/2/tweets/search/stream", true},
		{"http://api.x.com/2/tweets/search/stream", true},
		{"https://api.x.com/2/tweets/search/stream?query=test", true},

		// Test non-streaming endpoints
		{"/2/tweets/search/recent", false},
		{"/2/users/me", false},
		{"https://api.x.com/2/users/me", false},
		{"/not/a/streaming/endpoint", false},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.endpoint, func(t *testing.T) {
			result := IsStreamingEndpoint(tc.endpoint)
			assert.Equal(t, tc.expected, result, "IsStreamingEndpoint(%q) should return %v", tc.endpoint, tc.expected)
		})
	}
}
