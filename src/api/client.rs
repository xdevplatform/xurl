use crate::error::Error;
use reqwest::{Client, Method};
use serde_json::Value;

const API_BASE_URL: &str = "https://api.x.com";

pub struct ApiClient {
    url: String,
    client: Client,
}

impl ApiClient {
    pub fn new() -> Self {
        Self {
            client: Client::new(),
            url: API_BASE_URL.to_string(),
        }
    }

    #[allow(dead_code)]
    pub fn with_url(mut self, url: String) -> Self {
        self.url = url;
        self
    }

    pub async fn send_request(
        &self,
        method: &str,
        endpoint: &str,
        headers: &[String],
        data: Option<&str>,
        token: &str,
    ) -> Result<serde_json::Value, Error> {
        let endpoint = if !endpoint.starts_with('/') {
            format!("/{}", endpoint)
        } else {
            endpoint.to_string()
        };

        let url = format!("{}{}", self.url, endpoint);
        let method = Method::from_bytes(method.to_uppercase().as_bytes())?;

        let mut request_builder = self
            .client
            .request(method, &url)
            .header("Authorization", format!("Bearer {}", token))
            .header("User-Agent", "xurl/1.0");

        for header in headers {
            if let Some((key, value)) = header.split_once(':') {
                request_builder = request_builder.header(key.trim(), value.trim());
            }
        }

        if let Some(data) = data {
            if let Ok(json) = serde_json::from_str::<Value>(data) {
                request_builder = request_builder
                    .header("Content-Type", "application/json")
                    .json(&json);
            } else {
                request_builder = request_builder
                    .header("Content-Type", "text/plain")
                    .body(data.to_string());
            }
        }

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

    #[tokio::test]
    async fn test_successful_get_request() {
        let mut server = Server::new_async().await;
        let url = server.url();
        let mock = server
            .mock("GET", "/2/users/me")
            .with_status(200)
            .with_header("content-type", "application/json")
            .with_body(r#"{"data":{"id":"123","name":"test"}}"#)
            .create_async()
            .await;

        let client = ApiClient::new().with_url(url);
        let result = client
            .send_request("GET", "/2/users/me", &[], None, "test_token")
            .await;

        assert!(result.is_ok());
        mock.assert_async().await;
    }

    #[tokio::test]
    async fn test_error_response() {
        let mut server = Server::new_async().await;
        let url = server.url();
        let mock = server
            .mock("GET", "/2/users/me")
            .with_status(401)
            .with_body(r#"{"error":"Unauthorized"}"#)
            .create_async()
            .await;

        let client = ApiClient::new().with_url(url);
        let result = client
            .send_request("GET", "/2/users/me", &[], None, "invalid_token")
            .await;

        assert!(matches!(result, Err(Error::ApiError(_))));
        mock.assert_async().await;
    }
}
