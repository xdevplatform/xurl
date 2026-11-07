package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/store"
)

func TestWebhookSigningSecret(t *testing.T) {
	legacy := &store.Token{OAuth1: &store.OAuth1Token{ConsumerSecret: "oauth1-consumer-secret"}}
	tests := []struct {
		name       string
		apps       map[string]*store.App
		defaultApp string
		appName    string
		want       string
	}{
		{
			name:       "OAuth2-only app needs no user tokens",
			apps:       map[string]*store.App{"selected": {ClientID: "client-id", ClientSecret: "oauth2-client-secret"}},
			defaultApp: "selected",
			want:       "oauth2-client-secret",
		},
		{
			name:       "OAuth1-only app uses consumer secret without access tokens",
			apps:       map[string]*store.App{"selected": {OAuth1Token: legacy}},
			defaultApp: "selected",
			want:       "oauth1-consumer-secret",
		},
		{
			name:       "OAuth2 takes precedence over OAuth1",
			apps:       map[string]*store.App{"selected": {ClientSecret: "oauth2-client-secret", OAuth1Token: legacy}},
			defaultApp: "selected",
			want:       "oauth2-client-secret",
		},
		{
			name: "explicit app overrides default",
			apps: map[string]*store.App{
				"default":  {ClientSecret: "another-app-secret"},
				"selected": {ClientSecret: "oauth2-client-secret"},
			},
			defaultApp: "default",
			appName:    "selected",
			want:       "oauth2-client-secret",
		},
		{
			name: "explicit OAuth1 app does not use default OAuth2 secret",
			apps: map[string]*store.App{
				"default":  {ClientSecret: "another-app-secret"},
				"selected": {OAuth1Token: legacy},
			},
			defaultApp: "default",
			appName:    "selected",
			want:       "oauth1-consumer-secret",
		},
		{
			name:       "client ID alone is not a signing secret",
			apps:       map[string]*store.App{"selected": {ClientID: "client-id"}},
			defaultApp: "selected",
		},
		{
			name: "bearer and user tokens are not signing secrets",
			apps: map[string]*store.App{"selected": {
				BearerToken: &store.Token{Bearer: "bearer-token"},
				OAuth2Tokens: map[string]store.Token{
					"user": {OAuth2: &store.OAuth2Token{AccessToken: "access-token", RefreshToken: "refresh-token"}},
				},
				OAuth1Token: &store.Token{OAuth1: &store.OAuth1Token{AccessToken: "access-token", TokenSecret: "token-secret"}},
			}},
			defaultApp: "selected",
		},
		{
			name:       "missing OAuth1 fields",
			apps:       map[string]*store.App{"selected": {OAuth1Token: &store.Token{}}},
			defaultApp: "selected",
		},
		{
			name: "secretless app does not borrow default credentials",
			apps: map[string]*store.App{
				"default":  {ClientSecret: "another-app-secret", OAuth1Token: legacy},
				"selected": {},
			},
			defaultApp: "default",
			appName:    "selected",
		},
		{
			name:       "unknown app does not fall back to default",
			apps:       map[string]*store.App{"default": {ClientSecret: "another-app-secret", OAuth1Token: legacy}},
			defaultApp: "default",
			appName:    "unknown",
		},
		{
			name:       "missing default does not pick another app",
			apps:       map[string]*store.App{"other": {ClientSecret: "another-app-secret"}},
			defaultApp: "missing",
		},
		{
			name: "empty store",
			apps: map[string]*store.App{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &auth.Auth{TokenStore: &store.TokenStore{Apps: tt.apps, DefaultApp: tt.defaultApp}}
			if tt.appName != "" {
				a.WithAppName(tt.appName)
			}
			secret, err := webhookSigningSecret(a)
			if tt.want == "" {
				require.Error(t, err)
				assert.Empty(t, secret)
				assert.Contains(t, err.Error(), "OAuth 2.0 client secret")
				assert.Contains(t, err.Error(), "OAuth 1.0a consumer secret")
				assert.NotContains(t, err.Error(), "xurl auth oauth1")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, secret)
		})
	}
}

func TestWebhookSigningSecretUninitializedAuth(t *testing.T) {
	for _, a := range []*auth.Auth{nil, {}} {
		secret, err := webhookSigningSecret(a)
		require.Error(t, err)
		assert.Empty(t, secret)
	}
}

func TestWebhookCRCUsesSelectedSigningSecret(t *testing.T) {
	for _, tt := range []struct {
		name string
		app  *store.App
		want string
	}{
		{
			name: "OAuth2 preferred",
			app: &store.App{
				ClientSecret: "oauth2-client-secret",
				OAuth1Token:  &store.Token{OAuth1: &store.OAuth1Token{ConsumerSecret: "oauth1-consumer-secret"}},
			},
			want: "sha256=EBdkssF58HZ4DsDnQ/U28weK23NCd7d+AfU7JT+eYUo=",
		},
		{
			name: "OAuth1 fallback",
			app:  &store.App{OAuth1Token: &store.Token{OAuth1: &store.OAuth1Token{ConsumerSecret: "oauth1-consumer-secret"}}},
			want: "sha256=qb6/EujLt/sq/1RIIK986dZulgi6Shq9ZyVXMWtGPMg=",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &auth.Auth{TokenStore: &store.TokenStore{Apps: map[string]*store.App{"selected": tt.app}, DefaultApp: "selected"}}
			secret, err := webhookSigningSecret(a)
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/webhook?crc_token=crc-test-token", nil)
			handleWebhookCRC(recorder, request, secret)
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
			var response map[string]string
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, map[string]string{"response_token": tt.want}, response)
		})
	}
}

func TestWebhookCRCRejectsMissingToken(t *testing.T) {
	recorder := httptest.NewRecorder()
	handleWebhookCRC(recorder, httptest.NewRequest(http.MethodGet, "/webhook", nil), "synthetic-secret")
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "response_token")
}
