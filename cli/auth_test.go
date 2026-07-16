package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xdevplatform/xurl/store"
)

func TestOAuth2NoAppCredentialWarning(t *testing.T) {
	t.Run("no warning when --app is set", func(t *testing.T) {
		ts := &store.TokenStore{
			Apps: map[string]*store.App{
				"default": {ClientID: "", OAuth2Tokens: map[string]store.Token{}},
				"my-app":  {ClientID: "cid", OAuth2Tokens: map[string]store.Token{}},
			},
			DefaultApp: "default",
		}
		warn, _, _ := oauth2NoAppCredentialWarning(ts, "my-app")
		assert.False(t, warn)
	})

	t.Run("no warning when default app has credentials", func(t *testing.T) {
		// Regression: GetApp("") always returned nil, so this case spuriously warned
		// even when the real default (here app-2) had client credentials.
		ts := &store.TokenStore{
			Apps: map[string]*store.App{
				"app-2":   {ClientID: "WEpLT2ZF", OAuth2Tokens: map[string]store.Token{}},
				"default": {ClientID: "VUttdG9P", OAuth2Tokens: map[string]store.Token{}},
			},
			DefaultApp: "app-2",
		}
		warn, targetName, credentialed := oauth2NoAppCredentialWarning(ts, "")
		assert.False(t, warn)
		assert.Equal(t, "app-2", targetName)
		assert.Nil(t, credentialed)
	})

	t.Run("warns when default app lacks credentials but others have them", func(t *testing.T) {
		ts := &store.TokenStore{
			Apps: map[string]*store.App{
				"default": {ClientID: "", OAuth2Tokens: map[string]store.Token{}},
				"my-app":  {ClientID: "abc12345xyz", OAuth2Tokens: map[string]store.Token{}},
			},
			DefaultApp: "default",
		}
		warn, targetName, credentialed := oauth2NoAppCredentialWarning(ts, "")
		require.True(t, warn)
		assert.Equal(t, "default", targetName)
		assert.Equal(t, []string{"my-app"}, credentialed)
	})

	t.Run("uses real default app name in warning target", func(t *testing.T) {
		ts := &store.TokenStore{
			Apps: map[string]*store.App{
				"empty-app": {ClientID: "", OAuth2Tokens: map[string]store.Token{}},
				"prod":      {ClientID: "prod-id-1", OAuth2Tokens: map[string]store.Token{}},
			},
			DefaultApp: "empty-app",
		}
		warn, targetName, credentialed := oauth2NoAppCredentialWarning(ts, "")
		require.True(t, warn)
		assert.Equal(t, "empty-app", targetName)
		assert.Equal(t, []string{"prod"}, credentialed)
	})

	t.Run("no warning when no app has credentials", func(t *testing.T) {
		ts := &store.TokenStore{
			Apps: map[string]*store.App{
				"default": {ClientID: "", OAuth2Tokens: map[string]store.Token{}},
			},
			DefaultApp: "default",
		}
		warn, _, _ := oauth2NoAppCredentialWarning(ts, "")
		assert.False(t, warn)
	})

	t.Run("no warning when store is empty", func(t *testing.T) {
		ts := &store.TokenStore{Apps: map[string]*store.App{}}
		warn, targetName, _ := oauth2NoAppCredentialWarning(ts, "")
		assert.False(t, warn)
		assert.Equal(t, "default", targetName)
	})
}
