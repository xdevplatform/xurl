use crate::auth::AuthError;
use thiserror::Error;

#[derive(Error, Debug)]
pub enum Error {
    #[error("HTTP error: {0}")]
    HttpError(#[from] reqwest::Error),

    #[error("IO error: {0}")]
    IoError(#[from] std::io::Error),

    #[error("Invalid HTTP method: {0}")]
    InvalidMethod(#[from] http::method::InvalidMethod),

    #[error("API error: {0}")]
    ApiError(serde_json::Value),

    #[error("JSON error: {0}")]
    JsonError(#[from] serde_json::Error),

    #[error("Auth error: {0}")]
    AuthError(#[from] AuthError),
}
