package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/config"
	"github.com/xdevplatform/xurl/store"
)

// mcpTestAuth returns an *auth.Auth backed by a temp store holding a single
// non-expired OAuth2 token, so token resolution never hits the network.
func mcpTestAuth(t *testing.T, accessToken string) *auth.Auth {
	t.Helper()
	return mcpTestAuthRefreshable(t, accessToken, "")
}

// mcpTestAuthRefreshable is like mcpTestAuth but also wires a token URL so a
// forced refresh (e.g. on 401) can mint a new token.
func mcpTestAuthRefreshable(t *testing.T, accessToken, tokenURL string) *auth.Auth {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "xurl_mcp_test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	ts := &store.TokenStore{
		Apps:       map[string]*store.App{"default": {OAuth2Tokens: map[string]store.Token{}}},
		DefaultApp: "default",
		FilePath:   filepath.Join(tempDir, ".xurl"),
	}
	future := uint64(time.Now().Add(time.Hour).Unix())
	require.NoError(t, ts.SaveOAuth2TokenForApp("default", "alice", accessToken, "refresh", future))

	return auth.NewAuth(&config.Config{TokenURL: tokenURL}).WithTokenStore(ts)
}

// assertStdoutIsJSONLines fails if any non-empty stdout line isn't valid JSON.
func assertStdoutIsJSONLines(t *testing.T, out string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		assert.Truef(t, json.Valid([]byte(line)), "stdout line is not valid JSON: %q", line)
	}
}

// nonPostBoilerplate handles the GET (no standalone stream) and DELETE (session
// teardown) requests a test mock must tolerate, returning true if it handled the
// request. POST handling is left to the caller.
func nonPostBoilerplate(w http.ResponseWriter, r *http.Request) bool {
	switch r.Method {
	case http.MethodGet:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	case http.MethodDelete:
		w.WriteHeader(http.StatusOK)
		return true
	}
	return false
}

func TestMCPBridgeJSONResponse(t *testing.T) {
	var mu sync.Mutex
	var gotAuth, gotAccept, gotCT string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nonPostBoilerplate(w, r) {
			return
		}
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotCT = r.Header.Get("Content-Type")
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-123")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
	}))
	defer server.Close()

	a := mcpTestAuth(t, "tok-abc")
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n")
	var out bytes.Buffer

	b := newMCPBridgeWithIO(server.URL, a, "", in, &out)
	require.NoError(t, b.run(context.Background()))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "Bearer tok-abc", gotAuth, "bearer token must be injected")
	assert.Contains(t, gotAccept, "application/json")
	assert.Contains(t, gotAccept, "text/event-stream")
	assert.Contains(t, gotCT, "application/json")

	assert.Contains(t, out.String(), `"protocolVersion":"2025-03-26"`)
	assertStdoutIsJSONLines(t, out.String())
	assert.Equal(t, "sess-123", b.getSession(), "session id must be captured")
}

func TestMCPBridgeSSEResponse(t *testing.T) {
	var mu sync.Mutex
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nonPostBoilerplate(w, r) {
			return
		}
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		io.WriteString(w, ": keep-alive\n\n")
		io.WriteString(w, "event: message\n")
		io.WriteString(w, `data: {"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"hi"}]}}`+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer server.Close()

	a := mcpTestAuth(t, "tok-sse")
	in := strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call"}` + "\n")
	var out bytes.Buffer

	b := newMCPBridgeWithIO(server.URL, a, "", in, &out)
	require.NoError(t, b.run(context.Background()))

	mu.Lock()
	assert.Equal(t, "Bearer tok-sse", gotAuth)
	mu.Unlock()
	assert.Contains(t, out.String(), `"id":2`)
	assert.Contains(t, out.String(), `"text":"hi"`)
	assertStdoutIsJSONLines(t, out.String())
}

// TestMCPBridgeForwardsSessionID verifies that once the server assigns a
// session id, it is echoed on subsequent requests. Driven sequentially via
// forwardPost so ordering is deterministic.
func TestMCPBridgeForwardsSessionID(t *testing.T) {
	var mu sync.Mutex
	var seenSessions []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nonPostBoilerplate(w, r) {
			return
		}
		mu.Lock()
		seenSessions = append(seenSessions, r.Header.Get("Mcp-Session-Id"))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-xyz")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer server.Close()

	a := mcpTestAuth(t, "tok-1")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(server.URL, a, "", strings.NewReader(""), &out)

	ctx := context.Background()
	b.forwardPost(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	require.Equal(t, "sess-xyz", b.getSession())
	b.forwardPost(ctx, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seenSessions, 2)
	assert.Equal(t, "", seenSessions[0], "first request has no session id yet")
	assert.Equal(t, "sess-xyz", seenSessions[1], "second request must carry the session id")
	assertStdoutIsJSONLines(t, out.String())
}

// TestMCPBridgeAcceptedNoBody verifies that a 202 (e.g. for a notification)
// produces no stdout output.
func TestMCPBridgeAcceptedNoBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nonPostBoilerplate(w, r) {
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	a := mcpTestAuth(t, "tok-2")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(server.URL, a, "", strings.NewReader(""), &out)

	b.forwardPost(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	assert.Empty(t, strings.TrimSpace(out.String()), "202 responses must not write to stdout")
}

// TestMCPBridge401ForcesRefresh verifies that a 401 triggers a forced token
// refresh and the retry carries the new token.
func TestMCPBridge401ForcesRefresh(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "new-refresh",
		})
	}))
	defer tokenServer.Close()

	var mu sync.Mutex
	var seenAuth []string
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nonPostBoilerplate(w, r) {
			return
		}
		auth := r.Header.Get("Authorization")
		mu.Lock()
		seenAuth = append(seenAuth, auth)
		mu.Unlock()
		if auth == "Bearer old-access" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	}))
	defer mcpServer.Close()

	a := mcpTestAuthRefreshable(t, "old-access", tokenServer.URL+"/token")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(mcpServer.URL, a, "", strings.NewReader(""), &out)

	b.forwardPost(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(seenAuth), 2, "expected a retry after 401")
	assert.Equal(t, "Bearer old-access", seenAuth[0])
	assert.Equal(t, "Bearer new-access", seenAuth[len(seenAuth)-1], "retry must carry the refreshed token")
	assert.Contains(t, out.String(), `"ok":true`)
	assertStdoutIsJSONLines(t, out.String())
}

// TestMCPBridgeErrorStatusJSONForwarded verifies a JSON error body on a >=400
// response is forwarded to stdout (so the client surfaces the JSON-RPC error).
func TestMCPBridgeErrorStatusJSONForwarded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nonPostBoilerplate(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad request"}}`)
	}))
	defer server.Close()

	a := mcpTestAuth(t, "tok-err")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(server.URL, a, "", strings.NewReader(""), &out)

	b.forwardPost(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"x"}`))
	assert.Contains(t, out.String(), `"error"`)
	assert.Contains(t, out.String(), `"bad request"`)
	assertStdoutIsJSONLines(t, out.String())
}

// TestMCPBridgePumpSSEMultilineAndNonJSON verifies multi-line data is reassembled
// + compacted into one JSON line, and non-JSON keep-alives are dropped.
func TestMCPBridgePumpSSEMultilineAndNonJSON(t *testing.T) {
	a := mcpTestAuth(t, "tok")
	var out bytes.Buffer
	b := newMCPBridgeWithIO("", a, "", strings.NewReader(""), &out)

	sse := strings.Join([]string{
		": keep-alive",
		"",
		`data: {"jsonrpc":"2.0",`,
		`data: "id":5}`,
		"",
		"data: ping",
		"",
	}, "\n")

	b.pumpSSE(strings.NewReader(sse))

	got := strings.TrimSpace(out.String())
	assert.Contains(t, got, `{"jsonrpc":"2.0","id":5}`, "multi-line data must compact to one JSON line")
	assert.NotContains(t, got, "ping", "non-JSON keep-alive must be dropped")
	assertStdoutIsJSONLines(t, out.String())
}

// TestMCPBridgeOpenServerStreamSSE verifies the standalone server->client GET
// stream forwards messages to stdout.
func TestMCPBridgeOpenServerStreamSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "Bearer tok-get", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `data: {"jsonrpc":"2.0","method":"notifications/progress","params":{"p":1}}`+"\n\n")
	}))
	defer server.Close()

	a := mcpTestAuth(t, "tok-get")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(server.URL, a, "", strings.NewReader(""), &out)

	status, eventStream, err := b.openServerStream(context.Background())
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.True(t, eventStream, "a text/event-stream 200 must be reported as an event stream")
	assert.Contains(t, out.String(), `"notifications/progress"`)
	assertStdoutIsJSONLines(t, out.String())
}

// TestMCPBridgeRunReturnsOnContextCancel verifies the bridge shuts down when its
// context is cancelled even though stdin never reaches EOF.
func TestMCPBridgeRunReturnsOnContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nonPostBoilerplate(w, r) {
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	pr, pw := io.Pipe() // reads block forever until we close pw
	defer pw.Close()

	a := mcpTestAuth(t, "tok")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(server.URL, a, "", pr, &out)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.run(ctx) }()

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return after context cancellation")
	}
}

// parseRPCError decodes a single stdout line as a JSON-RPC error response.
func parseRPCError(t *testing.T, line string) (id string, code int, message string) {
	t.Helper()
	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(line), &resp))
	assert.Equal(t, "2.0", resp.JSONRPC)
	require.NotNil(t, resp.Error, "expected a JSON-RPC error member")
	return string(resp.ID), resp.Error.Code, resp.Error.Message
}

// TestMCPBridgeTransportFailureSynthesizesError verifies that when the server is
// unreachable, a request (with an id) still gets a synthesized JSON-RPC error so
// a strict client does not hang.
func TestMCPBridgeTransportFailureSynthesizesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // now connections to url are refused

	a := mcpTestAuth(t, "tok")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(url, a, "", strings.NewReader(""), &out)

	b.forwardPost(context.Background(), []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/list"}`))

	line := strings.TrimSpace(out.String())
	require.NotEmpty(t, line, "a request must get a synthesized error reply on transport failure")
	assertStdoutIsJSONLines(t, out.String())
	id, code, msg := parseRPCError(t, line)
	assert.Equal(t, "7", id)
	assert.Equal(t, rpcErrTransport, code)
	assert.NotEmpty(t, msg)
}

// TestMCPBridgeNotificationNoSynthesisOnFailure verifies a notification (no id)
// does NOT get a synthesized reply even when the server is unreachable.
func TestMCPBridgeNotificationNoSynthesisOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	a := mcpTestAuth(t, "tok")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(url, a, "", strings.NewReader(""), &out)

	b.forwardPost(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/cancelled"}`))
	assert.Empty(t, strings.TrimSpace(out.String()), "notifications must not get a synthesized reply")
}

// TestMCPBridgeErrorStatusEmptyBodySynthesizesError verifies a >=400 response
// with no usable body yields a synthesized error keyed to the request id.
func TestMCPBridgeErrorStatusEmptyBodySynthesizesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nonPostBoilerplate(w, r) {
			return
		}
		w.WriteHeader(http.StatusInternalServerError) // no body
	}))
	defer server.Close()

	a := mcpTestAuth(t, "tok")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(server.URL, a, "", strings.NewReader(""), &out)

	b.forwardPost(context.Background(), []byte(`{"jsonrpc":"2.0","id":9,"method":"x"}`))
	line := strings.TrimSpace(out.String())
	require.NotEmpty(t, line, "a 500 with no body must yield a synthesized error reply")
	assertStdoutIsJSONLines(t, out.String())
	id, code, _ := parseRPCError(t, line)
	assert.Equal(t, "9", id)
	assert.Equal(t, rpcErrUpstream, code)
}

// TestMCPBridgeResponseNoSynthesisOnFailure verifies a client->server RESPONSE
// (an id but no method) does NOT get a synthesized error reply on forward
// failure -- only genuine requests do.
func TestMCPBridgeResponseNoSynthesisOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	a := mcpTestAuth(t, "tok")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(url, a, "", strings.NewReader(""), &out)

	// A response carries an id + result but no method.
	b.forwardPost(context.Background(), []byte(`{"jsonrpc":"2.0","id":5,"result":{"ok":true}}`))
	assert.Empty(t, strings.TrimSpace(out.String()), "a client->server response must not get a synthesized reply")
}

// TestMCPBridgeNonJSONRPCErrorBodySynthesizes verifies a >=400 body that is valid
// JSON but not a JSON-RPC response (e.g. a gateway error with no id) is not
// forwarded verbatim; a correlatable error keyed to the request id is
// synthesized instead so a strict client cannot hang.
func TestMCPBridgeNonJSONRPCErrorBodySynthesizes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nonPostBoilerplate(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"title":"Too Many Requests","status":429}`)
	}))
	defer server.Close()

	a := mcpTestAuth(t, "tok")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(server.URL, a, "", strings.NewReader(""), &out)

	b.forwardPost(context.Background(), []byte(`{"jsonrpc":"2.0","id":11,"method":"tools/call"}`))
	line := strings.TrimSpace(out.String())
	require.NotEmpty(t, line, "a non-JSON-RPC 4xx body must yield a synthesized reply")
	assertStdoutIsJSONLines(t, out.String())
	id, code, _ := parseRPCError(t, line)
	assert.Equal(t, "11", id)
	assert.Equal(t, rpcErrUpstream, code)
	assert.NotContains(t, line, `"title"`, "the non-correlatable gateway body must not be forwarded as the reply")
}

// TestMCPBridgeLargeErrorBodyForwardedNotTruncated verifies a large-but-valid
// JSON error body (bigger than the old 64 KiB cap) is forwarded whole rather
// than truncated into invalid JSON (which would be dropped and synthesized).
func TestMCPBridgeLargeErrorBodyForwardedNotTruncated(t *testing.T) {
	big := strings.Repeat("a", 100*1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nonPostBoilerplate(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"`+big+`"}}`)
	}))
	defer server.Close()

	a := mcpTestAuth(t, "tok")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(server.URL, a, "", strings.NewReader(""), &out)

	b.forwardPost(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"x"}`))
	got := strings.TrimSpace(out.String())
	assertStdoutIsJSONLines(t, out.String())
	assert.Contains(t, got, big, "large valid JSON error body must be forwarded whole")
	assert.Equal(t, 1, len(strings.Split(got, "\n")), "the server body is the reply; no second synthesized error")
}

// TestMCPBridgeNotificationNotHeadOfLineBlocked verifies a notification can reach
// the server while a request's SSE response is still in flight. The server holds
// the request's stream open until the notification arrives; if notifications were
// blocked behind the in-flight request, run() would never complete.
func TestMCPBridgeNotificationNotHeadOfLineBlocked(t *testing.T) {
	gotNotif := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		case http.MethodDelete:
			w.WriteHeader(http.StatusOK)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("notifications/cancelled")) {
			close(gotNotif)
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// The long-running request: only stream the reply once the concurrent
		// notification has been received.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		select {
		case <-gotNotif:
		case <-time.After(3 * time.Second): // fail-safe so the server never hangs
		}
		io.WriteString(w, `data: {"jsonrpc":"2.0","id":1,"result":{"done":true}}`+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer server.Close()

	a := mcpTestAuth(t, "tok")
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call"}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/cancelled"}` + "\n")
	var out bytes.Buffer
	b := newMCPBridgeWithIO(server.URL, a, "", in, &out)

	done := make(chan error, 1)
	go func() { done <- b.run(context.Background()) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("run did not complete; the notification was head-of-line blocked behind the in-flight request")
	}
	assert.Contains(t, out.String(), `"done":true`)
	assertStdoutIsJSONLines(t, out.String())
}

// TestMCPBridgeBootstrapFailsFastWithoutToken verifies the bridge does NOT try
// to launch a browser when no token exists; it fails fast with guidance to
// authenticate out-of-band (including the --headless option).
func TestMCPBridgeBootstrapFailsFastWithoutToken(t *testing.T) {
	tempDir := t.TempDir()
	ts := &store.TokenStore{
		Apps:       map[string]*store.App{"default": {OAuth2Tokens: map[string]store.Token{}}},
		DefaultApp: "default",
		FilePath:   filepath.Join(tempDir, ".xurl"),
	}
	a := auth.NewAuth(&config.Config{}).WithTokenStore(ts)
	b := newMCPBridgeWithIO("http://127.0.0.1:0", a, "", strings.NewReader(""), &bytes.Buffer{})

	err := b.bootstrap()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "xurl auth oauth2")
	assert.Contains(t, err.Error(), "--headless")
}

// TestReadLineCapped verifies lines are returned intact under the cap, and lines
// over the cap are flagged oversized without buffering past the cap.
func TestReadLineCapped(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("hello\ntoolongline\nok\n"))
	const max = 8

	line, oversized, err := readLineCapped(r, max)
	require.NoError(t, err)
	assert.False(t, oversized)
	assert.Equal(t, "hello", string(line))

	line, oversized, err = readLineCapped(r, max)
	require.NoError(t, err)
	assert.True(t, oversized, "a line longer than max must be flagged oversized")
	assert.LessOrEqual(t, len(line), max, "buffered bytes must not exceed the cap")

	line, oversized, err = readLineCapped(r, max)
	require.NoError(t, err)
	assert.False(t, oversized)
	assert.Equal(t, "ok", string(line))
}
