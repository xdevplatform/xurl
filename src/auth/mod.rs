pub mod listener;
pub mod token_store;

use crate::auth::listener::listen_for_code;
use crate::auth::token_store::Token;
use crate::auth::token_store::TokenStore;
use crate::auth::token_store::TokenStoreError;
use crate::config::Config;

use oauth2::basic::BasicClient;
use oauth2::reqwest::async_http_client;
use oauth2::RefreshToken;
use oauth2::{
    basic::BasicTokenType, AuthUrl, AuthorizationCode, ClientId, ClientSecret, CsrfToken,
    EmptyExtraTokenFields, PkceCodeChallenge, RedirectUrl, Scope, StandardTokenResponse,
    TokenResponse, TokenUrl,
};

use base64::{engine::general_purpose::STANDARD, Engine};
use hmac::{Hmac, Mac};
use percent_encoding::{utf8_percent_encode, NON_ALPHANUMERIC};
use rand::Rng;
use sha1::Sha1;
use std::collections::BTreeMap;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

#[derive(Debug, thiserror::Error)]
pub enum AuthError {
    #[error("Missing environment variable: {0}")]
    MissingEnvVar(&'static str),
    #[error("Invalid URL: {0}")]
    InvalidUrl(String),
    #[error("Invalid code: {0}")]
    InvalidCode(String),
    #[error("Invalid token: {0}")]
    InvalidToken(String),
    #[error("Authorization error: {0}")]
    AuthorizationError(String),
    #[error("Network error: {0}")]
    NetworkError(String),
    #[error("IO error: {0}")]
    IoError(#[from] std::io::Error),
    #[error("Token not found")]
    TokenNotFound(String),
    #[error("Invalid auth type: {0}")]
    InvalidAuthType(String),
    #[error("Non-OAuth2 tokens found when looking for OAuth2 token")]
    WrongTokenFoundInStore,
    #[error("Token store error: {0}")]
    TokenStoreError(#[from] TokenStoreError),
}

pub struct Auth {
    token_store: TokenStore,
    info_url: String,
    client_id: String,
    client_secret: String,
    auth_url: String,
    token_url: String,
    redirect_uri: String,
}

impl Auth {
    pub fn new(config: Config) -> Self {
        Self {
            token_store: TokenStore::new(),
            info_url: config.info_url,
            client_id: config.client_id,
            client_secret: config.client_secret,
            auth_url: config.auth_url,
            token_url: config.token_url,
            redirect_uri: config.redirect_uri,
        }
    }

    #[allow(dead_code)]
    pub fn with_token_store(mut self, token_store: TokenStore) -> Self {
        self.token_store = token_store;
        self
    }

    pub fn oauth1(
        &self,
        method: &str,
        url: &str,
        additional_params: Option<BTreeMap<String, String>>,
    ) -> Result<String, AuthError> {
        let nonce = generate_nonce();
        let timestamp = generate_timestamp()?;

        let token = self
            .token_store
            .get_oauth1_tokens()
            .ok_or(AuthError::TokenNotFound(
                "No OAuth1 tokens found".to_string(),
            ))?;
        let (consumer_key, access_token, consumer_secret, token_secret) = match token {
            Token::OAuth1(token) => (
                token.consumer_key,
                token.access_token,
                token.consumer_secret,
                token.token_secret,
            ),
            _ => return Err(AuthError::InvalidToken("Invalid token type".to_string())),
        };

        let mut params = BTreeMap::new();
        params.insert("oauth_consumer_key".to_string(), consumer_key.to_string());
        params.insert("oauth_nonce".to_string(), nonce);
        params.insert(
            "oauth_signature_method".to_string(),
            "HMAC-SHA1".to_string(),
        );
        params.insert("oauth_timestamp".to_string(), timestamp);
        params.insert("oauth_token".to_string(), access_token.to_string());
        params.insert("oauth_version".to_string(), "1.0".to_string());

        // Add any additional parameters
        if let Some(add_params) = additional_params {
            params.extend(add_params);
        }

        let signature = generate_signature(method, url, &params, &consumer_secret, &token_secret)?;

        params.insert("oauth_signature".to_string(), signature);

        let auth_header = params
            .iter()
            .filter(|(k, _)| k.starts_with("oauth_"))
            .map(|(k, v)| format!("{}=\"{}\"", encode(k), encode(v)))
            .collect::<Vec<_>>()
            .join(", ");

        Ok(format!("OAuth {}", auth_header))
    }

    pub async fn oauth2(&mut self, username: Option<&str>) -> Result<String, AuthError> {
        if let Some(username) = username {
            if let Some(token) = self.token_store.get_oauth2_token(username) {
                match token {
                    Token::OAuth2(token) => {
                        if SystemTime::now()
                            .duration_since(UNIX_EPOCH)
                            .unwrap()
                            .as_secs()
                            > token.expiration_time
                        {
                            return self.oauth2_refresh_token(Some(username)).await;
                        }
                        return Ok(token.access_token);
                    }
                    _ => return Err(AuthError::WrongTokenFoundInStore),
                }
            } else {
                return Err(AuthError::TokenNotFound(format!(
                    "No cached OAuth2 token found for {}",
                    username
                )));
            }
        }

        if self.client_id.is_empty() || self.client_secret.is_empty() {
            return Err(AuthError::MissingEnvVar("CLIENT_ID or CLIENT_SECRET"));
        }

        let client = self.create_oauth2_client().await?;

        let (code_challenge, code_verifier) = PkceCodeChallenge::new_random_sha256();

        let (auth_url, _csrf_token) = client
            .authorize_url(CsrfToken::new_random)
            .add_scopes(OAuth2Scopes::all())
            .set_pkce_challenge(code_challenge)
            .url();

        webbrowser::open(auth_url.as_str())
            .map_err(|e| AuthError::IoError(std::io::Error::new(std::io::ErrorKind::Other, e)))?;

        let code = listen_for_code(8080)
            .await
            .map_err(|e| AuthError::InvalidCode(e))?;

        let token = client
            .exchange_code(AuthorizationCode::new(code))
            .set_pkce_verifier(code_verifier)
            .request_async(async_http_client)
            .await
            .map_err(|e| match e {
                oauth2::RequestTokenError::ServerResponse(e) => {
                    AuthError::AuthorizationError(e.to_string())
                }
                oauth2::RequestTokenError::Request(e) => AuthError::NetworkError(e.to_string()),
                _ => AuthError::InvalidToken(e.to_string()),
            })?;

        let username = self
            .fetch_username(&token.access_token().secret().to_string())
            .await?;
        self.save_token_data(&username, &token)?;

        Ok(token.access_token().secret().to_string())
    }

    pub async fn oauth2_refresh_token(
        &mut self,
        username: Option<&str>,
    ) -> Result<String, AuthError> {
        let refresh_token = if let Some(username) = username {
            if let Some(token) = self.token_store.get_oauth2_token(username) {
                match token {
                    Token::OAuth2(token) => token.refresh_token,
                    _ => return Err(AuthError::WrongTokenFoundInStore),
                }
            } else {
                return Err(AuthError::TokenNotFound(format!(
                    "No cached OAuth2 token found for {}",
                    username
                )));
            }
        } else {
            let token = self.token_store.get_first_oauth2_token();
            if let Some(token) = token {
                match token {
                    Token::OAuth2(token) => token.refresh_token,
                    _ => return Err(AuthError::WrongTokenFoundInStore),
                }
            } else {
                return Err(AuthError::TokenNotFound(
                    "No OAuth2 tokens found".to_string(),
                ));
            }
        };
        let client = self.create_oauth2_client().await?;

        let token = client
            .exchange_refresh_token(&RefreshToken::new(refresh_token))
            .request_async(async_http_client)
            .await
            .map_err(|e| AuthError::InvalidToken(e.to_string()))?;

        let username = self
            .fetch_username(&token.access_token().secret().to_string())
            .await?;
        self.save_token_data(&username, &token)?;

        Ok(token.access_token().secret().to_string())
    }

    pub fn bearer_token(&self) -> Option<String> {
        self.token_store
            .get_bearer_token()
            .as_ref()
            .and_then(|token| match token {
                Token::Bearer(token) => Some(token.clone()),
                _ => None,
            })
    }

    pub fn get_token_store(&mut self) -> &mut TokenStore {
        &mut self.token_store
    }

    async fn create_oauth2_client(&self) -> Result<BasicClient, AuthError> {
        let client = BasicClient::new(
            ClientId::new(self.client_id.clone()),
            Some(ClientSecret::new(self.client_secret.clone())),
            AuthUrl::new(self.auth_url.clone())
                .map_err(|e| AuthError::InvalidUrl(e.to_string()))?,
            Some(
                TokenUrl::new(self.token_url.clone())
                    .map_err(|e| AuthError::InvalidUrl(e.to_string()))?,
            ),
        )
        .set_redirect_uri(
            RedirectUrl::new(self.redirect_uri.clone())
                .map_err(|e| AuthError::InvalidUrl(e.to_string()))?,
        );

        Ok(client)
    }

    async fn fetch_username(&self, access_token: &str) -> Result<String, AuthError> {
        let response = reqwest::Client::new()
            .get(&self.info_url)
            .header("Authorization", format!("Bearer {}", access_token))
            .send()
            .await
            .map_err(|e| AuthError::NetworkError(e.to_string()))?
            .json::<serde_json::Value>()
            .await
            .map_err(|e| AuthError::NetworkError(e.to_string()))?;

        response["data"]["username"]
            .as_str()
            .ok_or_else(|| AuthError::NetworkError("Missing username field".to_string()))
            .map(String::from)
    }

    fn save_token_data(
        &mut self,
        username: &str,
        token: &StandardTokenResponse<EmptyExtraTokenFields, BasicTokenType>,
    ) -> Result<(), TokenStoreError> {
        let access_token = token.access_token().secret().to_string();
        let refresh_token = token
            .refresh_token()
            .ok_or(TokenStoreError::RefreshTokenNotFound)?
            .secret()
            .to_string();

        let expiration_time = token
            .expires_in()
            .unwrap_or(Duration::from_secs(7200))
            .as_secs()
            + SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_secs();

        self.token_store
            .save_oauth2_token(username, &access_token, &refresh_token, expiration_time)
    }
}

struct OAuth2Scopes {
    read_scopes: Vec<&'static str>,
    write_scopes: Vec<&'static str>,
    other_scopes: Vec<&'static str>,
}

impl OAuth2Scopes {
    fn all() -> Vec<Scope> {
        let scopes = Self {
            read_scopes: vec![
                "block.read",
                "bookmark.read",
                "dm.read",
                "follows.read",
                "like.read",
                "list.read",
                "mute.read",
                "space.read",
                "tweet.read",
                "timeline.read",
                "users.read",
            ],
            write_scopes: vec![
                "block.write",
                "bookmark.write",
                "dm.write",
                "follows.write",
                "like.write",
                "list.write",
                "mute.write",
                "tweet.write",
                "tweet.moderate.write",
                "timeline.write",
                "media.write",
            ],
            other_scopes: vec!["offline.access"],
        };
        scopes.to_oauth_scopes()
    }

    fn to_oauth_scopes(self) -> Vec<Scope> {
        self.read_scopes
            .into_iter()
            .chain(self.write_scopes)
            .chain(self.other_scopes)
            .map(|s| Scope::new(s.to_string()))
            .collect()
    }
}

// OAuth 1.0 helper functions

/// Generate OAuth 1.0 signature
fn generate_signature(
    method: &str,
    url: &str,
    params: &BTreeMap<String, String>,
    consumer_secret: &str,
    token_secret: &str,
) -> Result<String, AuthError> {
    let parameter_string = params
        .iter()
        .map(|(k, v)| format!("{}={}", encode(k), encode(v)))
        .collect::<Vec<_>>()
        .join("&");

    let base_string = format!(
        "{}&{}&{}",
        encode(method),
        encode(url),
        encode(&parameter_string)
    );

    let signing_key = format!("{}&{}", encode(consumer_secret), encode(token_secret));

    let mut mac = Hmac::<Sha1>::new_from_slice(signing_key.as_bytes())
        .map_err(|e| AuthError::InvalidToken(e.to_string()))?;

    mac.update(base_string.as_bytes());
    let result = mac.finalize();
    Ok(STANDARD.encode(result.into_bytes()))
}

/// Generate a random nonce
fn generate_nonce() -> String {
    let mut rng = rand::thread_rng();
    let nonce: u64 = rng.gen();
    format!("{:x}", nonce)
}

/// Generate a timestamp
fn generate_timestamp() -> Result<String, AuthError> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs().to_string())
        .map_err(|e| AuthError::InvalidToken(e.to_string()))
}

/// Encode a value for OAuth
fn encode(value: &str) -> String {
    utf8_percent_encode(value, NON_ALPHANUMERIC).to_string()
}
