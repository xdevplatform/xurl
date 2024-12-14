use crate::auth::token_store::TokenStore;
use crate::config::environment::Config;
use crate::auth::listener::OAuthServer;

use oauth2::basic::BasicClient;
use oauth2::reqwest::async_http_client;
use oauth2::{
    AuthUrl, AuthorizationCode, ClientId, ClientSecret, CsrfToken, PkceCodeChallenge, RedirectUrl,
    Scope, TokenResponse, TokenUrl,
};

#[derive(Debug, thiserror::Error)]
pub enum OAuthError {
    #[error("Invalid URL: {0}")]
    InvalidUrl(String),
    #[error("Invalid code: {0}")]
    InvalidCode(String),
    #[error("Invalid token: {0}")]
    InvalidToken(String),
    #[error("Token store error: {0}")]
    TokenStoreError(String),
    #[error("Authorization error: {0}")]
    AuthorizationError(String),
    #[error("Network error: {0}")]
    NetworkError(String),
    #[error("IO error: {0}")]
    IoError(#[from] std::io::Error),
}

pub struct OAuth {
    client: BasicClient,
    token_store: TokenStore,
}

impl OAuth {
    pub fn new(config: Config) -> Result<Self, OAuthError> {
        let client = BasicClient::new(
            ClientId::new(config.client_id),
            Some(ClientSecret::new(config.client_secret)),
            AuthUrl::new(config.auth_url).map_err(|e| OAuthError::InvalidUrl(e.to_string()))?,
            Some(TokenUrl::new(config.token_url).map_err(|e| OAuthError::InvalidUrl(e.to_string()))?),
        )
        .set_redirect_uri(RedirectUrl::new(config.redirect_uri).map_err(|e| OAuthError::InvalidUrl(e.to_string()))?);

        Ok(Self {
            client,
            token_store: TokenStore::new(),
        })
    }

    pub async fn get_token(&mut self, username: Option<String>) -> Result<String, OAuthError> {
        if let Some(ref username) = username {
            if let Some(token) = self.token_store.get_token(username) {
                return Ok(token);
            }
        }

        // Generate PKCE values
        let (code_challenge, code_verifier) = PkceCodeChallenge::new_random_sha256();

        // Generate authorization URL with PKCE
        let (auth_url, _csrf_token) = self
            .client
            .authorize_url(CsrfToken::new_random)
            .add_scope(Scope::new("tweet.read".to_string()))
            .add_scope(Scope::new("users.read".to_string()))
            .add_scope(Scope::new("offline.access".to_string()))
            .set_pkce_challenge(code_challenge)
            .url();

        // Start OAuth server
        let server = OAuthServer::new(8080);
        
        // Open browser
        webbrowser::open(auth_url.as_str())
            .map_err(|e| OAuthError::IoError(std::io::Error::new(std::io::ErrorKind::Other, e)))?;

        // Wait for the callback
        let code = server.listen_for_code().await
            .map_err(|e| OAuthError::InvalidCode(e))?;

        // Exchange code for token
        let token = self
            .client
            .exchange_code(AuthorizationCode::new(code))
            .set_pkce_verifier(code_verifier)
            .request_async(async_http_client)
            .await
            .map_err(|e| match e {
                oauth2::RequestTokenError::ServerResponse(e) => OAuthError::AuthorizationError(e.to_string()),
                oauth2::RequestTokenError::Request(e) => OAuthError::NetworkError(e.to_string()),
                _ => OAuthError::InvalidToken(e.to_string()),
            })?;

        if let Some(username) = username {
            self.token_store
                .save_token(&username, token.access_token().secret())
                .map_err(|e| OAuthError::TokenStoreError(e.to_string()))?;
        }

        Ok(token.access_token().secret().to_string())
    }
}