package api

import (
	"encoding/json"
	"io"
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
// Pure-function unit tests
// ---------------------------------------------------------------

func TestCompareChatKeyVersions(t *testing.T) {
	assert.Equal(t, 0, CompareChatKeyVersions("1700", "1700"))
	assert.Equal(t, -1, CompareChatKeyVersions("999", "1700"))
	assert.Equal(t, 1, CompareChatKeyVersions("1700", "999"))
	assert.Equal(t, 1, CompareChatKeyVersions("1701", "1700"))
}

func TestChatConversationIDForms(t *testing.T) {
	assert.Equal(t, "1-2", ChatConversationPathID("1:2"))
	assert.Equal(t, "1-2", ChatConversationPathID("1-2"))
	assert.Equal(t, "g123", ChatConversationPathID("g123"))
	assert.Equal(t, "1:2", ChatConversationEventID("1-2"))
	assert.Equal(t, "1:2", ChatConversationEventID("1:2"))
}

// ---------------------------------------------------------------
// Integration tests using httptest
// ---------------------------------------------------------------

func setupChatServer(t *testing.T, requests *[]*http.Request, bodies *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*requests = append(*requests, r)
		*bodies = append(*bodies, string(body))
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/public_keys") && r.Method == "GET":
			w.Write([]byte(`{"data":[{"public_key_version":"1700","public_key":"idpk","signing_public_key":"sigpk","identity_public_key_signature":"binding","juicebox_config":{"realms":[]}}]}`))
		case strings.HasSuffix(r.URL.Path, "/events"):
			w.Write([]byte(`{"data":[{"id":"s1","conversation_id":"1:2","sender_id":"7","encoded_event":"AAAA","is_trusted":true}],"meta":{"next_token":"tok2","conversation_key_events":["KEYEV"]}}`))
		case strings.HasSuffix(r.URL.Path, "/keys"):
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"data":{"conversation_id":"1-2"}}`))
		case strings.HasSuffix(r.URL.Path, "/messages"):
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"data":{"encoded_message_event":"BBBB"}}`))
		case r.URL.Path == "/2/chat/conversations":
			w.Write([]byte(`{"data":[{"id":"1:2","type":"direct"}],"meta":{"result_count":1}}`))
		case strings.HasSuffix(r.URL.Path, "/read"):
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"data":{"success":true}}`))
		case strings.HasSuffix(r.URL.Path, "/typing"):
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"data":{"success":true}}`))
		case strings.HasSuffix(r.URL.Path, "/members"):
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"data":{"conversation_key_change_sequence_id":"99"}}`))
		case r.URL.Path == "/2/chat/media/upload/initialize":
			w.Write([]byte(`{"data":{"session_id":"sess1","media_hash_key":"mhk1","conversation_id":"1:2"}}`))
		case strings.HasSuffix(r.URL.Path, "/append"):
			w.Write([]byte(`{"data":{"expires_at":123}}`))
		case strings.HasSuffix(r.URL.Path, "/finalize"):
			w.Write([]byte(`{"data":{"success":true}}`))
		case strings.HasPrefix(r.URL.Path, "/2/chat/media/"):
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte("RAWCIPHERTEXT"))
		default:
			w.Write([]byte(`{"data":{}}`))
		}
	}))
}

func chatTestClient(t *testing.T, server *httptest.Server) *ApiClient {
	t.Helper()
	authMock, tempDir := createMockAuth(t)
	t.Cleanup(func() { os.RemoveAll(tempDir) })
	cfg := &config.Config{APIBaseURL: server.URL}
	return NewApiClient(cfg, authMock)
}

func TestGetChatPublicKeys(t *testing.T) {
	var requests []*http.Request
	var bodies []string
	server := setupChatServer(t, &requests, &bodies)
	defer server.Close()
	client := chatTestClient(t, server)

	keys, err := GetChatPublicKeys(client, "7", RequestOptions{})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "1700", keys[0].Version)
	assert.Equal(t, "idpk", keys[0].PublicKey)
	assert.Equal(t, "sigpk", keys[0].SigningPublicKey)
	assert.NotEmpty(t, keys[0].JuiceboxConfig)

	require.Len(t, requests, 1)
	assert.Equal(t, "/2/users/7/public_keys", requests[0].URL.Path)
	// Every public_key field is always included; the route takes no
	// public_key.fields parameter.
	assert.Empty(t, requests[0].URL.RawQuery)
}

func TestGetChatEvents(t *testing.T) {
	var requests []*http.Request
	var bodies []string
	server := setupChatServer(t, &requests, &bodies)
	defer server.Close()
	client := chatTestClient(t, server)

	// Colon-form input converts to the hyphen form in the URL path.
	page, err := GetChatEvents(client, "1:2", 50, "tok1", RequestOptions{})
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	assert.Equal(t, "AAAA", page.Events[0].EncodedEvent)
	assert.Equal(t, "7", page.Events[0].SenderID)
	assert.Equal(t, "tok2", page.NextToken)
	assert.Equal(t, []string{"KEYEV"}, page.KeyEvents)

	require.Len(t, requests, 1)
	assert.Equal(t, "/2/chat/conversations/1-2/events", requests[0].URL.Path)
	assert.Equal(t, "50", requests[0].URL.Query().Get("max_results"))
	assert.Equal(t, "tok1", requests[0].URL.Query().Get("pagination_token"))
}

func TestAddChatConversationKeys(t *testing.T) {
	var requests []*http.Request
	var bodies []string
	server := setupChatServer(t, &requests, &bodies)
	defer server.Close()
	client := chatTestClient(t, server)

	body := map[string]any{"conversation_key_version": "v1"}
	_, err := AddChatConversationKeys(client, "1:2", body, RequestOptions{})
	require.NoError(t, err)

	require.Len(t, requests, 1)
	assert.Equal(t, "POST", requests[0].Method)
	assert.Equal(t, "/2/chat/conversations/1-2/keys", requests[0].URL.Path)
	assert.Contains(t, bodies[0], "conversation_key_version")
}

func TestSendChatMessage(t *testing.T) {
	var requests []*http.Request
	var bodies []string
	server := setupChatServer(t, &requests, &bodies)
	defer server.Close()
	client := chatTestClient(t, server)

	_, err := SendChatMessage(client, "1:2", ChatSendBody{
		MessageID:                    "m1",
		EncodedMessageCreateEvent:    "enc",
		EncodedMessageEventSignature: "sig",
	}, RequestOptions{})
	require.NoError(t, err)

	require.Len(t, requests, 1)
	assert.Equal(t, "/2/chat/conversations/1-2/messages", requests[0].URL.Path)

	var sent map[string]any
	require.NoError(t, json.Unmarshal([]byte(bodies[0]), &sent))
	assert.Equal(t, "m1", sent["message_id"])
	assert.Equal(t, "enc", sent["encoded_message_create_event"])
	assert.Equal(t, "sig", sent["encoded_message_event_signature"])
	// Empty optional token is omitted entirely.
	_, hasToken := sent["conversation_token"]
	assert.False(t, hasToken)
}

func TestMarkChatReadAndTyping(t *testing.T) {
	var requests []*http.Request
	var bodies []string
	server := setupChatServer(t, &requests, &bodies)
	defer server.Close()
	client := chatTestClient(t, server)

	_, err := MarkChatRead(client, "1:2", "77", RequestOptions{})
	require.NoError(t, err)
	assert.Equal(t, "/2/chat/conversations/1-2/read", requests[0].URL.Path)
	assert.Contains(t, bodies[0], `"seen_until_sequence_id":"77"`)

	_, err = SendChatTyping(client, "1:2", RequestOptions{})
	require.NoError(t, err)
	assert.Equal(t, "/2/chat/conversations/1-2/typing", requests[1].URL.Path)
}

func TestChatMediaRoundTrip(t *testing.T) {
	var requests []*http.Request
	var bodies []string
	server := setupChatServer(t, &requests, &bodies)
	defer server.Close()
	client := chatTestClient(t, server)

	sessionID, mhk, err := InitializeChatMediaUpload(client, "1:2", 10, RequestOptions{})
	require.NoError(t, err)
	assert.Equal(t, "sess1", sessionID)
	assert.Equal(t, "mhk1", mhk)
	assert.Equal(t, "/2/chat/media/upload/initialize", requests[0].URL.Path)

	err = UploadChatMedia(client, sessionID, "1:2", mhk, []byte("ciphertextbytes"), RequestOptions{})
	require.NoError(t, err)
	// One append (small payload) + one finalize.
	assert.Equal(t, "/2/chat/media/upload/sess1/append", requests[1].URL.Path)
	assert.Equal(t, "/2/chat/media/upload/sess1/finalize", requests[2].URL.Path)

	data, err := DownloadChatMedia(client, "1:2", mhk, RequestOptions{})
	require.NoError(t, err)
	assert.Equal(t, "RAWCIPHERTEXT", string(data))
	assert.Equal(t, "/2/chat/media/1-2/mhk1", requests[3].URL.Path)
}

func TestAddChatGroupMembers(t *testing.T) {
	var requests []*http.Request
	var bodies []string
	server := setupChatServer(t, &requests, &bodies)
	defer server.Close()
	client := chatTestClient(t, server)

	body := map[string]any{"user_ids": []string{"7"}, "conversation_key_version": "v2"}
	_, err := AddChatGroupMembers(client, "g123", body, RequestOptions{})
	require.NoError(t, err)
	assert.Equal(t, "/2/chat/conversations/g123/members", requests[0].URL.Path)
	assert.Contains(t, bodies[0], `"user_ids"`)
}

func TestGetChatConversationMeta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"participant_ids":["1","2"],"member_ids":["1","2","3"],"admin_ids":["1"],"group_name":"enc"}}`))
	}))
	defer server.Close()
	client := chatTestClient(t, server)

	meta, _, err := GetChatConversation(client, "g123", RequestOptions{})
	require.NoError(t, err)
	assert.Equal(t, "enc", meta.GroupName)
	assert.ElementsMatch(t, []string{"1", "2", "3"}, meta.AllUserIDs())
}

func TestGetChatConversations(t *testing.T) {
	var requests []*http.Request
	var bodies []string
	server := setupChatServer(t, &requests, &bodies)
	defer server.Close()
	client := chatTestClient(t, server)

	resp, err := GetChatConversations(client, 20, "", RequestOptions{})
	require.NoError(t, err)
	assert.Contains(t, string(resp), `"1:2"`)

	require.Len(t, requests, 1)
	assert.Equal(t, "/2/chat/conversations", requests[0].URL.Path)
	assert.Equal(t, "20", requests[0].URL.Query().Get("max_results"))

	// max_results clamps into the API's 1-100 window.
	_, err = GetChatConversations(client, 1000, "", RequestOptions{})
	require.NoError(t, err)
	assert.Equal(t, "100", requests[1].URL.Query().Get("max_results"))
}
