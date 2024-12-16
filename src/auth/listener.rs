use axum::{extract::Query, routing::get, Router};
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use tokio::sync::oneshot;

pub async fn listen_for_code(port: u16) -> Result<String, String> {
    let (tx, rx) = oneshot::channel();
    let tx = Arc::new(Mutex::new(Some(tx)));

    let app = Router::new().route(
        "/callback",
        get(move |Query(params): Query<HashMap<String, String>>| {
            let tx = tx.clone();
            async move {
                if let Some(code) = params.get("code") {
                    if let Some(tx) = tx.lock().unwrap().take() {
                        let _ = tx.send(code.clone());
                    }
                }
                "Authorization successful! You can close this window.".to_string()
            }
        }),
    );

    let addr: std::net::SocketAddr = format!("127.0.0.1:{}", port).parse().unwrap();
    let listener = tokio::net::TcpListener::bind(addr).await.unwrap();

    let server_handle = tokio::spawn(async move { axum::serve(listener, app).await.unwrap() });

    let code = rx
        .await
        .map_err(|_| "Failed to receive authorization code".to_string())?;

    server_handle.abort();

    Ok(code)
}
