use crate::error::Error;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fs;
use std::path::PathBuf;
#[derive(Debug, Serialize, Deserialize)]
pub struct TokenStore {
    tokens: HashMap<String, String>,
    file_path: PathBuf,
}

impl TokenStore {
    pub fn new() -> Self {
        let file_path = home::home_dir()
            .unwrap_or_else(|| PathBuf::from("."))
            .join(".xurl");

        // Load existing tokens or create new store
        let tokens = if file_path.exists() {
            let content = fs::read_to_string(&file_path).unwrap_or_default();
            serde_json::from_str(&content).unwrap_or_default()
        } else {
            HashMap::new()
        };

        Self { tokens, file_path }
    }

    pub fn save_token(&mut self, username: &str, token: &str) -> Result<(), Error> {
        self.tokens.insert(username.to_string(), token.to_string());
        let content = serde_json::to_string(&self.tokens)?;
        fs::write(&self.file_path, content)?;
        Ok(())
    }

    pub fn get_token(&self, username: &str) -> Option<String> {
        self.tokens.get(username).cloned()
    }
}
