use crate::error::Error;
use std::env;

pub struct Config {
    pub client_id: String,
    pub client_secret: String,
    pub redirect_uri: String,
    pub auth_url: String,
    pub token_url: String,
}

impl Config {
    pub fn from_env() -> Result<Self, Error> {
        let client_id = env::var("CLIENT_ID").map_err(|_| Error::MissingEnvVar("CLIENT_ID"))?;

        let client_secret =
            env::var("CLIENT_SECRET").map_err(|_| Error::MissingEnvVar("CLIENT_SECRET"))?;

        let redirect_uri =
            env::var("REDIRECT_URI").unwrap_or_else(|_| "http://localhost:8080/callback".to_string());
        let auth_url = env::var("AUTH_URL")
            .unwrap_or_else(|_| "https://x.com/i/oauth2/authorize".to_string());
        let token_url = env::var("TOKEN_URL")
            .unwrap_or_else(|_| "https://api.x.com/2/oauth2/token".to_string());
        Ok(Self {
            client_id,
            client_secret,
            redirect_uri,
            auth_url,
            token_url,
        })
    }
}
