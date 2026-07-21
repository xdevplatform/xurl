//go:build cgo && ((darwin && (amd64 || arm64)) || (linux && amd64))

package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xdevplatform/chat-xdk/go/chatxdk"
	"github.com/xdevplatform/xurl/api"
)

// stubChatClient serves canned responses for the endpoints resolveConversation
// touches (user lookup by username).
type stubChatClient struct {
	api.Client
	lookupID string
}

func (c *stubChatClient) SendRequest(opts api.RequestOptions) (json.RawMessage, error) {
	if strings.HasPrefix(opts.Endpoint, "/2/users/by/username/") {
		return json.RawMessage(fmt.Sprintf(`{"data":{"id":%q,"username":"stub"}}`, c.lookupID)), nil
	}
	return json.RawMessage(`{"data":{}}`), nil
}

func TestResolveConversationForms(t *testing.T) {
	s := &chatSession{userID: "42", client: &stubChatClient{lookupID: "100"}}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"group id", "g123", "g123"},
		{"hyphen 1:1 id", "100-200", "100-200"},
		{"colon 1:1 id converts to hyphen", "100:200", "100-200"},
		{"peer id greater than mine", "100", "42-100"},
		{"peer id smaller than mine", "7", "7-42"},
		{"same length ids order lexically", "41", "41-42"},
		{"username starting with g resolves as user", "gandalf", "42-100"},
		{"@username starting with g resolves as user", "@gandalf", "42-100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.resolveConversation(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsAllDigits(t *testing.T) {
	assert.True(t, isAllDigits("123"))
	assert.False(t, isAllDigits(""))
	assert.False(t, isAllDigits("@bob"))
	assert.False(t, isAllDigits("12a"))
}

func TestMatchesRegisteredKeyEncodings(t *testing.T) {
	// The registered-key match is delegated to chat-xdk's
	// MatchesRegisteredKey, which accepts both wire encodings: the SPKI/DER
	// form the API returns and the raw SEC1 point from GetPublicKeys.
	chat := chatxdk.New()
	defer chat.Close()
	payload, err := chat.GenerateKeypairs()
	require.NoError(t, err)
	pub, err := chat.GetPublicKeys()
	require.NoError(t, err)

	assert.NotEqual(t, pub.Identity, payload.PublicKey.PublicKey, "sanity: encodings differ")
	ok, err := chat.MatchesRegisteredKey(payload.PublicKey.PublicKey)
	require.NoError(t, err)
	assert.True(t, ok, "SPKI form matches")
	ok, err = chat.MatchesRegisteredKey(pub.Identity)
	require.NoError(t, err)
	assert.True(t, ok, "raw SEC1 form matches")

	// A different key must not match.
	other := chatxdk.New()
	defer other.Close()
	otherPayload, err := other.GenerateKeypairs()
	require.NoError(t, err)
	ok, err = chat.MatchesRegisteredKey(otherPayload.PublicKey.PublicKey)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestChatCommandStructure(t *testing.T) {
	cmd := CreateChatCommand(nil)
	assert.Equal(t, "chat", cmd.Name())
	assert.True(t, chatSupported)

	names := map[string]bool{}
	for _, c := range cmd.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"keys", "conversations", "read", "send", "listen"} {
		assert.True(t, names[want], "missing subcommand %q", want)
	}

	for _, c := range cmd.Commands() {
		if c.Name() == "keys" {
			sub := map[string]bool{}
			for _, k := range c.Commands() {
				sub[k.Name()] = true
			}
			for _, want := range []string{"status", "restore", "import"} {
				assert.True(t, sub[want], "missing keys subcommand %q", want)
			}
		}
	}
}

func mockPreparedChange() *chatxdk.PreparedConversationChange {
	return &chatxdk.PreparedConversationChange{
		ConversationID:         "1:2",
		ConversationKeyVersion: "v9",
		ParticipantKeys: []chatxdk.EncryptedKeyForRecipient{
			{UserID: "7", EncryptedKey: "enc-key", PublicKeyVersion: "1700"},
		},
		ActionSignatures: []chatxdk.ActionSignature{
			{MessageID: "m1", EncodedMessageEventDetail: "detail", Signature: "sig", SignatureVersion: "7", PublicKeyVersion: "1700"},
		},
	}
}

func TestPreparedChangeToRequestShape(t *testing.T) {
	// Exercised indirectly via chat-xdk types; here we validate the JSON
	// field names the API expects.
	body := preparedChangeToRequest(mockPreparedChange(), "signing-pub")

	assert.Equal(t, "v9", body["conversation_key_version"])
	pks := body["conversation_participant_keys"].([]map[string]any)
	require.Len(t, pks, 1)
	assert.Equal(t, "7", pks[0]["user_id"])
	assert.Equal(t, "enc-key", pks[0]["encrypted_conversation_key"])
	assert.Equal(t, "1700", pks[0]["public_key_version"])

	sigs := body["action_signatures"].([]map[string]any)
	require.Len(t, sigs, 1)
	assert.Equal(t, "m1", sigs[0]["message_id"])
	mes := sigs[0]["message_event_signature"].(map[string]any)
	assert.Equal(t, "signing-pub", mes["signing_public_key"])
	// signature_payload is omitted when empty.
	_, has := sigs[0]["signature_payload"]
	assert.False(t, has)
}

func TestChatConversationPathHelpers(t *testing.T) {
	assert.Equal(t, "1-2", api.ChatConversationPathID("1:2"))
	assert.Equal(t, "1:2", api.ChatConversationEventID("1-2"))
}
