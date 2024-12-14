use crate::api::client::ApiClient;
use crate::auth::oauth::OAuth;
use crate::config::environment::Config;
use crate::error::Error;
use clap::Parser;

#[derive(Parser)]
#[command(name = "xurl")]
#[command(about = "curl-like tool for X API", long_about = None)]
pub struct Cli {
    /// URL to send request to
    #[arg(required = true)]
    url: String,

    /// HTTP method
    #[arg(short = 'X', long, default_value = "GET")]
    method: String,

    /// Username for authentication
    #[arg(short, long)]
    username: Option<String>,

    /// Request headers
    #[arg(short = 'H', long)]
    headers: Vec<String>,

    /// Request data
    #[arg(short, long)]
    data: Option<String>,
}

pub async fn execute(args: Cli) -> Result<(), Error> {
    let config = match Config::from_env() {
        Ok(config) => config,
        Err(e) => {
            match e {
                Error::MissingEnvVar(var) => eprintln!("Missing environment variable: {}", var),
                _ => eprintln!("Error: {}", e),
            }
            return Err(e);
        }
    };

    let mut oauth = match OAuth::new(config) {
        Ok(oauth) => oauth,
        Err(e) => {
            eprintln!("Error: {}", e);
            return Err(Error::OAuthError(e));
        }
    };

    let token = match oauth.get_token(args.username).await {
        Ok(token) => token,
        Err(e) => {
            eprintln!("Error: {}", e);
            return Err(Error::OAuthError(e));
        }
    };

    let client = ApiClient::new();

    let path = if let Some(path) = args.url.strip_prefix("https://api.x.com") {
        path.to_string()
    } else {
        args.url
    };

    let response = match client
        .send_request(
            &args.method,
            &path,
            &args.headers,
            args.data.as_deref(),
            &token,
        )
        .await {
        Ok(response) => response,
        Err(e) => {
            match &e {
                Error::HttpError(e) => eprintln!("HTTP error: {}", e),
                Error::ApiError(e) => eprintln!("API error: {}", serde_json::to_string_pretty(e)?),
                Error::JsonError(e) => eprintln!("JSON deserialization error: {}", e),
                _ => eprintln!("Error: {}", e),
            }
            return Err(e);
        }
    };

    println!("{}", serde_json::to_string_pretty(&response)?);
    Ok(())
}
