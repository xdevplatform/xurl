use clap::Parser;
mod api;
mod auth;
mod cli;
mod config;
mod error;

use api::client::ApiClient;
use auth::{token_store::TokenStore, Auth};
use cli::{AuthCommands, Cli, Commands, OAuth1Commands};
use config::Config;
use error::Error;

#[tokio::main]
async fn main() -> Result<(), Error> {
    let cli = Cli::parse();

    let config = Config::from_env()?;
    let mut auth = Auth::new(config.clone())?;

    // Handle auth subcommands
    if let Some(Commands::Auth { command }) = cli.command {
        match command {
            AuthCommands::OAuth2 => {
                auth.oauth2(None).await?;
                println!("OAuth2 authentication successful!");
            }

            AuthCommands::OAuth1 { command } => match command {
                OAuth1Commands::Set {
                    consumer_key,
                    consumer_secret,
                    access_token,
                    token_secret,
                } => {
                    let mut store = TokenStore::new();
                    store.save_oauth1_tokens(
                        access_token,
                        token_secret,
                        consumer_key,
                        consumer_secret,
                    )?;
                    println!("OAuth1 credentials saved successfully!");
                }
            },

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
            } => {
                let mut store = TokenStore::new();
                if all {
                    store.clear_all()?;
                    println!("All authentication cleared!");
                } else if oauth1 {
                    store.clear_oauth1_tokens()?;
                    println!("OAuth1 tokens cleared!");
                } else if let Some(username) = oauth2_username {
                    store.clear_oauth2_token(&username)?;
                    println!("OAuth2 token cleared for {}!", username);
                }
            }
        }
        return Ok(());
    }

    if let Some(url) = cli.url {
        let client = ApiClient::new(config).with_auth(auth);

        // Make the request
        let response = client
            .send_request(
                cli.method.as_deref().unwrap_or("GET"),
                &url,
                &cli.headers,
                cli.data.as_deref(),
                cli.auth.as_deref(),
                cli.username.as_deref(),
            )
            .await?;

        // Pretty print the response
        println!("{}", serde_json::to_string_pretty(&response)?);

        return Ok(());
    }
    Ok(())
}
