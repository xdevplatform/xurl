package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xdevplatform/xurl/store"
	"gopkg.in/yaml.v3"
)

// Independent bridge processes all load the expired grant BEFORE any refresh.
// The fake provider rotates it once and rejects subsequent uses of the old grant.
func TestSharedRefreshAcrossProcesses(t *testing.T) {
	if os.Getenv("XURL_FIX_HELPER") == "1" {
		data, err := os.ReadFile(os.Getenv("XURL_FIX_FILE"))
		require.NoError(t, err)
		ts := &store.TokenStore{}
		require.NoError(t, yaml.Unmarshal(data, ts))
		ts.FilePath = os.Getenv("XURL_FIX_FILE")
		a := &Auth{TokenStore: ts, tokenURL: os.Getenv("XURL_FIX_URL")}
		ready := os.Getenv("XURL_FIX_READY")
		require.NoError(t, os.WriteFile(ready, []byte("ready"), 0600))
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(os.Getenv("XURL_FIX_GATE")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("gate timed out")
			}
			time.Sleep(10 * time.Millisecond)
		}
		_, err = a.GetValidOAuth2Token("alice")
		if err != nil {
			t.Fatal("shared refresh failed")
		}
		return
	}
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) != 1 || r.Form.Get("refresh_token") != "old-refresh" {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		time.Sleep(80 * time.Millisecond)
		json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "token_type": "Bearer", "expires_in": 7200})
	}))
	defer provider.Close()
	ts, dir := createTempTokenStore(t)
	defer os.RemoveAll(dir)
	require.NoError(t, ts.SaveOAuth2TokenForApp("default", "alice", "old-access", "old-refresh", uint64(time.Now().Add(-time.Hour).Unix())))
	gate := filepath.Join(dir, "gate")
	var children []*exec.Cmd
	for i := 0; i < 4; i++ {
		ready := filepath.Join(dir, string(rune('a'+i))+".ready")
		c := exec.Command(os.Args[0], "-test.run=^TestSharedRefreshAcrossProcesses$", "-test.timeout=15s")
		c.Env = append(os.Environ(), "XURL_FIX_HELPER=1", "XURL_FIX_FILE="+ts.FilePath, "XURL_FIX_URL="+provider.URL, "XURL_FIX_READY="+ready, "XURL_FIX_GATE="+gate)
		require.NoError(t, c.Start())
		children = append(children, c)
		t.Cleanup(func() {
			if c.ProcessState == nil {
				_ = c.Process.Kill()
				_ = c.Wait()
			}
		})
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			require.True(t, time.Now().Before(deadline), "helper did not load grant")
			time.Sleep(10 * time.Millisecond)
		}
	}
	require.NoError(t, os.WriteFile(gate, []byte("go"), 0600))
	for _, c := range children {
		require.NoError(t, c.Wait(), "independent process must reuse the refreshed grant")
	}
	require.Equal(t, int32(1), calls.Load(), "only one process may redeem the old refresh token")
}

func TestForceRefreshReusesSiblingToken(t *testing.T) {
	ts, dir := createTempTokenStore(t)
	defer os.RemoveAll(dir)
	future := uint64(time.Now().Add(time.Hour).Unix())
	require.NoError(t, ts.SaveOAuth2TokenForApp("default", "alice", "old-access", "old-refresh", future))
	a := &Auth{TokenStore: ts, tokenURL: "http://127.0.0.1:1"}
	data, err := os.ReadFile(ts.FilePath)
	require.NoError(t, err)
	sibling := &store.TokenStore{}
	require.NoError(t, yaml.Unmarshal(data, sibling))
	sibling.FilePath = ts.FilePath
	require.NoError(t, sibling.SaveOAuth2TokenForApp("default", "alice", "sibling-access", "sibling-refresh", future))
	got, err := a.ForceRefreshOAuth2Token("alice")
	require.NoError(t, err)
	require.Equal(t, "sibling-access", got)
}

func TestRefreshDoesNotResurrectRemovedGrant(t *testing.T) {
	ts, dir := createTempTokenStore(t)
	defer os.RemoveAll(dir)
	require.NoError(t, ts.SaveOAuth2TokenForApp("default", "alice", "cached-access", "cached-refresh", uint64(time.Now().Add(time.Hour).Unix())))
	a := &Auth{TokenStore: ts, tokenURL: "http://127.0.0.1:1"}
	require.NoError(t, os.Remove(ts.FilePath))
	_, err := a.GetValidOAuth2Token("alice")
	require.Error(t, err)
	require.Contains(t, err.Error(), "TokenNotFound")
	_, err = os.Stat(ts.FilePath)
	require.True(t, os.IsNotExist(err))
}
