use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fs;
use std::path::PathBuf;

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct OAuth1Token {
    pub(crate) access_token: String,
    pub(crate) token_secret: String,
    pub(crate) consumer_key: String,
    pub(crate) consumer_secret: String,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct OAuth2Token {
    pub(crate) access_token: String,
    pub(crate) refresh_token: String,
    pub(crate) expiration_time: u64,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub enum Token {
    #[serde(rename = "bearer")]
    Bearer(String), // Bearer token
    #[serde(rename = "oauth2")]
    OAuth2(OAuth2Token), // access_token
    #[serde(rename = "oauth1")]
    OAuth1(OAuth1Token),
}

#[derive(thiserror::Error, Debug, Serialize, Deserialize)]
pub enum TokenStoreError {
    #[error("JSON serialization error")]
    JSONSerializationError,
    #[error("JSON deserialization error")]
    JSONDeserializationError,
    #[error("IO error")]
    IOError,
    #[error("Refresh token not found")]
    RefreshTokenNotFound,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct TokenStore {
    oauth2_tokens: HashMap<String, Token>, // username -> access_token
    oauth1_tokens: Option<Token>,          // Only one set of OAuth1 credentials
    bearer_token: Option<Token>,           // Bearer token
    file_path: PathBuf,
}

impl TokenStore {
    pub fn new() -> Self {
        let file_path = home::home_dir()
            .unwrap_or_else(|| PathBuf::from("."))
            .join(".xurl");

        Self::from_file_path(file_path)
    }

    pub fn from_file_path(file_path: PathBuf) -> Self {
        let store = if file_path.exists() {
            let content = fs::read_to_string(&file_path).unwrap_or_default();
            serde_json::from_str(&content).unwrap_or_else(|_| TokenStore {
                oauth2_tokens: HashMap::new(),
                oauth1_tokens: None,
                bearer_token: None,
                file_path: file_path.clone(),
            })
        } else {
            TokenStore {
                oauth2_tokens: HashMap::new(),
                oauth1_tokens: None,
                bearer_token: None,
                file_path,
            }
        };

        store
    }

    pub fn save_bearer_token(&mut self, token: &str) -> Result<(), TokenStoreError> {
        self.bearer_token = Some(Token::Bearer(token.to_string()));
        self.save_to_file()
    }

    pub fn save_oauth2_token(
        &mut self,
        username: &str,
        token: &str,
        refresh_token: &str,
        expiration_time: u64,
    ) -> Result<(), TokenStoreError> {
        self.oauth2_tokens.insert(
            username.to_string(),
            Token::OAuth2(OAuth2Token {
                access_token: token.to_string(),
                refresh_token: refresh_token.to_string(),
                expiration_time,
            }),
        );
        self.save_to_file()
    }

    pub fn save_oauth1_tokens(
        &mut self,
        access_token: String,
        token_secret: String,
        consumer_key: String,
        consumer_secret: String,
    ) -> Result<(), TokenStoreError> {
        self.oauth1_tokens = Some(Token::OAuth1(OAuth1Token {
            access_token,
            token_secret,
            consumer_key,
            consumer_secret,
        }));
        self.save_to_file()
    }

    pub fn get_oauth2_token(&self, username: &str) -> Option<Token> {
        self.oauth2_tokens.get(username).cloned()
    }

    pub fn get_first_oauth2_token(&self) -> Option<Token> {
        self.oauth2_tokens.values().next().cloned()
    }

    pub fn get_oauth1_tokens(&self) -> Option<Token> {
        self.oauth1_tokens.clone()
    }

    pub fn get_bearer_token(&self) -> Option<Token> {
        self.bearer_token.clone()
    }

    pub fn clear_oauth2_token(&mut self, username: &str) -> Result<(), TokenStoreError> {
        self.oauth2_tokens.remove(username);
        self.save_to_file()
    }

    pub fn clear_oauth1_tokens(&mut self) -> Result<(), TokenStoreError> {
        self.oauth1_tokens = None;
        self.save_to_file()
    }

    pub fn clear_all(&mut self) -> Result<(), TokenStoreError> {
        self.oauth2_tokens.clear();
        self.oauth1_tokens = None;
        self.bearer_token = None;
        self.save_to_file()
    }

    pub fn clear_bearer_token(&mut self) -> Result<(), TokenStoreError> {
        self.bearer_token = None;
        self.save_to_file()
    }

    pub fn get_oauth2_usernames(&self) -> Vec<String> {
        self.oauth2_tokens.keys().cloned().collect()
    }

    pub fn has_oauth1_tokens(&self) -> bool {
        self.oauth1_tokens.is_some()
    }

    fn save_to_file(&self) -> Result<(), TokenStoreError> {
        let content =
            serde_json::to_string(&self).map_err(|_| TokenStoreError::JSONSerializationError)?;
        fs::write(&self.file_path, content).map_err(|_| TokenStoreError::IOError)?;
        Ok(())
    }
}
