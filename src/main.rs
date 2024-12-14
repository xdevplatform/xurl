use clap::Parser;
mod api;
mod auth;
mod cli;
mod config;
mod error;

#[tokio::main]
async fn main() {
    let args = cli::Cli::parse();
    match cli::execute(args).await {
        Ok(_) => (),
        Err(_) => std::process::exit(1),
    }
}
