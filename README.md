# xurl - A curl-like CLI Tool for the X API

A command-line tool for interacting with the X (formerly Twitter) API, supporting both OAuth 1.0a and OAuth 2.0 authentication.

## Features

- OAuth 2.0 PKCE flow authentication
- OAuth 1.0a authentication
- Multiple OAuth 2.0 account support
- Persistent token storage
- HTTP request customization (headers, methods, body)

## Installation
```bash
curl -fsSL https://raw.githubusercontent.com/xdevplatform/xurl/main/install.sh | sudo bash
```

## Configuration

Add the following to your environment variables if you want to use OAuth 2.0:

```env
export CLIENT_ID=your_client_id
export CLIENT_SECRET=your_client_secret
```

Optional environment variables:
- `REDIRECT_URI` (default: http://localhost:8080/callback)
- `AUTH_URL` (default: https://x.com/i/oauth2/authorize)
- `TOKEN_URL` (default: https://api.x.com/2/oauth2/token)
- `API_BASE_URL` (default: https://api.x.com)

## Usage

### Authentication

OAuth 2.0 authentication:
```bash
xurl auth oauth2
```

OAuth 1.0a authentication:
```bash
xurl auth oauth1 --consumer-key KEY --consumer-secret SECRET --access-token TOKEN --token-secret SECRET
```

View authentication status:
```bash
xurl auth status
```

Clear authentication:
```bash
xurl auth clear --all              # Clear all tokens
xurl auth clear --oauth1           # Clear OAuth 1.0a tokens
xurl auth clear --oauth2-username USERNAME  # Clear specific OAuth 2.0 token
```

### Making Requests

Basic GET request:
```bash
xurl /2/users/me
```

Custom HTTP method:
```bash
xurl -X POST /2/tweets -d '{"text":"Hello world!"}'
```

Add headers:
```bash
xurl -H "Content-Type: application/json" /2/tweets
```

Specify authentication type:
```bash
xurl --auth oauth2 /2/users/me
xurl --auth oauth1 /2/tweets
```

Use specific OAuth 2.0 account:
```bash
xurl --username johndoe /2/users/me
```

## Token Storage

Tokens are stored securely in `~/.xurl` in your home directory.

## Development

The project uses the following structure:
- `src/main.rs`: Entry point and command handling
- `src/auth/`: Authentication implementation
- `src/api/`: API client implementation
- `src/cli/`: Command-line interface definitions
- `src/config/`: Configuration management
- `src/error/`: Error types and handling

## Testing

Run the test suite:
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

## License
This project is open-sourced under the MIT License - see the LICENSE file for details.
