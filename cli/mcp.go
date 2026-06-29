package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/version"
)

// defaultMCPURL is the hosted X API MCP endpoint used when no URL is given.
const defaultMCPURL = "https://api.x.com/mcp"

// maxMCPMessageBytes bounds a single JSON-RPC message (stdin line or SSE event).
const maxMCPMessageBytes = 16 * 1024 * 1024

// JSON-RPC error codes the bridge uses when it must synthesize a reply for a
// client request it could not satisfy, so a strict client never hangs waiting
// for the matching id. These sit in the implementation-defined server-error
// range (-32000..-32099) reserved by the JSON-RPC 2.0 spec.
const (
	rpcErrTransport = -32001 // could not reach the MCP server
	rpcErrAuth      = -32002 // token refresh failed after a 401
	rpcErrUpstream  = -32003 // server replied but no usable message could be forwarded
)

// rpcError is the "error" member of a synthesized JSON-RPC error response.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// mcpBridge relays MCP traffic between a stdio client and a remote Streamable
// HTTP MCP server, injecting a Bearer token on every outbound request. It
// implements the client side of the MCP Streamable HTTP transport (2025-03-26):
// client->server messages are POSTed; the server replies with either a single
// JSON object or a text/event-stream of JSON-RPC messages, and may assign an
// Mcp-Session-Id that must be echoed on subsequent requests.
//
// Client messages are processed sequentially (stdio MCP is a serial channel),
// which guarantees the initialize handshake establishes the session id before
// later requests are sent. A best-effort background goroutine consumes the
// optional standalone server->client SSE stream.
type mcpBridge struct {
	url        string
	auth       *auth.Auth
	username   string
	httpClient *http.Client

	// tokenMu serialises all access to the (mutex-less) token store, so the
	// message loop and the server->client listener never refresh/persist
	// concurrently (which would be a fatal map race and could corrupt ~/.xurl).
	tokenMu sync.Mutex

	in  io.Reader
	out io.Writer

	outMu sync.Mutex // serialises writes to out (stdout)

	sessMu       sync.Mutex
	sessionID    string
	sessionOnce  sync.Once
	sessionReady chan struct{}
}

func newMCPBridge(url string, a *auth.Auth, username string) *mcpBridge {
	return newMCPBridgeWithIO(url, a, username, os.Stdin, os.Stdout)
}

func newMCPBridgeWithIO(url string, a *auth.Auth, username string, in io.Reader, out io.Writer) *mcpBridge {
	return &mcpBridge{
		url:      url,
		auth:     a,
		username: username,
		// No client timeout: SSE responses and the server->client stream are
		// long-lived; cancellation is driven by the request context instead.
		httpClient:   &http.Client{},
		in:           in,
		out:          out,
		sessionReady: make(chan struct{}),
	}
}

// accessToken returns a valid token using the same resolution as `xurl token`
// (refresh-if-expired, persist, never browser), serialised under tokenMu.
func (b *mcpBridge) accessToken() (string, error) {
	b.tokenMu.Lock()
	defer b.tokenMu.Unlock()
	return b.auth.GetValidOAuth2Token(b.username)
}

// forceRefreshToken mints a brand-new token regardless of local expiry. Used on
// an HTTP 401, where the server rejected a token the local clock still trusts.
func (b *mcpBridge) forceRefreshToken() (string, error) {
	b.tokenMu.Lock()
	defer b.tokenMu.Unlock()
	return b.auth.ForceRefreshOAuth2Token(b.username)
}

// bootstrap ensures a usable token exists before bridging. It will silently
// refresh an expired token, but it never launches a browser: the bridge's stdio
// is the MCP channel (owned by the client) and a login prompt mid-startup would
// hang the client's handshake and corrupt stdout. If no token is available it
// fails fast with instructions to authenticate out-of-band first.
func (b *mcpBridge) bootstrap() error {
	if _, err := b.accessToken(); err == nil {
		return nil
	}
	hint := appFlagHint(b.auth.AppName())
	return fmt.Errorf("no valid OAuth2 token for this app. Authenticate first, then start the MCP server:\n"+
		"  xurl auth oauth2%s             # local machine with a browser\n"+
		"  xurl auth oauth2%s --headless  # remote/headless machine (paste a code)", hint, hint)
}

// run reads JSON-RPC messages from stdin and bridges them until stdin closes or
// the context is cancelled (e.g. SIGINT/SIGTERM). Requests are processed in
// order but off the read loop, so notifications are never head-of-line blocked;
// a best-effort server->client stream runs concurrently.
func (b *mcpBridge) run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var listeners sync.WaitGroup
	listeners.Add(1)
	go func() {
		defer listeners.Done()
		b.listen(ctx)
	}()

	// Read stdin in a goroutine so the main loop can also react to context
	// cancellation (a bufio read on stdin is not interruptible by ctx).
	lines := make(chan []byte)
	var readErr error
	go func() {
		defer close(lines)
		reader := bufio.NewReader(b.in)
		for {
			raw, oversized, err := readLineCapped(reader, maxMCPMessageBytes)
			if oversized {
				b.logf("dropping oversized message (>%d bytes)", maxMCPMessageBytes)
			} else if msg := bytes.TrimSpace(raw); len(msg) > 0 {
				cp := make([]byte, len(msg))
				copy(cp, msg)
				select {
				case lines <- cp:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					readErr = err
				}
				return
			}
		}
	}()

	// inflight tracks every dispatched message goroutine so they are drained
	// before the session is torn down.
	//
	// Requests are processed serially to preserve order (notably the initialize
	// handshake, which must capture the session id before later requests), but
	// without blocking the read loop: each request waits for the previous one to
	// finish via a chained channel. Notifications carry no id, need no reply, and
	// must not be head-of-line blocked behind an in-flight streaming response
	// (e.g. notifications/cancelled during a long tools/call), so they are
	// dispatched immediately. Shared state (token store, session id, stdout) is
	// mutex-protected, so this stays race-free.
	var inflight sync.WaitGroup
	prevDone := make(chan struct{})
	close(prevDone) // the first request has no predecessor to wait for

	for {
		select {
		case <-ctx.Done():
			// Signal/shutdown: in-flight goroutines observe ctx and unwind, then
			// stop the listener and best-effort end the session.
			inflight.Wait()
			listeners.Wait()
			b.deleteSession()
			return nil
		case msg, ok := <-lines:
			if !ok {
				// stdin closed by the client: let already-queued requests finish
				// (graceful EOF), then stop the listener and end the session.
				inflight.Wait()
				cancel()
				listeners.Wait()
				b.deleteSession()
				return readErr
			}
			if isNotification(msg) {
				inflight.Add(1)
				go func(m []byte) {
					defer inflight.Done()
					b.forwardPost(ctx, m)
				}(msg)
				continue
			}
			// Request: chain after the previous request so ordering is preserved
			// without blocking this loop.
			done := make(chan struct{})
			inflight.Add(1)
			go func(m []byte, wait <-chan struct{}, signal chan<- struct{}) {
				defer inflight.Done()
				defer close(signal)
				select {
				case <-wait:
				case <-ctx.Done():
					return
				}
				b.forwardPost(ctx, m)
			}(msg, prevDone, done)
			prevDone = done
		}
	}
}

// forwardPost POSTs one client message and forwards the server's reply. If the
// message is a request (carries an id) and the bridge cannot deliver a reply --
// transport failure, a failed refresh/retry after a 401, or a response whose
// body is empty/non-JSON -- it synthesizes a JSON-RPC error response for that id
// so a strict client never hangs. Notifications (no id) need no reply.
func (b *mcpBridge) forwardPost(ctx context.Context, msg []byte) {
	id := requestID(msg)

	resp := b.postWithRetry(ctx, msg)
	if resp == nil {
		if ctx.Err() == nil {
			b.writeErrorResponse(id, rpcErrTransport, "xurl mcp: could not reach the MCP server (transport error)")
		}
		return
	}

	if resp.StatusCode == http.StatusUnauthorized {
		b.logf("server returned 401 Unauthorized; forcing a token refresh and retrying once")
		drainClose(resp)
		if _, err := b.forceRefreshToken(); err != nil {
			b.logf("token refresh after 401 failed: %v", err)
			b.writeErrorResponse(id, rpcErrAuth, "xurl mcp: token refresh after 401 failed")
			return
		}
		resp = b.postWithRetry(ctx, msg)
		if resp == nil {
			if ctx.Err() == nil {
				b.writeErrorResponse(id, rpcErrTransport, "xurl mcp: retry after token refresh failed (transport error)")
			}
			return
		}
	}

	defer drainClose(resp)
	b.captureSession(resp)
	if !b.forwardResponse(resp) && ctx.Err() == nil {
		b.writeErrorResponse(id, rpcErrUpstream, fmt.Sprintf("xurl mcp: no usable reply from MCP server (HTTP %s)", resp.Status))
	}
}

// postWithRetry sends one POST, retrying once on a transient transport error so
// a single blip never tears the bridge down. Returns nil if both attempts fail.
func (b *mcpBridge) postWithRetry(ctx context.Context, msg []byte) *http.Response {
	resp, err := b.post(ctx, msg)
	if err == nil {
		return resp
	}
	if ctx.Err() != nil {
		return nil
	}
	b.logf("request error (retrying): %v", err)
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(500 * time.Millisecond):
	}
	resp, err = b.post(ctx, msg)
	if err != nil {
		b.logf("request failed: %v", err)
		return nil
	}
	return resp
}

func (b *mcpBridge) post(ctx context.Context, body []byte) (*http.Response, error) {
	token, err := b.accessToken()
	if err != nil {
		return nil, fmt.Errorf("token error: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "xurl/"+version.Version)
	if sid := b.getSession(); sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	return b.httpClient.Do(req)
}

// forwardResponse writes the server's reply to stdout as newline-delimited JSON,
// handling JSON, SSE, 202 (no body) and error responses. It returns true if a
// reply was delivered (or none is expected, e.g. 202), and false if the
// response could not be turned into a client reply (empty/non-JSON body, read
// error, or an SSE stream that yielded nothing) so the caller can synthesize a
// JSON-RPC error for the pending request id.
func (b *mcpBridge) forwardResponse(resp *http.Response) bool {
	if resp.StatusCode == http.StatusAccepted {
		// 202 Accepted: the message was a notification/response; no reply body.
		return true
	}

	ct := resp.Header.Get("Content-Type")

	if resp.StatusCode >= 400 {
		// Read up to the message cap (not a small 64 KiB slice) so large but
		// valid JSON-RPC error bodies are forwarded whole rather than truncated
		// into invalid JSON that writeMessage would drop.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxMCPMessageBytes))
		body = bytes.TrimSpace(body)
		b.logf("server error %s: %s", resp.Status, truncateForLog(body))
		// Forward the body only if it is a JSON-RPC response the client can
		// correlate to its request id; a bare gateway/proxy error (valid JSON
		// but no id, or not JSON at all) is reported as failure so the caller
		// synthesizes a correlatable reply instead of letting the client hang.
		if isJSONRPCResponse(body) {
			return b.writeMessage(body)
		}
		return false
	}

	switch {
	case strings.HasPrefix(ct, "text/event-stream"):
		return b.pumpSSE(resp.Body)
	default:
		// Treat everything else as a single JSON message. writeMessage validates
		// it is JSON and skips (with a stderr note) anything that is not, so the
		// stdout channel stays strictly newline-delimited JSON.
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxMCPMessageBytes))
		if err != nil {
			b.logf("read response failed: %v", err)
			return false
		}
		return b.writeMessage(body)
	}
}

// listen opens the optional standalone server->client SSE stream. It starts once
// a session exists (or shortly after, to support stateless servers), resets its
// backoff only after a stream stays open long enough to be considered healthy,
// retries transient failures (incl. 408/429), and stops permanently when the
// server signals the stream is unsupported (a non-retryable 4xx, or a 200 that
// is not an event-stream).
func (b *mcpBridge) listen(ctx context.Context) {
	select {
	case <-b.sessionReady:
	case <-time.After(2 * time.Second):
		// Stateless server that issues no session id: try the stream anyway.
	case <-ctx.Done():
		return
	}

	// A stream must stay open at least this long for its 200 to count as
	// "healthy"; otherwise a server that accepts the GET and immediately closes
	// would reset the backoff every iteration and produce a tight reconnect loop.
	const minHealthyStream = 5 * time.Second

	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		status, eventStream, err := b.openServerStream(ctx)
		elapsed := time.Since(start)
		if err != nil && ctx.Err() == nil {
			b.logf("server stream error: %v", err)
		}
		// A 200 that is not an event-stream means the server does not offer the
		// standalone server->client channel: stop probing (same as a 4xx).
		if status == http.StatusOK && !eventStream {
			b.logf("server->client stream unsupported (HTTP 200, not an event-stream); not retrying")
			return
		}
		if status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests {
			// Unsupported standalone stream (e.g. 404/405): stop probing.
			return
		}
		// Only reset the backoff when a stream actually stayed open for a while.
		if eventStream && elapsed >= minHealthyStream {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// openServerStream issues the GET for the standalone server->client stream. It
// returns the HTTP status, whether the response was actually an event-stream
// that was pumped, and any transport error.
func (b *mcpBridge) openServerStream(ctx context.Context) (status int, eventStream bool, err error) {
	token, err := b.accessToken()
	if err != nil {
		return 0, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.url, nil)
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "xurl/"+version.Version)
	if sid := b.getSession(); sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer drainClose(resp)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, false, nil
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		b.logf("server->client stream open")
		b.pumpSSE(resp.Body)
		return resp.StatusCode, true, nil
	}
	return resp.StatusCode, false, nil
}

// pumpSSE parses a text/event-stream and forwards each event's JSON data payload
// to stdout as one line. Multi-line data fields are concatenated per the SSE
// spec; writeMessage then validates and compacts each event.
func (b *mcpBridge) pumpSSE(r io.Reader) bool {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMCPMessageBytes)

	wrote := false
	var data strings.Builder
	flush := func() {
		if data.Len() == 0 {
			return
		}
		payload := data.String()
		data.Reset()
		if b.writeMessage([]byte(payload)) {
			wrote = true
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			flush() // blank line terminates an event
		case strings.HasPrefix(line, ":"):
			// SSE comment; ignore.
		case strings.HasPrefix(line, "data:"):
			chunk := strings.TrimPrefix(line, "data:")
			chunk = strings.TrimPrefix(chunk, " ")
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(chunk)
		case line == "data":
			// A field name with no value is an empty data line.
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
		default:
			// Other SSE fields (event:, id:, retry:) aren't needed here.
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		b.logf("sse read error: %v", err)
	}
	return wrote
}

func (b *mcpBridge) captureSession(resp *http.Response) {
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		return
	}
	b.sessMu.Lock()
	changed := b.sessionID != sid
	b.sessionID = sid
	b.sessMu.Unlock()
	if changed {
		b.logf("session id: %s", sid)
	}
	b.sessionOnce.Do(func() { close(b.sessionReady) })
}

func (b *mcpBridge) getSession() string {
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	return b.sessionID
}

// deleteSession best-effort terminates the MCP session on shutdown (the spec
// says clients SHOULD). It uses a fresh short-lived context because the bridge
// context is already cancelled by the time this runs.
func (b *mcpBridge) deleteSession() {
	sid := b.getSession()
	if sid == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	token, err := b.accessToken()
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, b.url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "xurl/"+version.Version)
	req.Header.Set("Mcp-Session-Id", sid)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.logf("session delete failed: %v", err)
		return
	}
	drainClose(resp)
	b.logf("session %s deleted", sid)
}

// writeMessage writes a single newline-terminated JSON message to stdout. It is
// the ONLY path to stdout, and it enforces the transport invariant: every line
// must be exactly one compact, valid JSON value. Non-JSON payloads (e.g. SSE
// keep-alives) are dropped with a stderr note rather than corrupting the channel.
// writeMessage returns true if payload was a usable (valid, compactable) JSON
// value -- i.e. a real reply we could forward -- and false if it was empty or
// not JSON (and therefore dropped). The boolean reflects whether the server gave
// us something to forward, not whether the stdout write itself succeeded: a
// broken stdout cannot be fixed by synthesizing another message.
func (b *mcpBridge) writeMessage(payload []byte) bool {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return false
	}
	if !json.Valid(payload) {
		b.logf("dropping non-JSON server message (%d bytes)", len(payload))
		return false
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, payload); err != nil {
		b.logf("failed to compact server message: %v", err)
		return false
	}
	buf.WriteByte('\n')

	b.outMu.Lock()
	defer b.outMu.Unlock()
	if _, err := b.out.Write(buf.Bytes()); err != nil {
		b.logf("stdout write error: %v", err)
	}
	return true
}

// writeErrorResponse synthesizes a JSON-RPC error response for a client request
// the bridge could not satisfy, so a strict client does not hang waiting on the
// id. A nil/empty/null id means the source was not a correlatable request (a
// notification or a client->server response), in which case nothing is written.
func (b *mcpBridge) writeErrorResponse(id json.RawMessage, code int, message string) {
	if !hasConcreteID(id) {
		return
	}
	payload, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   rpcError        `json:"error"`
	}{
		JSONRPC: "2.0",
		ID:      id,
		Error:   rpcError{Code: code, Message: message},
	})
	if err != nil {
		b.logf("failed to build synthetic error response: %v", err)
		return
	}
	b.writeMessage(payload)
}

// requestID returns the JSON-RPC id of a single *request* -- an object carrying
// BOTH a method and an id -- preserving the raw bytes so a synthesized reply
// echoes the exact id (number or string). It returns nil for anything that must
// never receive a synthesized reply: notifications (no id), client->server
// responses (an id but no method), JSON-RPC batches (a top-level array), and
// unparseable input. Note: batched requests therefore fall outside the no-hang
// guarantee; they are rare over this transport and intentionally left unhandled.
func requestID(msg []byte) json.RawMessage {
	var probe struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(msg, &probe); err != nil {
		return nil
	}
	if probe.Method == "" || !hasConcreteID(probe.ID) {
		return nil
	}
	return probe.ID
}

// hasConcreteID reports whether a raw JSON-RPC id is present and not null -- i.e.
// a value the client can correlate a reply to.
func hasConcreteID(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && !bytes.Equal(t, []byte("null"))
}

// isJSONRPCResponse reports whether body is a JSON-RPC response the client can
// correlate to its request: valid JSON, an object with a concrete (non-null) id
// and a result or error member. A bare gateway/proxy error body (valid JSON but
// no id, e.g. {"status":429,...}) is not one, so the caller can synthesize a
// correlatable reply instead of forwarding something the client can't match.
func isJSONRPCResponse(body []byte) bool {
	if !json.Valid(body) {
		return false
	}
	var probe struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &probe) != nil {
		return false
	}
	return hasConcreteID(probe.ID) && (len(probe.Result) > 0 || len(probe.Error) > 0)
}

// isNotification reports whether msg is a JSON-RPC notification: a single object
// carrying a method but no id. Such messages expect no reply and can be sent
// concurrently with an in-flight streaming response.
func isNotification(msg []byte) bool {
	var probe struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(msg, &probe); err != nil {
		return false
	}
	return probe.Method != "" && len(bytes.TrimSpace(probe.ID)) == 0
}

// readLineCapped reads a single '\n'-terminated line from r while never
// buffering more than max bytes: if a line exceeds the cap, the surplus through
// the next newline is read and discarded so memory stays bounded, and
// oversized is true. The returned bytes never include the trailing newline.
func readLineCapped(r *bufio.Reader, max int) (line []byte, oversized bool, err error) {
	for {
		c, e := r.ReadByte()
		if e != nil {
			return line, oversized, e
		}
		if c == '\n' {
			return line, oversized, nil
		}
		if len(line) < max {
			line = append(line, c)
		} else {
			oversized = true
		}
	}
}

// truncateForLog renders bytes for a stderr diagnostic without dumping a large
// body (the error-body read cap is the full message size).
func truncateForLog(p []byte) string {
	const max = 2048
	if len(p) <= max {
		return string(p)
	}
	return string(p[:max]) + "...(truncated)"
}

// logf writes a diagnostic line to stderr; stdout is reserved for JSON-RPC.
func (b *mcpBridge) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[xurl mcp] "+format+"\n", args...)
}

func drainClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// CreateMCPCommand creates the `mcp` command: a stdio<->Streamable-HTTP MCP bridge
// that authenticates with the active app's OAuth2 token.
func CreateMCPCommand(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp [URL]",
		Short: "Bridge a stdio MCP client to a remote (X API) MCP server",
		Long: `Bridge a stdio MCP client to a remote Streamable HTTP MCP server.

xurl reads newline-delimited JSON-RPC from stdin, forwards each message to the
MCP endpoint over HTTP with an 'Authorization: Bearer <token>' header, and
writes the server's responses to stdout as newline-delimited JSON. Both single
JSON responses and text/event-stream (SSE) responses are supported, and the MCP
session id is maintained across requests.

The access token is resolved exactly like 'xurl token': an existing token is
refreshed automatically as it expires (including a forced refresh on a 401).
Authenticate once before starting the bridge with 'xurl auth oauth2 [--app NAME]'
(add --headless on a remote/headless machine). The bridge never opens a browser
itself; if no token exists it exits with that instruction. All diagnostics go to
stderr so stdout stays a clean JSON-RPC channel.

If URL is omitted it defaults to ` + defaultMCPURL + `.

Example MCP client config:
  {
    "mcpServers": {
      "xapi": {
        "command": "npx",
        "args": ["-y", "@xdevplatform/xurl", "mcp", "https://api.x.com/mcp"],
        "env": { "CLIENT_ID": "...", "CLIENT_SECRET": "..." }
      }
    }
  }`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			url := defaultMCPURL
			if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
				url = strings.TrimSpace(args[0])
			}
			username, _ := cmd.Flags().GetString("username")

			bridge := newMCPBridge(url, a, username)

			if err := bridge.bootstrap(); err != nil {
				fprintError(os.Stderr, "Error: %v", err)
				os.Exit(1)
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			bridge.logf("bridging stdio <-> %s", url)
			if err := bridge.run(ctx); err != nil {
				fprintError(os.Stderr, "mcp bridge error: %v", err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringP("username", "u", "", "OAuth2 username to act as")
	return cmd
}
