package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/xdevplatform/xurl/config"
	xurlErrors "github.com/xdevplatform/xurl/errors"
	"github.com/xdevplatform/xurl/store"

	"runtime"

	"golang.org/x/oauth2"
)

type Auth struct {
	TokenStore         *store.TokenStore
	infoURL            string
	clientID           string
	clientSecret       string
	authURL            string
	tokenURL           string
	redirectURI        string
	redirectURIFromEnv bool
	appName            string // explicit app override (empty = use default)
}

var openBrowserFunc = openBrowser

var startListenerFunc = StartListener

// oauth2ExpirySkewSeconds refreshes a token slightly before its real expiry so a
// token handed to a caller does not expire mid-request.
const oauth2ExpirySkewSeconds = 30

// NewAuth creates a new Auth object.
// Credentials are resolved in order: env-var config → active app in .xurl store.
// If env var credentials are present, they're also backfilled into any migrated
// app that has tokens but no stored credentials.
func NewAuth(cfg *config.Config) *Auth {
	ts := store.NewTokenStoreWithCredentials(cfg.ClientID, cfg.ClientSecret)

	// Resolve client ID / secret: env vars take priority, then the active app.
	clientID := cfg.ClientID
	clientSecret := cfg.ClientSecret
	appName := cfg.AppName

	app := ts.ResolveApp(appName)
	if clientID == "" && app != nil {
		clientID = app.ClientID
	}
	if clientSecret == "" && app != nil {
		clientSecret = app.ClientSecret
	}

	return &Auth{
		TokenStore:         ts,
		infoURL:            cfg.InfoURL,
		clientID:           clientID,
		clientSecret:       clientSecret,
		authURL:            cfg.AuthURL,
		tokenURL:           cfg.TokenURL,
		redirectURI:        cfg.RedirectURI,
		redirectURIFromEnv: cfg.RedirectURIFromEnv,
		appName:            appName,
	}
}

// WithTokenStore sets the token store for the Auth object
func (a *Auth) WithTokenStore(tokenStore *store.TokenStore) *Auth {
	a.TokenStore = tokenStore
	return a
}

// AppName returns the active app name override (empty means use default).
func (a *Auth) AppName() string {
	return a.appName
}

// WithAppName sets the explicit app name override.
func (a *Auth) WithAppName(appName string) *Auth {
	a.appName = appName
	app := a.TokenStore.ResolveApp(appName)
	if app != nil {
		if app.ClientID != "" {
			a.clientID = app.ClientID
		}
		if app.ClientSecret != "" {
			a.clientSecret = app.ClientSecret
		}
	}
	if !a.redirectURIFromEnv {
		a.redirectURI = a.resolveRedirectURIForApp(appName)
	}
	return a
}

func (a *Auth) resolveRedirectURIForApp(appName string) string {
	app := a.TokenStore.ResolveApp(appName)
	if app != nil && app.RedirectURI != "" {
		return app.RedirectURI
	}
	return config.DefaultRedirectURI
}

// GetOAuth1Header gets the OAuth1 header for a request
func (a *Auth) GetOAuth1Header(method, urlStr string, additionalParams map[string]string) (string, error) {
	token := a.TokenStore.GetOAuth1TokensForApp(a.appName)
	if token == nil || token.OAuth1 == nil {
		return "", xurlErrors.NewAuthError("TokenNotFound", errors.New("OAuth1 token not found"))
	}

	oauth1Token := token.OAuth1

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", xurlErrors.NewAuthError("InvalidURL", err)
	}

	params := make(map[string]string)

	query := parsedURL.Query()
	for key := range query {
		params[key] = query.Get(key)
	}

	for key, value := range additionalParams {
		params[key] = value
	}

	params["oauth_consumer_key"] = oauth1Token.ConsumerKey
	params["oauth_nonce"] = generateNonce()
	params["oauth_signature_method"] = "HMAC-SHA1"
	params["oauth_timestamp"] = generateTimestamp()
	params["oauth_token"] = oauth1Token.AccessToken
	params["oauth_version"] = "1.0"

	signature, err := generateSignature(method, urlStr, params, oauth1Token.ConsumerSecret, oauth1Token.TokenSecret)
	if err != nil {
		return "", xurlErrors.NewAuthError("SignatureGenerationError", err)
	}

	var oauthParams []string
	oauthParams = append(oauthParams, fmt.Sprintf("oauth_consumer_key=\"%s\"", encode(oauth1Token.ConsumerKey)))
	oauthParams = append(oauthParams, fmt.Sprintf("oauth_nonce=\"%s\"", encode(params["oauth_nonce"])))
	oauthParams = append(oauthParams, fmt.Sprintf("oauth_signature=\"%s\"", encode(signature)))
	oauthParams = append(oauthParams, fmt.Sprintf("oauth_signature_method=\"%s\"", encode("HMAC-SHA1")))
	oauthParams = append(oauthParams, fmt.Sprintf("oauth_timestamp=\"%s\"", encode(params["oauth_timestamp"])))
	oauthParams = append(oauthParams, fmt.Sprintf("oauth_token=\"%s\"", encode(oauth1Token.AccessToken)))
	oauthParams = append(oauthParams, fmt.Sprintf("oauth_version=\"%s\"", encode("1.0")))

	return "OAuth " + strings.Join(oauthParams, ", "), nil
}

// GetOAuth2Token gets or refreshes an OAuth2 token
func (a *Auth) GetOAuth2Header(username string) (string, error) {
	var token *store.Token

	if username != "" {
		token = a.TokenStore.GetOAuth2TokenForApp(a.appName, username)
	} else {
		token = a.TokenStore.GetFirstOAuth2TokenForApp(a.appName)
	}

	if token == nil {
		accessToken, err := a.OAuth2Flow(username)
		if err != nil {
			return "", err
		}
		return "Bearer " + accessToken, nil
	}

	accessToken, err := a.RefreshOAuth2Token(username)
	if err != nil {
		return "", xurlErrors.NewAuthError("RefreshTokenError", err)
	}
	return "Bearer " + accessToken, nil
}

// oauth2AuthStyle picks how client credentials are sent to the token endpoint.
// X requires confidential clients (those with a client secret) to authenticate
// with an HTTP Basic Authorization header; public clients (PKCE, no secret) send
// the client_id in the request body. Letting x/oauth2 auto-detect proved
// unreliable against X (it could fail with "unauthorized_client: Missing valid
// authorization header"), so the style is selected explicitly.
func (a *Auth) oauth2AuthStyle() oauth2.AuthStyle {
	if a.clientSecret != "" {
		return oauth2.AuthStyleInHeader
	}
	return oauth2.AuthStyleInParams
}

// newOAuth2Config builds the OAuth2 config for the authorization-code flow.
func (a *Auth) newOAuth2Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     a.clientID,
		ClientSecret: a.clientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   a.authURL,
			TokenURL:  a.tokenURL,
			AuthStyle: a.oauth2AuthStyle(),
		},
		RedirectURL: a.redirectURI,
		Scopes:      getOAuth2Scopes(),
	}
}

// oauth2Attempt carries the per-login PKCE/state material and the authorize URL,
// shared by the interactive and headless flows.
type oauth2Attempt struct {
	config   *oauth2.Config
	state    string
	verifier string
	authURL  string
}

// prepareOAuth2Flow generates the state and PKCE verifier/challenge and builds
// the authorize URL.
func (a *Auth) prepareOAuth2Flow() (*oauth2Attempt, error) {
	config := a.newOAuth2Config()

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, xurlErrors.NewAuthError("IOError", err)
	}
	state := base64.StdEncoding.EncodeToString(b)

	verifier, challenge, err := generateCodeVerifierAndChallenge()
	if err != nil {
		return nil, xurlErrors.NewAuthError("IOError", err)
	}

	authURL := config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"))

	return &oauth2Attempt{config: config, state: state, verifier: verifier, authURL: authURL}, nil
}

// exchangeAndSave swaps an authorization code for a token (using the PKCE
// verifier) and persists it. Diagnostics go to stderr so callers that reserve
// stdout for machine output (e.g. the mcp bridge) are never corrupted.
func (a *Auth) exchangeAndSave(attempt *oauth2Attempt, username, code string) (string, error) {
	token, err := attempt.config.Exchange(context.Background(), code,
		oauth2.SetAuthURLParam("code_verifier", attempt.verifier))
	if err != nil {
		return "", xurlErrors.NewAuthError("TokenExchangeError", err)
	}

	usernameStr, resolvedFromLookup := a.resolveStorageUsername(username, token.AccessToken)
	if err := a.saveOAuth2Token(usernameStr, token); err != nil {
		return "", xurlErrors.NewAuthError("TokenStorageError", err)
	}
	if username == "" && !resolvedFromLookup {
		fmt.Fprintln(os.Stderr, "Warning: authenticated successfully, but could not resolve your username via /2/users/me.")
		fmt.Fprintln(os.Stderr, "The OAuth2 token was saved without a username label. Re-run `xurl auth oauth2 YOUR_USERNAME` if you want a named token.")
	}

	return token.AccessToken, nil
}

// OAuth2Flow runs the interactive authorization-code flow: it starts a local
// callback listener, opens the browser, and waits for the redirect. On machines
// without a reachable browser/callback, use the headless flow (StartHeadlessLogin) instead.
func (a *Auth) OAuth2Flow(username string) (string, error) {
	attempt, err := a.prepareOAuth2Flow()
	if err != nil {
		return "", err
	}

	listenerConfig, err := listenerConfigFromRedirectURI(a.redirectURI)
	if err != nil {
		return "", xurlErrors.NewAuthError("InvalidRedirectURI", err)
	}

	codeChan := make(chan string, 1)
	listenerReady := make(chan struct{})
	listenerErrChan := make(chan error, 1)

	callback := func(code, receivedState string) error {
		if receivedState != attempt.state {
			return xurlErrors.NewAuthError("InvalidState", errors.New("invalid state parameter"))
		}

		if code == "" {
			return xurlErrors.NewAuthError("InvalidCode", errors.New("empty authorization code"))
		}

		codeChan <- code
		return nil
	}

	go func() {
		if err := startListenerFunc(listenerConfig.Addresses, listenerConfig.CallbackPath, callback, listenerReady); err != nil {
			listenerErrChan <- err
		}
	}()

	select {
	case <-listenerReady:
	case err := <-listenerErrChan:
		return "", xurlErrors.NewAuthError("ListenerError", err)
	}

	if err := openBrowserFunc(attempt.authURL); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to open browser automatically. Please visit this URL manually:")
		fmt.Fprintln(os.Stderr, attempt.authURL)
		fmt.Fprintln(os.Stderr, "(On a remote/headless machine, re-run with --headless to paste the code instead.)")
	}

	var code string
	select {
	case code = <-codeChan:
		if code == "" {
			return "", xurlErrors.NewAuthError("ListenerError", errors.New("oauth2 listener failed"))
		}
	case err := <-listenerErrChan:
		return "", xurlErrors.NewAuthError("ListenerError", err)
	case <-time.After(5 * time.Minute):
		return "", xurlErrors.NewAuthError("Timeout", errors.New("authentication timed out"))
	}

	return a.exchangeAndSave(attempt, username, code)
}

// HeadlessLogin is an in-progress headless authorization-code login. Obtain one
// with StartHeadlessLogin, show the user AuthURL(), then pass whatever they paste
// back (the full redirect URL or just the code) to Complete. This avoids a local
// browser/callback entirely, so it works on headless/remote machines and never
// depends on the browser or a listener succeeding. Presentation is left to the
// caller -- the auth package never writes prompts itself.
type HeadlessLogin struct {
	auth     *Auth
	attempt  *oauth2Attempt
	username string
}

// StartHeadlessLogin begins a headless login: it generates the PKCE/state
// material and the authorize URL without opening a browser or starting a
// listener.
func (a *Auth) StartHeadlessLogin(username string) (*HeadlessLogin, error) {
	attempt, err := a.prepareOAuth2Flow()
	if err != nil {
		return nil, err
	}
	return &HeadlessLogin{auth: a, attempt: attempt, username: username}, nil
}

// AuthURL is the URL the user opens in a browser (on any device) to authorize.
func (h *HeadlessLogin) AuthURL() string { return h.attempt.authURL }

// RedirectURI is the callback the browser is redirected to (where the code
// appears in the address bar), shown to the user so they know what to copy.
func (h *HeadlessLogin) RedirectURI() string { return h.auth.redirectURI }

// Complete finishes the login from the value the user pasted back -- the full
// redirect URL, a bare query string, or just the code -- verifying state (when
// present), exchanging the code for a token, and persisting it.
func (h *HeadlessLogin) Complete(pasted string) (string, error) {
	code, err := parseHeadlessAuthCode(pasted, h.attempt.state)
	if err != nil {
		return "", xurlErrors.NewAuthError("InvalidCode", err)
	}
	return h.auth.exchangeAndSave(h.attempt, h.username, code)
}

// parseHeadlessAuthCode extracts the authorization code from a pasted value,
// which may be the full redirect URL, a bare query string, or just the code. If
// a state value is present it must match wantState (CSRF protection); a bare
// code carries no state, which is acceptable for this user-initiated paste flow.
func parseHeadlessAuthCode(input, wantState string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("no authorization code provided")
	}

	// A pasted URL or query string carries "code=" (and usually "state=").
	if strings.Contains(input, "code=") {
		var q url.Values
		if u, perr := url.Parse(input); perr == nil && len(u.Query()) > 0 {
			q = u.Query()
		} else if pq, perr := url.ParseQuery(input); perr == nil {
			q = pq
		}
		code := q.Get("code")
		if code == "" {
			return "", errors.New("could not find a 'code' value in the pasted input")
		}
		if st := q.Get("state"); st != "" && wantState != "" && st != wantState {
			return "", errors.New("state mismatch: the pasted URL is from a different login attempt")
		}
		return code, nil
	}

	// Otherwise treat the whole input as the bare authorization code.
	return input, nil
}

// RefreshOAuth2Token validates and refreshes an OAuth2 token if needed
func (a *Auth) RefreshOAuth2Token(username string) (string, error) {
	return a.refreshOAuth2Token(username, false)
}

// ForceRefreshOAuth2Token always performs the refresh-token grant, ignoring the
// locally cached expiry. Use it when the server rejects a token the local clock
// still considers valid (e.g. an HTTP 401 after a revocation or scope change).
func (a *Auth) ForceRefreshOAuth2Token(username string) (string, error) {
	return a.refreshOAuth2Token(username, true)
}

func (a *Auth) refreshOAuth2Token(username string, force bool) (string, error) {
	storedUsername, token := a.getOAuth2TokenRecord(username)
	if token == nil || token.OAuth2 == nil {
		return "", xurlErrors.NewAuthError("TokenNotFound", errors.New("oauth2 token not found"))
	}

	if !force {
		currentTime := time.Now().Unix()
		// Refresh slightly before the real expiry so a token handed to a caller
		// does not expire in-flight (mirrors x/oauth2's expiryDelta).
		if uint64(currentTime)+oauth2ExpirySkewSeconds < token.OAuth2.ExpirationTime {
			return token.OAuth2.AccessToken, nil
		}
	}

	config := &oauth2.Config{
		ClientID:     a.clientID,
		ClientSecret: a.clientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL:  a.tokenURL,
			AuthStyle: a.oauth2AuthStyle(),
		},
	}

	tokenSource := config.TokenSource(context.Background(), &oauth2.Token{
		RefreshToken: token.OAuth2.RefreshToken,
	})

	newToken, err := tokenSource.Token()
	if err != nil {
		return "", xurlErrors.NewAuthError("RefreshTokenError", err)
	}

	usernameStr := storedUsername
	if usernameStr == "" {
		resolvedUsername, _ := a.resolveStorageUsername("", newToken.AccessToken)
		usernameStr = resolvedUsername
	}
	if storedUsername == "" && usernameStr != "" {
		if err := a.TokenStore.ClearOAuth2TokenForApp(a.appName, storedUsername); err != nil {
			return "", xurlErrors.NewAuthError("RefreshTokenError", err)
		}
	}
	if err := a.saveOAuth2Token(usernameStr, newToken); err != nil {
		return "", xurlErrors.NewAuthError("RefreshTokenError", err)
	}

	return newToken.AccessToken, nil
}

// GetValidOAuth2Token returns a valid OAuth2 access token for the active app and
// the given username, refreshing and persisting it if it has expired. Pass an
// empty username to use the app's default (or first) user.
//
// Unlike GetOAuth2Header it never launches the interactive browser flow, so it
// is safe for non-interactive/scripted use. Callers that want browser fallback
// (e.g. the mcp bridge) should invoke OAuth2Flow themselves when this returns an
// error. This is the shared token-resolution primitive used by `xurl token` and
// `xurl mcp`.
func (a *Auth) GetValidOAuth2Token(username string) (string, error) {
	return a.RefreshOAuth2Token(username)
}

type oauth2ListenerConfig struct {
	Addresses    []string
	CallbackPath string
}

func listenerConfigFromRedirectURI(redirectURI string) (oauth2ListenerConfig, error) {
	parsedURL, err := url.Parse(redirectURI)
	if err != nil {
		return oauth2ListenerConfig{}, err
	}

	host := parsedURL.Hostname()
	if host == "" {
		host = "localhost"
	}

	port := parsedURL.Port()
	if port == "" {
		port = "8080"
	}

	callbackPath := parsedURL.Path
	if callbackPath == "" {
		callbackPath = "/callback"
	}

	return oauth2ListenerConfig{
		Addresses:    listenerAddressesForHost(host, port),
		CallbackPath: callbackPath,
	}, nil
}

func listenerAddressesForHost(host, port string) []string {
	if strings.EqualFold(host, "localhost") {
		return []string{
			net.JoinHostPort("127.0.0.1", port),
			net.JoinHostPort("::1", port),
		}
	}

	return []string{net.JoinHostPort(host, port)}
}

func (a *Auth) resolveStorageUsername(explicitUsername, accessToken string) (string, bool) {
	if explicitUsername != "" {
		return explicitUsername, true
	}

	username, err := a.fetchUsername(accessToken)
	if err != nil {
		return "", false
	}

	return username, true
}

func (a *Auth) getOAuth2TokenRecord(username string) (string, *store.Token) {
	if username != "" {
		return username, a.TokenStore.GetOAuth2TokenForApp(a.appName, username)
	}

	return a.TokenStore.GetFirstOAuth2TokenRecordForApp(a.appName)
}

func (a *Auth) saveOAuth2Token(username string, token *oauth2.Token) error {
	// A zero expiry means the provider didn't return one; store 0 so the token
	// is treated as already expired and refreshed on next use rather than cast
	// into a far-future timestamp that would never refresh.
	var expirationTime uint64
	if !token.Expiry.IsZero() {
		expirationTime = uint64(token.Expiry.Unix())
	}
	return a.TokenStore.SaveOAuth2TokenForApp(a.appName, username, token.AccessToken, token.RefreshToken, expirationTime)
}

// GetBearerTokenHeader gets the bearer token from the token store
func (a *Auth) GetBearerTokenHeader() (string, error) {
	token := a.TokenStore.GetBearerTokenForApp(a.appName)
	if token == nil {
		return "", xurlErrors.NewAuthError("TokenNotFound", errors.New("bearer token not found"))
	}
	return "Bearer " + token.Bearer, nil
}

func (a *Auth) fetchUsername(accessToken string) (string, error) {
	req, err := http.NewRequest("GET", a.infoURL, nil)
	if err != nil {
		return "", xurlErrors.NewAuthError("RequestCreationError", err)
	}

	req.Header.Add("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", xurlErrors.NewAuthError("NetworkError", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", xurlErrors.NewAuthError("IOError", err)
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return "", xurlErrors.NewAuthError("JSONDeserializationError", err)
	}

	if data["data"] != nil {
		if userData, ok := data["data"].(map[string]any); ok {
			if username, ok := userData["username"].(string); ok {
				return username, nil
			}
		}
	}

	return "", xurlErrors.NewAuthError("UsernameNotFound", errors.New("username not found when fetching username"))
}

func generateSignature(method, urlStr string, params map[string]string, consumerSecret, tokenSecret string) (string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", xurlErrors.NewAuthError("InvalidURL", err)
	}

	baseURL := fmt.Sprintf("%s://%s%s", parsedURL.Scheme, parsedURL.Host, parsedURL.Path)

	var keys []string
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var paramPairs []string
	for _, key := range keys {
		paramPairs = append(paramPairs, fmt.Sprintf("%s=%s", encode(key), encode(params[key])))
	}
	paramString := strings.Join(paramPairs, "&")

	signatureBaseString := fmt.Sprintf("%s&%s&%s",
		strings.ToUpper(method),
		encode(baseURL),
		encode(paramString))

	signingKey := fmt.Sprintf("%s&%s", encode(consumerSecret), encode(tokenSecret))

	h := hmac.New(sha1.New, []byte(signingKey))
	h.Write([]byte(signatureBaseString))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature, nil
}

func generateNonce() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000000))
	return n.String()
}

func generateTimestamp() string {
	return fmt.Sprintf("%d", time.Now().Unix())
}

func encode(s string) string {
	return url.QueryEscape(s)
}

func generateCodeVerifierAndChallenge() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.New()
	h.Write([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	return verifier, challenge, nil
}

func getOAuth2Scopes() []string {
	readScopes := []string{
		"tweet.read",
		"users.read",
		"bookmark.read",
		"follows.read",
		"list.read",
		"block.read",
		"mute.read",
		"like.read",
		"users.email",
		"dm.read",
		"broadcast.read",
	}

	writeScopes := []string{
		"tweet.write",
		"tweet.moderate.write",
		"follows.write",
		"bookmark.write",
		"block.write",
		"mute.write",
		"like.write",
		"list.write",
		"media.write",
		"dm.write",
		"broadcast.write",
	}

	otherScopes := []string{
		"offline.access",
		"space.read",
	}

	var scopes []string
	scopes = append(scopes, readScopes...)
	scopes = append(scopes, writeScopes...)
	scopes = append(scopes, otherScopes...)

	return scopes
}

func openBrowser(url string) error {
	cmd, args := browserLaunchCommand(runtime.GOOS, url)
	return exec.Command(cmd, args...).Start()
}

func browserLaunchCommand(goos, url string) (string, []string) {
	switch goos {
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		return "open", []string{url}
	default:
		return "xdg-open", []string{url}
	}
}
