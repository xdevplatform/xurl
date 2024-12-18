use std::env;

#[derive(Clone)]
pub struct Config {
    // OAuth2 client tokens
    pub client_id: String,
    pub client_secret: String,
    // OAuth2 PKCE flow urls
    pub redirect_uri: String,
    pub auth_url: String,
    pub token_url: String,
    // API base url
    pub api_base_url: String,
    // API user info url
    pub info_url: String,
}

impl Config {
    pub fn from_env() -> Self {
        let client_id = env::var("CLIENT_ID").unwrap_or_default();
        let client_secret = env::var("CLIENT_SECRET").unwrap_or_default();
        let redirect_uri = env::var("REDIRECT_URI")
            .unwrap_or_else(|_| "http://localhost:8080/callback".to_string());
        let auth_url =
            env::var("AUTH_URL").unwrap_or_else(|_| "https://x.com/i/oauth2/authorize".to_string());
        let token_url = env::var("TOKEN_URL")
            .unwrap_or_else(|_| "https://api.x.com/2/oauth2/token".to_string());
        let api_base_url =
            env::var("API_BASE_URL").unwrap_or_else(|_| "https://api.x.com".to_string());
        let info_url =
            env::var("INFO_URL").unwrap_or_else(|_| format!("{}/2/users/me", api_base_url));
        Self {
            client_id,
            client_secret,
            redirect_uri,
            auth_url,
            token_url,
            api_base_url,
            info_url,
        }
    }
}
