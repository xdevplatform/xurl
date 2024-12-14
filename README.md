
# xurl - A curl-like CLI Tool for X (Twitter) API
`xurl` is a command-line tool that simplifies making authenticated requests to the X (formerly Twitter) API. It handles OAuth 2.0 authentication automatically and provides a curl-like interface for API interactions.

## Features
- OAuth 2.0 authentication with PKCE
- Token persistence for multiple users
- Automatic token refresh
- Support for all HTTP methods
- JSON request/response handling
- Custom header support


## Configuration
Before using xurl, you need to set up the following environment variables:
```bash
export CLIENT_ID="your_client_id"
export CLIENT_SECRET="your_client_secret"
```

Optional environment variables (to override defaults):
```bash
export REDIRECT_URI="<your_redirect_uri>"
export AUTH_URL="<your_auth_url>"
export TOKEN_URL="<your_token_url>"
```

## Usage
Basic usage:
```bash
xurl '/2/users/me'
```

To cache authentication for a specific user, use the `-u` option:
```bash
xurl -u santiagomedr '/2/users/me'
```


POST request with data:
```bash
xurl -X POST -d '{"text":"Hello, World!"}' '/2/tweets'
```

Adding custom headers:
```bash
xurl -H "Content-Type: application/json" '/2/users/me'
```

## Authentication Flow
The tool implements OAuth 2.0 with PKCE for secure authentication. When you make your first request:
1. A browser window opens for X authentication
2. After authorizing, you'll be redirected back to the local callback server
3. The token is automatically saved for future use


## Code Structure
- `src/auth/`: OAuth implementation and token management
- `src/api/`: API client and request handling
- `src/cli/`: Command-line interface and argument parsing
- `src/config/`: Configuration management
- `src/errors/`: Error handling


## Error Handling
- OAuth authentication errors
- Network and HTTP errors
- API response errors
- Configuration errors


## Testing
To run tests:
```bash
cargo test
```


## Contributing
Contributions are welcome!

1. Fork the repository
2. Create your feature branch
3. Commit your changes
4. Push to the branch
5. Create a new Pull Request

## Dependencies
Key dependencies include:
- `oauth2`: OAuth 2.0 authentication
- `reqwest`: HTTP client
- `tokio`: Async runtime
- `axum`: Web server for OAuth callback
- `clap`: Command line argument parsing
- `serde`: Serialization/deserialization

## License
This project is open-sourced under the MIT License - see the LICENSE file for details.
