package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ------------------------------------------------
// XChat (encrypted chat) endpoint executors.
//
// These are thin, crypto-free wrappers over the /2/chat and
// /2/users/:id/public_keys routes. All encryption and decryption
// happens in the cli layer via the chat-xdk binding; this file only
// moves opaque base64 payloads.
// ------------------------------------------------

// ChatConversationPathID converts a conversation id to the form the API URL
// paths expect (hyphen-separated), from the colon-separated form found
// inside decrypted events and signatures.
func ChatConversationPathID(id string) string {
	return strings.ReplaceAll(id, ":", "-")
}

// ChatConversationEventID converts a conversation id to the canonical
// colon-separated form embedded in events and signatures.
func ChatConversationEventID(id string) string {
	return strings.ReplaceAll(id, "-", ":")
}

// ChatEventItem is one element of the conversation events response. ID is
// the event's sequence id (the resource exposes sequence_id as id).
type ChatEventItem struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	SenderID       string `json:"sender_id"`
	EncodedEvent   string `json:"encoded_event"`
	IsTrusted      bool   `json:"is_trusted"`
}

// ChatPublicKey is one registered public key row for a user. Every field of
// the public_key resource is always included by the API (no public_key.fields
// parameter exists for opting in); the version field is exposed as
// public_key_version.
type ChatPublicKey struct {
	// UserID tags the key's owner; only the batch endpoint
	// (GET /2/users/public_keys) sets it.
	UserID                     string          `json:"user_id,omitempty"`
	Version                    string          `json:"public_key_version"`
	PublicKey                  string          `json:"public_key"`
	SigningPublicKey           string          `json:"signing_public_key"`
	IdentityPublicKeySignature string          `json:"identity_public_key_signature"`
	JuiceboxConfig             json.RawMessage `json:"juicebox_config,omitempty"`
}

// CompareChatKeyVersions numerically compares two key version strings
// (non-padded positive integers, typically millisecond timestamps): -1 when
// a < b, 0 when equal, 1 when a > b.
func CompareChatKeyVersions(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// GetChatPublicKeys fetches a user's registered public keys (including the
// juicebox_config, which the API always returns).
func GetChatPublicKeys(client Client, userID string, opts RequestOptions) ([]ChatPublicKey, error) {
	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/public_keys", url.PathEscape(userID))
	opts.Data = ""

	resp, err := client.SendRequest(opts)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []ChatPublicKey `json:"data"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, fmt.Errorf("failed to parse public keys response: %w", err)
	}
	return out.Data, nil
}

// GetChatUsersPublicKeys fetches registered public keys for the given users;
// each returned row carries its owner's user_id. The endpoint accepts at
// most 100 ids per request, so larger inputs are fetched in batches.
func GetChatUsersPublicKeys(client Client, userIDs []string, opts RequestOptions) ([]ChatPublicKey, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	var keys []ChatPublicKey
	for start := 0; start < len(userIDs); start += 100 {
		batch := userIDs[start:min(start+100, len(userIDs))]
		opts.Method = "GET"
		opts.Endpoint = "/2/users/public_keys?ids=" + url.QueryEscape(strings.Join(batch, ","))
		opts.Data = ""

		resp, err := client.SendRequest(opts)
		if err != nil {
			return nil, err
		}
		var out struct {
			Data []ChatPublicKey `json:"data"`
		}
		if err := json.Unmarshal(resp, &out); err != nil {
			return nil, fmt.Errorf("failed to parse public keys response: %w", err)
		}
		keys = append(keys, out.Data...)
	}
	return keys, nil
}

// GetChatConversations fetches a page of the authenticated user's chat inbox.
func GetChatConversations(client Client, maxResults int, paginationToken string, opts RequestOptions) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("max_results", fmt.Sprintf("%d", clampResults(maxResults, 1, 100)))
	if paginationToken != "" {
		q.Set("pagination_token", paginationToken)
	}
	opts.Method = "GET"
	opts.Endpoint = "/2/chat/conversations?" + q.Encode()
	opts.Data = ""

	return client.SendRequest(opts)
}

// ChatEventsPage is one page of a conversation's events.
type ChatEventsPage struct {
	Events []ChatEventItem
	// KeyEvents are base64-encoded key-change events that apply to messages
	// in this page but are not part of it (meta.conversation_key_events).
	// Feed them to the SDK's batch decrypt alongside the page's events so
	// the conversation keys can be extracted.
	KeyEvents []string
	NextToken string
}

// GetChatEvents fetches a page of raw (encrypted) events for a conversation.
func GetChatEvents(client Client, conversationID string, maxResults int, paginationToken string, opts RequestOptions) (*ChatEventsPage, error) {
	q := url.Values{}
	q.Set("max_results", fmt.Sprintf("%d", clampResults(maxResults, 1, 100)))
	if paginationToken != "" {
		q.Set("pagination_token", paginationToken)
	}
	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/chat/conversations/%s/events?%s", url.PathEscape(ChatConversationPathID(conversationID)), q.Encode())
	opts.Data = ""

	resp, err := client.SendRequest(opts)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []ChatEventItem `json:"data"`
		Meta struct {
			NextToken             string   `json:"next_token"`
			ConversationKeyEvents []string `json:"conversation_key_events"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, fmt.Errorf("failed to parse conversation events response: %w", err)
	}
	return &ChatEventsPage{
		Events:    out.Data,
		KeyEvents: out.Meta.ConversationKeyEvents,
		NextToken: out.Meta.NextToken,
	}, nil
}

// ChatConversationMeta is a conversation's metadata. Group name and avatar
// URL arrive encrypted under the conversation key when the group set them
// from an encrypted client.
type ChatConversationMeta struct {
	ParticipantIDs               []string `json:"participant_ids"`
	MemberIDs                    []string `json:"member_ids"`
	AdminIDs                     []string `json:"admin_ids"`
	GroupName                    string   `json:"group_name"`
	GroupAvatarURL               string   `json:"group_avatar_url"`
	MessageTTLMs                 *int64   `json:"message_ttl_ms"`
	ScreenCaptureBlockingEnabled *bool    `json:"screen_capture_blocking_enabled"`
}

// AllUserIDs returns the deduplicated union of participant, member, and
// admin ids.
func (m *ChatConversationMeta) AllUserIDs() []string {
	seen := map[string]bool{}
	var ids []string
	for _, group := range [][]string{m.ParticipantIDs, m.MemberIDs, m.AdminIDs} {
		for _, id := range group {
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// GetChatConversation fetches one conversation's metadata.
func GetChatConversation(client Client, conversationID string, opts RequestOptions) (*ChatConversationMeta, json.RawMessage, error) {
	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/chat/conversations/%s", url.PathEscape(ChatConversationPathID(conversationID)))
	opts.Data = ""

	resp, err := client.SendRequest(opts)
	if err != nil {
		return nil, nil, err
	}
	var out struct {
		Data ChatConversationMeta `json:"data"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, nil, fmt.Errorf("failed to parse conversation metadata response: %w", err)
	}
	return &out.Data, resp, nil
}

// AddChatConversationKeys posts a prepared conversation-key change
// (initialize or rotate). For a 1:1, conversationID may be the recipient's
// user id; the server derives the canonical conversation id.
func AddChatConversationKeys(client Client, conversationID string, body any, opts RequestOptions) (json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal conversation keys body: %w", err)
	}
	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/chat/conversations/%s/keys", url.PathEscape(ChatConversationPathID(conversationID)))
	opts.Data = string(data)

	return client.SendRequest(opts)
}

// ChatSendBody is the request body for sending an encrypted chat message.
type ChatSendBody struct {
	MessageID                    string `json:"message_id"`
	EncodedMessageCreateEvent    string `json:"encoded_message_create_event"`
	EncodedMessageEventSignature string `json:"encoded_message_event_signature"`
	ConversationToken            string `json:"conversation_token,omitempty"`
}

// SendChatMessage posts an encrypted message produced by chat-xdk.
func SendChatMessage(client Client, conversationID string, body ChatSendBody, opts RequestOptions) (json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal chat message body: %w", err)
	}
	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/chat/conversations/%s/messages", url.PathEscape(ChatConversationPathID(conversationID)))
	opts.Data = string(data)

	return client.SendRequest(opts)
}

// MarkChatRead marks a conversation read up to the given sequence id.
func MarkChatRead(client Client, conversationID, seenUntilSequenceID string, opts RequestOptions) (json.RawMessage, error) {
	data, err := json.Marshal(map[string]string{"seen_until_sequence_id": seenUntilSequenceID})
	if err != nil {
		return nil, err
	}
	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/chat/conversations/%s/read", url.PathEscape(ChatConversationPathID(conversationID)))
	opts.Data = string(data)

	return client.SendRequest(opts)
}

// SendChatTyping sends a typing indicator to a conversation.
func SendChatTyping(client Client, conversationID string, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/chat/conversations/%s/typing", url.PathEscape(ChatConversationPathID(conversationID)))
	opts.Data = ""

	return client.SendRequest(opts)
}

// AddChatGroupMembers adds members to a group conversation. body carries the
// new user_ids plus the rotated key change built from a prepared group
// members change.
func AddChatGroupMembers(client Client, conversationID string, body any, opts RequestOptions) (json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal group members body: %w", err)
	}
	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/chat/conversations/%s/members", url.PathEscape(ChatConversationPathID(conversationID)))
	opts.Data = string(data)

	return client.SendRequest(opts)
}

// ------------------------------------------------
// Encrypted media (opaque ciphertext blobs)
// ------------------------------------------------

// chatMediaUploadChunk is the append segment size for the three-step upload.
const chatMediaUploadChunk = 3 * 1024 * 1024

// InitializeChatMediaUpload starts an encrypted media upload session.
// totalBytes is the size of the ciphertext, not the plaintext. The media
// endpoints take the colon form of the conversation id in bodies.
func InitializeChatMediaUpload(client Client, conversationID string, totalBytes int, opts RequestOptions) (sessionID, mediaHashKey string, err error) {
	data, err := json.Marshal(map[string]any{
		"conversation_id": ChatConversationEventID(conversationID),
		"total_bytes":     totalBytes,
	})
	if err != nil {
		return "", "", err
	}
	opts.Method = "POST"
	opts.Endpoint = "/2/chat/media/upload/initialize"
	opts.Data = string(data)

	resp, err := client.SendRequest(opts)
	if err != nil {
		return "", "", err
	}
	var out struct {
		Data struct {
			SessionID    string `json:"session_id"`
			MediaHashKey string `json:"media_hash_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return "", "", fmt.Errorf("failed to parse media initialize response: %w", err)
	}
	if out.Data.SessionID == "" || out.Data.MediaHashKey == "" {
		return "", "", fmt.Errorf("media upload initialize returned no session (response: %s)", resp)
	}
	return out.Data.SessionID, out.Data.MediaHashKey, nil
}

// UploadChatMedia appends the ciphertext in 3 MB base64 segments and
// finalizes the session.
func UploadChatMedia(client Client, sessionID, conversationID, mediaHashKey string, ciphertext []byte, opts RequestOptions) error {
	conv := ChatConversationEventID(conversationID)
	segment := 0
	for offset := 0; offset < len(ciphertext); offset += chatMediaUploadChunk {
		end := min(offset+chatMediaUploadChunk, len(ciphertext))
		data, err := json.Marshal(map[string]any{
			"conversation_id": conv,
			"media_hash_key":  mediaHashKey,
			"segment_index":   fmt.Sprintf("%d", segment),
			"media":           base64.StdEncoding.EncodeToString(ciphertext[offset:end]),
		})
		if err != nil {
			return err
		}
		o := opts
		o.Method = "POST"
		o.Endpoint = fmt.Sprintf("/2/chat/media/upload/%s/append", url.PathEscape(sessionID))
		o.Data = string(data)
		if _, err := client.SendRequest(o); err != nil {
			return fmt.Errorf("media append (segment %d) failed: %w", segment, err)
		}
		segment++
	}

	data, err := json.Marshal(map[string]any{
		"conversation_id": conv,
		"media_hash_key":  mediaHashKey,
		"num_parts":       fmt.Sprintf("%d", segment),
	})
	if err != nil {
		return err
	}
	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/chat/media/upload/%s/finalize", url.PathEscape(sessionID))
	opts.Data = string(data)
	if _, err := client.SendRequest(opts); err != nil {
		return fmt.Errorf("media finalize failed: %w", err)
	}
	return nil
}

// DownloadChatMedia fetches an encrypted media blob as raw ciphertext bytes.
// The response is binary, so it bypasses the JSON response path.
func DownloadChatMedia(client Client, conversationID, mediaHashKey string, opts RequestOptions) ([]byte, error) {
	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/chat/media/%s/%s", url.PathEscape(ChatConversationPathID(conversationID)), url.PathEscape(mediaHashKey))
	opts.Data = ""

	req, err := client.BuildRequest(opts)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("media download failed (%d): %s", resp.StatusCode, body)
	}
	return body, nil
}

// GetUserByID fetches a user object by id (used to render sender usernames).
func GetUserByID(client Client, userID string, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/users/%s", url.PathEscape(userID))
	opts.Data = ""

	return client.SendRequest(opts)
}
