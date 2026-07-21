# Changelog

All user-visible bugs and enhancements should be recorded here.

## Unreleased

### Added

- `xurl chat` — an end-to-end encrypted XChat client: `keys status|restore|import`, `rotate`, `download`, `add-members`, `mark-read`, `typing`, (keys must already be registered by another XChat client; xurl never generates or registers keys), `conversations`, `read`, `send`, and `listen`. Encryption and decryption happen locally via the chat-xdk library; private keys live in `~/.xurl/keys.yml` (mode 600). Requires a cgo build on macOS (amd64/arm64) or Linux (amd64) — prebuilt release binaries ship a stub explaining how to build with chat enabled.

### Changed

- `~/.xurl` is now a directory: tokens and app credentials live in `~/.xurl/auth.yml`, and XChat private keys live in `~/.xurl/keys.yml`. An existing single-file `~/.xurl` migrates automatically (rename-based and non-destructive) on first use, and the existing legacy migrations still apply on top of the new layout: pre-v1.0 JSON-format token files are converted to YAML, and `.twurlrc` import is unchanged. Older xurl binaries cannot read the new layout.

## v1.2.3 - 2026-07-16

### Added

- [2026-07-15] **Cut Release** GitHub Actions workflow (`workflow_dispatch`) to promote `CHANGELOG.md`, commit + tag on `main`, and publish (GitHub release, Homebrew, npm) in one run.

### Fixed

- [2026-07-15] `xurl auth oauth2` no longer always warns that the "default" app has no client credentials when `--app` is omitted. The check used `GetApp("")` (empty-key map lookup, always nil) instead of the real default app, so it false-alarmed even when the active default (e.g. `app-2`) had credentials. The warning now resolves `default_app` and names that app correctly.

## v1.2.2 - 2026-06-29

### Fixed

- [2026-06-29] `mcp --help` no longer contradicts the bridge's behavior. The v1.2.1 help text still said the bridge "never opens a browser itself; if no token exists it exits with that instruction", but v1.2.1 changed the bridge to open the browser for a first-run OAuth2 login when no token is cached. The help now documents that, and points remote/headless hosts to `xurl auth oauth2 [--app NAME] --headless`.

## v1.2.1 - 2026-06-29

### Changed

- [2026-06-29] `mcp` bridge now runs the interactive browser OAuth2 login on first run when no token is cached (using `CLIENT_ID`/`CLIENT_SECRET` from its environment), instead of failing fast. This lets the bridge authenticate with no prior xurl setup — e.g. straight from `npx … mcp` — and then caches/auto-refreshes the token. The MCP handshake is held until the login completes (set a generous `startup_timeout_sec` on the server), and login diagnostics stay on stderr so the stdout JSON-RPC channel is unaffected. On a headless host, authenticate out-of-band first with `xurl auth oauth2 --headless`.

## v1.2.0 - 2026-06-29

### Fixed

- [2026-06-29] `install.sh` now uses `id -u` instead of the bash-only `$EUID` to detect root, so `curl ... | sh` (POSIX/dash) installs to `/usr/local/bin` as root instead of silently falling back to `~/.local/bin`. (#68)
- [2026-06-29] npm install on Windows works again: `install.js` extracts the `.zip` with PowerShell's `Expand-Archive` instead of the Unix `unzip` command. (#56)
- [2026-06-29] `whoami` (and `user`) now request `verified_type` and `subscription_type`, so Premium/blue accounts are reported correctly instead of `verified: false`. (#41)
- [2026-06-29] OAuth2 token exchange and refresh now send client credentials with the correct auth style — HTTP Basic header for confidential clients (those with a secret), `client_id` in the body for public clients — instead of relying on autodetection, which could fail against X with `unauthorized_client: Missing valid authorization header`.
- [2026-06-29] `mcp` bridge no longer launches a browser at startup: it still refreshes an existing token silently, but when none is available it fails fast with instructions (`xurl auth oauth2 [--app NAME] [--headless]`) instead of opening a browser mid-startup (which could hang an MCP client's handshake) and printing to the JSON-RPC stdout channel. OAuth2 diagnostics now go to stderr.
- [2026-06-29] `mcp` bridge no longer lets a strict client hang: a request that cannot be answered — transport failure, a failed token refresh/retry after a 401, or a response with an empty/non-JSON body — now gets a synthesized JSON-RPC error keyed to its id. Notifications (e.g. `notifications/cancelled`) are no longer head-of-line blocked behind an in-flight streaming response, large but valid JSON error bodies are forwarded whole instead of being truncated, the standalone server->client stream stops probing a non-event-stream `200` and only resets its reconnect backoff after a healthy stream, and stdin memory stays bounded when an oversized line is dropped.
- [2026-06-25] `mcp` bridge hardening: serialized token-store access (fixes a fatal data race when a token expires mid-session), strict newline-delimited-JSON stdout (SSE/JSON responses are validated and compacted, non-JSON keep-alives dropped), a forced token refresh on HTTP 401, cancelable stdin so SIGINT/SIGTERM shuts the bridge down, resilience to oversized input lines, a server->client stream that resets its backoff/supports stateless servers/retries 408 & 429, and a best-effort session `DELETE` on shutdown.
- [2026-06-25] `media upload --wait` now also waits for animated GIFs (auto-detected as `tweet_gif`), and a media type that cannot be detected — or is recognized but unsupported (e.g. `application/pdf`) — now fails with a clear message instead of guessing `tweet_image` and getting an opaque API error.
- [2026-06-25] `timeline` `--max-results` minimum corrected to 1 (matches the reverse-chronological endpoint).
- [2026-06-25] The raw-request "No URL provided" usage message now prints to stderr.
- [2026-06-25] `media upload --wait` now actually waits for processing and no longer always sends the trace header — the `waitForProcessing` and `trace` arguments were passed in the wrong order.
- [2026-06-25] Raw API requests now surface the real transport/auth error instead of printing `null` when a request fails before getting an HTTP response (e.g. DNS or connection failures).
- [2026-06-25] Requests with no usable credentials, or an invalid `--auth` value, now fail with a clear authentication error instead of silently sending an unauthenticated request.
- [2026-06-25] `xurl dm` now JSON-encodes message text correctly; quotes, backslashes, and newlines no longer produce a malformed request body.
- [2026-06-25] OAuth2 expiry is stored correctly; a token returned without an expiry now refreshes on next use instead of being treated as never-expiring.
- [2026-06-25] `--max-results` is clamped to each endpoint's accepted range for timeline, mentions, bookmarks, likes, following, followers, dms, and posts.
- [2026-06-25] `fetchUsername` now uses a 10s HTTP timeout, and PKCE verifier generation now handles RNG errors instead of ignoring them.
- [2026-06-25] `webhook start` help now references the correct `-P` pretty-print flag and serves on an isolated `ServeMux`.
- [2026-06-25] `.gitignore` now correctly ignores `.DS_Store` (a missing newline had merged it with a comment).
- [2026-04-19 23:08:51 CEST] OAuth2 callback listeners now bind to the host and port derived from the effective redirect URI instead of always listening on `127.0.0.1:8080`. For `localhost`, `xurl` now listens on both `127.0.0.1` and `::1`, which fixes browser-dependent loopback resolution failures while still supporting non-default callback paths.
- [2026-04-19 23:08:51 CEST] The OAuth2 listener now starts listening before the browser opens, which removes a race where the browser could reach the callback URL before the local server was ready.
- [2026-04-19 23:08:51 CEST] OAuth2 token refresh no longer depends on `/2/users/me` succeeding. If username discovery fails, `xurl` keeps the refreshed token instead of failing the request.
- [2026-04-19 23:08:51 CEST] Shortcut commands that need the current user ID now fall back to `--username` lookups when `/2/users/me` is unavailable.
- [2026-04-19 23:08:51 CEST] `GetOAuth2Header` now consistently returns a `Bearer` header even when it has to trigger a fresh OAuth2 flow.

### Enhanced

- [2026-06-29] Added `xurl auth oauth2 --headless` for authenticating on remote/headless machines where the localhost OAuth callback is unreachable: xurl prints the authorization URL, you open it on any device and approve, then paste the resulting redirect URL (or just the `code`) back at the prompt. No callback listener or local browser is required. (Closes the headless half of #62 / #40.)
- [2026-06-25] OAuth2 tokens now refresh ~30s before expiry (clock-skew leeway) so a token handed to a caller does not expire in-flight; a new forced-refresh path backs the `mcp` bridge's 401 recovery.
- [2026-06-25] `xurl token`'s missing-token error now names the requested user, and `token`/`mcp` errors omit ANSI color when stderr is not a terminal (cleaner piped/logged output). The auto-generated `help`/`completion` commands now appear under the Management group.
- [2026-06-25] Added `xurl token`: prints a valid (refreshed, persisted) OAuth2 access token for the active app to stdout without opening a browser, so it can be scripted. Respects `--app` and `-u/--username`.
- [2026-06-25] Added `xurl mcp [URL]`: a stdio↔Streamable-HTTP MCP bridge for the hosted X API MCP server (default `https://api.x.com/mcp`). It injects `Authorization: Bearer <token>`, maintains the MCP session id, handles plain-JSON and SSE responses, refreshes the token in-process, and triggers the browser login on first run if needed. Usable from any MCP client via `npx -y @xdevplatform/xurl mcp`.
- [2026-06-29] The app-only token command is now `xurl auth app-only [TOKEN]` (named for the auth mode, not the "bearer" token scheme that OAuth2 user tokens also use), taking the token as an argument or from stdin via `-`. It removes the old `app` vs `apps` confusion and the redundant `auth bearer --bearer-token`. Back-compat: `auth app` and `auth bearer` remain aliases and `--bearer-token` is still accepted.
- [2026-06-25] `xurl --help` now groups subcommands into "Posting & Engagement", "Users & Social Graph", "Reading & Lists", and "Management" sections instead of one flat list.
- [2026-06-25] Added `xurl posts USERNAME` to list a user's recent posts.
- [2026-06-25] `xurl --version` is now supported in addition to `xurl version`.
- [2026-06-25] Raw requests now default to `POST` when `-d` is supplied (curl-like), and `media upload` auto-detects the media type and category from the file extension when they are not provided.
- [2026-05-14 11:38:34 PDT] Documentation and the bundled `xurl` skill now recommend authenticating registered apps with `xurl auth oauth2 --app APP_NAME` and explain that omitting `--app` saves the token to the current default app.
- [2026-04-19 23:08:51 CEST] OAuth2 tokens can now be retained without a discovered username label when X’s `/2/users/me` lookup is unavailable. Status output makes that state visible as `(unknown user)` instead of silently dropping the token.
- [2026-04-19 23:08:51 CEST] Repo documentation now describes the effective redirect URI as the source of callback host, port, and path, calls out explicit username authentication as the safer fallback when username discovery is unreliable, and documents the new stored `redirect_uri` behavior.
- [2026-04-19 23:08:51 CEST] Apps can now store a per-app `redirect_uri` in `~/.xurl`, `REDIRECT_URI` from the environment still takes precedence, and `xurl auth apps redirect-uri get/set` plus `auth apps update --redirect-uri` make that configuration visible and editable from the CLI.
- [2026-04-19 23:48:20 CEST] Documentation now records the confirmed X platform enrollment requirement behind `client-forbidden` / `client-not-enrolled` read failures: moving the app to the `Pay-per-use` package and the `Production` environment fixed live `/2/*` reads after OAuth had already succeeded.
