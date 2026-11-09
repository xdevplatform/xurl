package cli

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectNgrokAuthtoken(t *testing.T) {
	tests := []struct {
		name        string
		promptToken string
		envToken    string
		wantToken   string
		wantErr     bool
	}{
		{
			name:        "prompt token is preferred",
			promptToken: "prompt-token-123\n",
			envToken:    "env-token-456",
			wantToken:   "prompt-token-123",
		},
		{
			name:        "prompt token with surrounding whitespace",
			promptToken: "   prompt-token-123   \n",
			envToken:    "",
			wantToken:   "prompt-token-123",
		},
		{
			name:        "empty prompt falls back to env token",
			promptToken: "\n",
			envToken:    "env-token-456",
			wantToken:   "env-token-456",
		},
		{
			name:        "whitespace prompt falls back to env token",
			promptToken: "   \n",
			envToken:    "  env-token-456  ",
			wantToken:   "env-token-456",
		},
		{
			name:        "both empty returns actionable error",
			promptToken: "\n",
			envToken:    "",
			wantErr:     true,
		},
		{
			name:        "both whitespace returns actionable error",
			promptToken: "   \n",
			envToken:    "   ",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := selectNgrokAuthtoken(tt.promptToken, tt.envToken)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, token)
				assert.Contains(t, err.Error(), "ngrok authtoken is required")
				assert.Contains(t, err.Error(), "NGROK_AUTHTOKEN")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, token)
		})
	}
}

func TestResolveNgrokAuthtoken(t *testing.T) {
	t.Run("resolves from reader", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("my-token\n"))
		token, err := resolveNgrokAuthtoken(reader)
		require.NoError(t, err)
		assert.Equal(t, "my-token", token)
	})

	t.Run("resolves from env when reader is blank", func(t *testing.T) {
		t.Setenv("NGROK_AUTHTOKEN", "env-authtoken-789")
		reader := bufio.NewReader(strings.NewReader("\n"))
		token, err := resolveNgrokAuthtoken(reader)
		require.NoError(t, err)
		assert.Equal(t, "env-authtoken-789", token)
	})

	t.Run("returns error when reader and env are blank", func(t *testing.T) {
		t.Setenv("NGROK_AUTHTOKEN", "")
		reader := bufio.NewReader(strings.NewReader("\n"))
		token, err := resolveNgrokAuthtoken(reader)
		require.Error(t, err)
		assert.Empty(t, token)
		assert.Contains(t, err.Error(), "ngrok authtoken is required")
	})
}
