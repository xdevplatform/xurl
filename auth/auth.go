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
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"

	"xurl/config"
	xurlErrors "xurl/errors"
	"xurl/store"

	"runtime"

	"golang.org/x/oauth2"
)

type Auth struct {
	TokenStore   *store.TokenStore
	infoURL      string
	clientID     string
	clientSecret string
	authURL      string
	tokenURL     string
	redirectURI  string
}

// NewAuth creates a new Auth object
func NewAuth(config *config.Config) *Auth {
	return &Auth{
		TokenStore:   store.NewTokenStore(),
		infoURL:      config.InfoURL,
		clientID:     config.ClientID,
		clientSecret: config.ClientSecret,
		authURL:      config.AuthURL,
		tokenURL:     config.TokenURL,
		redirectURI:  config.RedirectURI,
	}
}

// WithTokenStore sets the token store for the Auth object
func (a *Auth) WithTokenStore(tokenStore *store.TokenStore) *Auth {
	a.TokenStore = tokenStore
	return a
}

// GetOAuth1Header gets the OAuth1 header for a request
func (a *Auth) GetOAuth1Header(method, urlStr string, additionalParams map[string]string) (string, error) {
	token := a.TokenStore.GetOAuth1Tokens()
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
		token = a.TokenStore.GetOAuth2Token(username)
	} else {
		token = a.TokenStore.GetFirstOAuth2Token()
	}

	if token == nil {
		return a.OAuth2Flow(username)
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

	err := openBrowser(authURL)
	if err != nil {
		fmt.Println("Failed to open browser automatically. Please visit this URL manually:")
		fmt.Println(authURL)
	}

	codeChan := make(chan string, 1)

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
		parsedURL, err := url.Parse(a.redirectURI)
		if err != nil {
			codeChan <- ""
			return
		}

		port := 8080
		if parsedURL.Port() != "" {
			fmt.Sscanf(parsedURL.Port(), "%d", &port)
		}

		if err := StartListener(port, callback); err != nil {
			fmt.Printf("Error in OAuth listener: %v\n", err)
		}
	}()

	var code string
	select {
	case code = <-codeChan:
		if code == "" {
			return "", xurlErrors.NewAuthError("ListenerError", errors.New("oauth2 listener failed"))
		}
	case <-time.After(5 * time.Minute):
		return "", xurlErrors.NewAuthError("Timeout", errors.New("authentication timed out"))
	}

	token, err := config.Exchange(context.Background(), code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return "", xurlErrors.NewAuthError("TokenExchangeError", err)
	}

	var usernameStr string
	if username != "" {
		usernameStr = username
	} else {
		fetchedUsername, err := a.fetchUsername(token.AccessToken)
		if err != nil {
			return "", err
		}
		usernameStr = fetchedUsername
	}

	expirationTime := uint64(time.Now().Add(time.Duration(token.Expiry.Unix()-time.Now().Unix()) * time.Second).Unix())

	err = a.TokenStore.SaveOAuth2Token(usernameStr, token.AccessToken, token.RefreshToken, expirationTime)
	if err != nil {
		return "", xurlErrors.NewAuthError("TokenStorageError", err)
	}

	return token.AccessToken, nil
}

// RefreshOAuth2Token validates and refreshes an OAuth2 token if needed
func (a *Auth) RefreshOAuth2Token(username string) (string, error) {
	var token *store.Token

	if username != "" {
		token = a.TokenStore.GetOAuth2Token(username)
	} else {
		token = a.TokenStore.GetFirstOAuth2Token()
	}

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

	var usernameStr string
	if username != "" {
		usernameStr = username
	} else {
		fetchedUsername, err := a.fetchUsername(newToken.AccessToken)
		if err != nil {
			return "", xurlErrors.NewAuthError("UsernameFetchError", err)
		}
		usernameStr = fetchedUsername
	}

	expirationTime := uint64(time.Now().Add(time.Duration(newToken.Expiry.Unix()-time.Now().Unix()) * time.Second).Unix())

	err = a.TokenStore.SaveOAuth2Token(usernameStr, newToken.AccessToken, newToken.RefreshToken, expirationTime)
	if err != nil {
		return "", xurlErrors.NewAuthError("RefreshTokenError", err)
	}

	return newToken.AccessToken, nil
}

// GetBearerTokenHeader gets the bearer token from the token store
func (a *Auth) GetBearerTokenHeader() (string, error) {
	token := a.TokenStore.GetBearerToken()
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

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", xurlErrors.NewAuthError("JSONDeserializationError", err)
	}

	if data["data"] != nil {
		if userData, ok := data["data"].(map[string]interface{}); ok {
			if username, ok := userData["username"].(string); ok {
				return username, nil
			}
		}
	}

	return "", xurlErrors.NewAuthError("UsernameNotFound", errors.New("username not found in response"))
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
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}
