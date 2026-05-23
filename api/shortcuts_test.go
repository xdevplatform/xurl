package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xdevplatform/xurl/config"
)

// ---------------------------------------------------------------
// Pure‑function unit tests
// ---------------------------------------------------------------

func TestResolvePostID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bare ID", "1234567890", "1234567890"},
		{"x.com URL", "https://x.com/user/status/1234567890", "1234567890"},
		{"legacy domain URL", "https://twitter.com/user/status/9876543210", "9876543210"},
		{"URL with query params", "https://x.com/user/status/111?s=20", "111"},
		{"ID with whitespace", "  1234567890  ", "1234567890"},
		{"URL without status segment", "https://x.com/user", "https://x.com/user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePostID(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveUsername(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"elonmusk", "elonmusk"},
		{"@elonmusk", "elonmusk"},
		{"  @XDev  ", "XDev"},
		{"plain", "plain"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ResolveUsername(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------
// Integration tests using httptest
// ---------------------------------------------------------------

func setupShortcutServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		// POST /2/tweets — create post
		case r.URL.Path == "/2/tweets" && r.Method == "POST":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"data":{"id":"99999","text":"Hello!"}}`))

		// DELETE /2/tweets/:id — delete post
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/2/tweets/"):
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"deleted":true}}`))

		// GET /2/tweets/search/recent — search posts
		case strings.HasPrefix(r.URL.Path, "/2/tweets/search/recent") && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":[{"id":"1","text":"result one"}],"meta":{"result_count":1}}`))

		// GET /2/tweets/:id — read post
		case strings.HasPrefix(r.URL.Path, "/2/tweets/") && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"id":"123","text":"existing post","public_metrics":{"like_count":5}}}`))

		// GET /2/users/me
		case r.URL.Path == "/2/users/me" && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"id":"42","username":"testbot","name":"Test Bot"}}`))

		// GET /2/users/by/username/:username
		case strings.HasPrefix(r.URL.Path, "/2/users/by/username/"):
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{"id":"100","username":"lookedup","name":"Looked Up"}}`))

		default:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{}}`))
		}
	}))
}

func shortcutClient(t *testing.T, server *httptest.Server) *ApiClient {
	authMock, tempDir := createMockAuth(t)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	cfg := &config.Config{APIBaseURL: server.URL}
	return NewApiClient(cfg, authMock)
}

func baseTestOpts() RequestOptions {
	return RequestOptions{Verbose: false}
}

// ---- CreatePost ----

func TestCreatePost(t *testing.T) {
	server := setupShortcutServer()
	defer server.Close()
	client := shortcutClient(t, server)

	resp, err := CreatePost(client, "Hello!", nil, baseTestOpts())
	require.NoError(t, err)

	var result struct {
		Data struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp, &result))
	assert.Equal(t, "99999", result.Data.ID)
	assert.Equal(t, "Hello!", result.Data.Text)
}

func TestCreatePostWithMedia(t *testing.T) {
	server := setupShortcutServer()
	defer server.Close()
	client := shortcutClient(t, server)

	resp, err := CreatePost(client, "With media", []string{"m1", "m2"}, baseTestOpts())
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// ---- ReplyToPost ----

func TestReplyToPost(t *testing.T) {
	server := setupShortcutServer()
	defer server.Close()
	client := shortcutClient(t, server)

	resp, err := ReplyToPost(client, "123", "nice!", nil, baseTestOpts())
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestReplyToPostWithURL(t *testing.T) {
	server := setupShortcutServer()
	defer server.Close()
	client := shortcutClient(t, server)

	resp, err := ReplyToPost(client, "https://x.com/u/status/123", "nice!", nil, baseTestOpts())
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// ---- QuotePost ----

func TestQuotePost(t *testing.T) {
	server := setupShortcutServer()
	defer server.Close()
	client := shortcutClient(t, server)

	resp, err := QuotePost(client, "123", "my take", baseTestOpts())
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// ---- DeletePost ----

func TestDeletePost(t *testing.T) {
	server := setupShortcutServer()
	defer server.Close()
	client := shortcutClient(t, server)

	resp, err := DeletePost(client, "123", baseTestOpts())
	require.NoError(t, err)

	var result struct {
		Data struct {
			Deleted bool `json:"deleted"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp, &result))
	assert.True(t, result.Data.Deleted)
}

// ---- ReadPost ----

func TestReadPost(t *testing.T) {
	server := setupShortcutServer()
	defer server.Close()
	client := shortcutClient(t, server)

	resp, err := ReadPost(client, "123", baseTestOpts())
	require.NoError(t, err)

	var result struct {
		Data struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp, &result))
	assert.Equal(t, "123", result.Data.ID)
}

// ---- SearchPosts ----

func TestSearchPosts(t *testing.T) {
	server := setupShortcutServer()
	defer server.Close()
	client := shortcutClient(t, server)

	resp, err := SearchPosts(client, "golang", 10, PaginationOptions{}, baseTestOpts())
	require.NoError(t, err)

	var result struct {
		Meta struct {
			ResultCount int `json:"result_count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(resp, &result))
	assert.Equal(t, 1, result.Meta.ResultCount)
}

// ---- GetMe ----

func TestGetMe(t *testing.T) {
	server := setupShortcutServer()
	defer server.Close()
	client := shortcutClient(t, server)

	resp, err := GetMe(client, baseTestOpts())
	require.NoError(t, err)

	var result struct {
		Data struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp, &result))
	assert.Equal(t, "42", result.Data.ID)
	assert.Equal(t, "testbot", result.Data.Username)
}

// ---- LookupUser ----

func TestLookupUser(t *testing.T) {
	server := setupShortcutServer()
	defer server.Close()
	client := shortcutClient(t, server)

	resp, err := LookupUser(client, "@someuser", baseTestOpts())
	require.NoError(t, err)

	var result struct {
		Data struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(resp, &result))
	assert.Equal(t, "100", result.Data.ID)
	assert.Equal(t, "lookedup", result.Data.Username)
}
