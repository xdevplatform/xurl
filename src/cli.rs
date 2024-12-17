use clap::{command, Parser, Subcommand};

#[derive(Parser)]
#[command(
    name = "xurl",
    about = "Auth enabled curl-like interface for the X API",
    version,
    author,
    propagate_version(true),
    arg_required_else_help(true)
)]
pub struct Cli {
    /// Command to execute
    #[command(subcommand)]
    pub command: Option<Commands>,

    /// HTTP method (GET by default)
    #[arg(short = 'X', long)]
    pub method: Option<String>,

    /// URL to request
    pub url: Option<String>,

    /// Request headers
    #[arg(short = 'H', long = "header")]
    pub headers: Vec<String>,

    /// Request body data
    #[arg(short = 'd', long = "data")]
    pub data: Option<String>,

    /// Authentication type (oauth1 or oauth2)
    #[arg(long = "auth")]
    pub auth: Option<String>,

    /// Username for OAuth2 authentication
    #[arg(short, long)]
    pub username: Option<String>,
}

#[derive(Subcommand)]
pub enum Commands {
    /// Authentication management
    Auth {
        #[command(subcommand)]
        command: AuthCommands,
    },
}

#[derive(Subcommand)]
pub enum AuthCommands {
    /// Configure app-auth
    #[command(name = "app")]
    App {
        #[arg(long)]
        bearer_token: String,
    },

    /// Configure OAuth2 authentication
    #[command(name = "oauth2")]
    OAuth2,

    /// Configure OAuth1 authentication
    #[command(name = "oauth1")]
    OAuth1 {
        #[arg(long)]
        consumer_key: String,
        #[arg(long)]
        consumer_secret: String,
        #[arg(long)]
        access_token: String,
        #[arg(long)]
        token_secret: String,
    },

    /// Show authentication status
    Status,

    /// Clear authentication tokens
    Clear {
        #[arg(long)]
        all: bool,
        #[arg(long)]
        oauth1: bool,
        #[arg(long)]
        oauth2_username: Option<String>,
        #[arg(long)]
        bearer: bool,
    },
}
