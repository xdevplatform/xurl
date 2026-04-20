package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xdevplatform/xurl/api"
)

type fakeClient struct {
	sendRequest func(options api.RequestOptions) (json.RawMessage, error)
}

func (f fakeClient) BuildRequest(options api.RequestOptions) (*http.Request, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f fakeClient) BuildMultipartRequest(options api.MultipartOptions) (*http.Request, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f fakeClient) SendRequest(options api.RequestOptions) (json.RawMessage, error) {
	return f.sendRequest(options)
}

func (f fakeClient) StreamRequest(options api.RequestOptions) error {
	return fmt.Errorf("not implemented")
}

func (f fakeClient) SendMultipartRequest(options api.MultipartOptions) (json.RawMessage, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestResolveMyUserIDUsesUsernameFallback(t *testing.T) {
	client := fakeClient{
		sendRequest: func(options api.RequestOptions) (json.RawMessage, error) {
			require.Equal(t, "/2/users/by/username/alice?user.fields=created_at,description,public_metrics,verified,profile_image_url", options.Endpoint)
			return json.RawMessage(`{"data":{"id":"42"}}`), nil
		},
	}

	userID, err := resolveMyUserID(client, api.RequestOptions{Username: "alice"})
	require.NoError(t, err)
	assert.Equal(t, "42", userID)
}

func TestResolveMyUserIDReturnsHelpfulErrorWhenGetMeFails(t *testing.T) {
	client := fakeClient{
		sendRequest: func(options api.RequestOptions) (json.RawMessage, error) {
			require.Equal(t, "/2/users/me?user.fields=created_at,description,public_metrics,verified,profile_image_url", options.Endpoint)
			return nil, fmt.Errorf("boom")
		},
	}

	_, err := resolveMyUserID(client, api.RequestOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "try --username")
}
