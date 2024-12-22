use clap::Parser;
mod api;
mod auth;
mod cli;
mod config;
mod error;

use api::client::ApiClient;
use auth::{token_store::TokenStore, Auth};
use cli::{AuthCommands, Cli, Commands};
use config::Config;
use error::Error;

#[tokio::main]
async fn main() -> Result<(), Error> {
    let cli = Cli::parse();

    let config = Config::from_env();
    let mut auth = Auth::new(config.clone());

    // Handle auth subcommands
    if let Some(Commands::Auth { command }) = cli.command {
        match command {
            AuthCommands::App { bearer_token } => {
                auth.get_token_store()
                    .save_bearer_token(&bearer_token)
                    .map_err(|e| Error::AuthError(auth::AuthError::TokenStoreError(e)))?;
                println!("App authentication successful!");
            }

            AuthCommands::OAuth2 => {
                auth.oauth2(None).await?;
                println!("OAuth2 authentication successful!");
            }

            AuthCommands::OAuth1 {
                consumer_key,
                consumer_secret,
                access_token,
                token_secret,
            } => {
                auth.get_token_store()
                    .save_oauth1_tokens(access_token, token_secret, consumer_key, consumer_secret)
                    .map_err(|e| Error::AuthError(auth::AuthError::TokenStoreError(e)))?;
                println!("OAuth1 credentials saved successfully!");
            }

            AuthCommands::Status => {
                let store = TokenStore::new();
                println!("OAuth2 Accounts:");
                for username in store.get_oauth2_usernames() {
                    println!("- {}", username);
                }
                println!(
                    "OAuth1: {}",
                    if store.has_oauth1_tokens() {
                        "Configured"
                    } else {
                        "Not configured"
                    }
                );
            }

            AuthCommands::Clear {
                all,
                oauth1,
                oauth2_username,
                bearer,
            } => {
                if all {
                    auth.get_token_store()
                        .clear_all()
                        .map_err(|e| Error::AuthError(auth::AuthError::TokenStoreError(e)))?;
                    println!("All authentication cleared!");
                } else if oauth1 {
                    auth.get_token_store()
                        .clear_oauth1_tokens()
                        .map_err(|e| Error::AuthError(auth::AuthError::TokenStoreError(e)))?;
                    println!("OAuth1 tokens cleared!");
                } else if let Some(username) = oauth2_username {
                    auth.get_token_store()
                        .clear_oauth2_token(&username)
                        .map_err(|e| Error::AuthError(auth::AuthError::TokenStoreError(e)))?;
                    println!("OAuth2 token cleared for {}!", username);
                } else if bearer {
                    auth.get_token_store()
                        .clear_bearer_token()
                        .map_err(|e| Error::AuthError(auth::AuthError::TokenStoreError(e)))?;
                    println!("Bearer token cleared!");
                } else {
                    println!("No authentication cleared! Use --all to clear all authentication.");
                    std::process::exit(1);
                }
            }
        }
        return Ok(());
    }

    if let Some(url) = cli.url {
        let client = ApiClient::new(config).with_auth(auth);

        // Make the request
        let response = match client
            .send_request(
                cli.method.as_deref().unwrap_or("GET"),
                &url,
                &cli.headers,
                cli.data.as_deref(),
                cli.auth.as_deref(),
                cli.username.as_deref(),
                cli.verbose,
            )
            .await
        {
            Ok(res) => res,
            Err(e) => match e {
                Error::ApiError(e) => {
                    println!("{}", serde_json::to_string_pretty(&e)?);
                    std::process::exit(1)
                }
                Error::HttpError(e) => {
                    println!("{}", e);
                    std::process::exit(1)
                }
                _ => {
                    println!("{}", e);
                    std::process::exit(1)
                }
            },
        };

        // Pretty print the response
        println!("{}", serde_json::to_string_pretty(&response)?);

        return Ok(());
    }

    println!("No URL provided\n");
    println!("Usage: xurl [OPTIONS] [URL] [COMMAND]");
    println!("Try 'xurl --help' for more information.");
    std::process::exit(1);
}
