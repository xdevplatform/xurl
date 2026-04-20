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

// OAuth2Flow starts the OAuth2 flow
func (a *Auth) OAuth2Flow(username string) (string, error) {
	config := &oauth2.Config{
		ClientID:     a.clientID,
		ClientSecret: a.clientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  a.authURL,
			TokenURL: a.tokenURL,
		},
		RedirectURL: a.redirectURI,
		Scopes:      getOAuth2Scopes(),
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", xurlErrors.NewAuthError("IOError", err)
	}
	state := base64.StdEncoding.EncodeToString(b)

	verifier, challenge := generateCodeVerifierAndChallenge()

	authURL := config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"))

	listenerConfig, err := listenerConfigFromRedirectURI(a.redirectURI)
	if err != nil {
		return "", xurlErrors.NewAuthError("InvalidRedirectURI", err)
	}

	codeChan := make(chan string, 1)
	listenerReady := make(chan struct{})
	listenerErrChan := make(chan error, 1)

	callback := func(code, receivedState string) error {
		if receivedState != state {
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

	err = openBrowserFunc(authURL)
	if err != nil {
		fmt.Println("Failed to open browser automatically. Please visit this URL manually:")
		fmt.Println(authURL)
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

	token, err := config.Exchange(context.Background(), code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return "", xurlErrors.NewAuthError("TokenExchangeError", err)
	}

	usernameStr, resolvedFromLookup := a.resolveStorageUsername(username, token.AccessToken)
	if err := a.saveOAuth2Token(usernameStr, token); err != nil {
		return "", xurlErrors.NewAuthError("TokenStorageError", err)
	}
	if username == "" && !resolvedFromLookup {
		fmt.Println("Warning: authenticated successfully, but could not resolve your username via /2/users/me.")
		fmt.Println("The OAuth2 token was saved without a username label. Re-run `xurl auth oauth2 YOUR_USERNAME` if you want a named token.")
	}

	return token.AccessToken, nil
}

// RefreshOAuth2Token validates and refreshes an OAuth2 token if needed
func (a *Auth) RefreshOAuth2Token(username string) (string, error) {
	storedUsername, token := a.getOAuth2TokenRecord(username)
	if token == nil || token.OAuth2 == nil {
		return "", xurlErrors.NewAuthError("TokenNotFound", errors.New("oauth2 token not found"))
	}

	currentTime := time.Now().Unix()
	if uint64(currentTime) < token.OAuth2.ExpirationTime {
		return token.OAuth2.AccessToken, nil
	}

	config := &oauth2.Config{
		ClientID:     a.clientID,
		ClientSecret: a.clientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: a.tokenURL,
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
	expirationTime := uint64(time.Now().Add(time.Duration(token.Expiry.Unix()-time.Now().Unix()) * time.Second).Unix())
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

	client := &http.Client{}
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

func generateCodeVerifierAndChallenge() (string, string) {
	b := make([]byte, 32)
	rand.Read(b)
	verifier := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.New()
	h.Write([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	return verifier, challenge
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
