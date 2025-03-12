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
    
    // IPv4 binding
    let addr_v4: std::net::SocketAddr = format!("127.0.0.1:{}", port).parse().unwrap();
    let v4_listener = match tokio::net::TcpListener::bind(addr_v4).await {
        Ok(listener) => {
            Some(listener)
        },
        Err(_) => {
            None
        }
    };
    
    // IPv6 binding
    let addr_v6: std::net::SocketAddr = format!("[::1]:{}", port).parse().unwrap();
    let v6_listener = match tokio::net::TcpListener::bind(addr_v6).await {
        Ok(listener) => {
            Some(listener)
        },
        Err(_) => {
            None
        }
    };
    
    // Ensure at least one listener was created
    if v4_listener.is_none() && v6_listener.is_none() {
        return Err(format!("Failed to bind to any address on port {}", port));
    }
    
    // Create servers for each listener
    let mut server_handles = vec![];
    
    if let Some(listener) = v4_listener {
        let app_clone = app.clone();
        let handle = tokio::spawn(async move {
            match axum::serve(listener, app_clone).await {
                Ok(_) => println!("IPv4 server shutdown gracefully"),
                Err(e) => println!("IPv4 server error: {}", e),
            }
        });
        server_handles.push(handle);
    }
    
    if let Some(listener) = v6_listener {
        let handle = tokio::spawn(async move {
            match axum::serve(listener, app).await {
                Ok(_) => println!("IPv6 server shutdown gracefully"),
                Err(e) => println!("IPv6 server error: {}", e),
            }
        });
        server_handles.push(handle);
    }

    let code = rx
        .await
        .map_err(|_| "Failed to receive authorization code".to_string())?;

    for handle in server_handles {
        handle.abort();
    }

    Ok(code)
}
