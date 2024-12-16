use crate::auth::token_store::Token;
use crate::auth::{Auth, AuthError};
use crate::config::Config;
use crate::error::Error;
use reqwest::RequestBuilder;
use reqwest::{Client, Method};
use serde_json::Value;
use std::cell::RefCell;

pub struct ApiClient {
    url: String,
    client: Client,
    auth: Option<RefCell<Auth>>,
}

impl ApiClient {
    pub fn new(config: Config) -> Self {
        Self {
            client: Client::new(),
            url: config.api_base_url,
            auth: None,
        }
    }

    #[allow(dead_code)]
    pub fn with_url(mut self, url: String) -> Self {
        self.url = url;
        self
    }

    pub fn with_auth(mut self, auth: Auth) -> Self {
        self.auth = Some(RefCell::new(auth));
        self
    }

    async fn get_oauth2_token(
        &self,
        auth: &RefCell<Auth>,
        username: Option<&str>,
    ) -> Result<String, Error> {
        match username {
            Some(username) => {
                let token = auth.borrow_mut().oauth2(Some(username)).await?;
                Ok(format!("Bearer {}", token))
            }
            None => {
                if let Some(token) = auth.borrow_mut().oauth2_token() {
                    match token {
                        Token::OAuth2(token) => Ok(format!("Bearer {}", token)),
                        _ => Err(Error::AuthError(AuthError::TokenNotFound)),
                    }
                } else {
                    let token = auth.borrow_mut().oauth2(None).await?;
                    Ok(format!("Bearer {}", token))
                }
            }
        }
    }

    async fn get_auth_header(
        &self,
        method: &str,
        url: &str,
        auth_type: Option<&str>,
        username: Option<&str>,
    ) -> Result<String, Error> {
        let auth = match &self.auth {
            Some(auth) => auth,
            None => return Ok("".to_string()),
        };

        match auth_type.as_deref() {
            Some("oauth2") => self.get_oauth2_token(auth, username).await,
            Some("oauth1") => Ok(auth.borrow().oauth1(method, url, None)?),
            None => {
                // Try OAuth2 first, then OAuth1, then fetch new OAuth2 token
                if let Some(token) = auth.borrow().oauth2_token() {
                    match token {
                        Token::OAuth2(token) => Ok(format!("Bearer {}", token)),
                        _ => Err(Error::AuthError(AuthError::TokenNotFound)),
                    }
                } else if let Ok(oauth1_header) = auth.borrow().oauth1(method, url, None) {
                    Ok(oauth1_header)
                } else {
                    let token = auth.borrow_mut().oauth2(None).await?;
                    Ok(format!("Bearer {}", token))
                }
            }
            Some(auth_type) => Err(Error::AuthError(AuthError::InvalidAuthType(format!(
                "Invalid auth type: {}",
                auth_type
            )))),
        }
    }

    pub async fn build_request(
        &self,
        method: &str,
        endpoint: &str,
        headers: &[String],
        data: Option<&str>,
        auth_type: Option<&str>,
        username: Option<&str>,
    ) -> Result<RequestBuilder, Error> {
        let endpoint = if !endpoint.starts_with('/') {
            format!("/{}", endpoint)
        } else {
            endpoint.to_string()
        };

        let url = format!("{}{}", self.url, endpoint);
        let method = Method::from_bytes(method.to_uppercase().as_bytes())?;

        let auth_header = self
            .get_auth_header(method.as_str(), &url, auth_type, username)
            .await?;

        let mut request_builder = self
            .client
            .request(method, &url)
            .header("Authorization", auth_header)
            .header("User-Agent", "xurl/1.0");

        for header in headers {
            if let Some((key, value)) = header.split_once(':') {
                request_builder = request_builder.header(key.trim(), value.trim());
            }
        }

        if let Some(data) = data {
            if let Ok(json) = serde_json::from_str::<Value>(&data) {
                request_builder = request_builder
                    .header("Content-Type", "application/json")
                    .json(&json);
            } else {
                request_builder = request_builder
                    .header("Content-Type", "text/plain")
                    .body(data.to_string());
            }
        }

        Ok(request_builder)
    }

    pub async fn send_request(
        &self,
        method: &str,
        endpoint: &str,
        headers: &[String],
        data: Option<&str>,
        auth_type: Option<&str>,
        username: Option<&str>,
    ) -> Result<serde_json::Value, Error> {
        let request_builder = self
            .build_request(method, endpoint, headers, data, auth_type, username)
            .await?;
        let response = request_builder.send().await?;

        let status = response.status();
        let body: Value = response.json().await?;

        if !status.is_success() {
            return Err(Error::ApiError(body));
        }

        Ok(body)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use mockito::Server;

    fn setup_env() {
        std::env::set_var("CLIENT_ID", "test");
        std::env::set_var("CLIENT_SECRET", "test");
    }

    #[tokio::test]
    async fn test_successful_get_request() {
        setup_env();
        let mut server = Server::new_async().await;
        let url = server.url();
        let mock = server
            .mock("GET", "/2/users/me")
            .with_status(200)
            .with_header("content-type", "application/json")
            .with_body(r#"{"data":{"id":"123","name":"test"}}"#)
            .create_async()
            .await;

        let config = Config::from_env().unwrap();
        let client = ApiClient::new(config.clone())
            .with_url(url)
            .with_auth(Auth::new(config).unwrap());
        let result = client
            .send_request("GET", "/2/users/me", &[], None, None, None)
            .await;

        assert!(result.is_ok());
        mock.assert_async().await;
    }

    #[tokio::test]
    async fn test_error_response() {
        setup_env();
        let mut server = Server::new_async().await;
        let url = server.url();
        let mock = server
            .mock("GET", "/2/users/me")
            .with_status(401)
            .with_body(r#"{"error":"Unauthorized"}"#)
            .create_async()
            .await;

        let config = Config::from_env().unwrap();
        let client = ApiClient::new(config.clone())
            .with_url(url)
            .with_auth(Auth::new(config).unwrap());
        let result = client
            .send_request("GET", "/2/users/me", &[], None, None, None)
            .await;

        assert!(matches!(result, Err(Error::ApiError(_))));
        mock.assert_async().await;
    }
}
