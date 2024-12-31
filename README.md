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

You must have a developer account and app to use this tool. 

App authentication:
```bash
xurl auth app --bearer-token BEARER_TOKEN
```

OAuth 2.0 authentication:
```bash
xurl auth oauth2
```

**Note:** For OAuth 2.0 authentication, you must specify the redirect URI in the [X API developer portal](https://developer.x.com/en/portal/dashboard).

1. Create an app at the [X API developer portal](https://developer.x.com/en/portal/dashboard).
2. Go to authentication settings and set the redirect URI to `http://localhost:8080/callback`.
![Setup](./assets/setup.png)
![Redirect URI](./assets/callback.png)
3. Set the client ID and secret in your environment variables.

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
xurl auth clear --bearer           # Clear bearer token
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
xurl --auth app /2/users/me
```

Use specific OAuth 2.0 account:
```bash
xurl --username johndoe /2/users/me
```

## Token Storage

Tokens are stored securely in `~/.xurl` in your home directory.

## Testing

Run the test suite:
```bash
cargo test
```

## Contributing
Contributions are welcome!

## License
This project is open-sourced under the MIT License - see the LICENSE file for details.
