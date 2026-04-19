# Changelog

All user-visible bugs and enhancements should be recorded here.

## Unreleased

Last updated: 2026-04-19 23:48:20 CEST

### Fixed

- [2026-04-19 23:08:51 CEST] OAuth2 callback listeners now bind to the host and port derived from the effective redirect URI instead of always listening on `127.0.0.1:8080`. For `localhost`, `xurl` now listens on both `127.0.0.1` and `::1`, which fixes browser-dependent loopback resolution failures while still supporting non-default callback paths.
- [2026-04-19 23:08:51 CEST] The OAuth2 listener now starts listening before the browser opens, which removes a race where the browser could reach the callback URL before the local server was ready.
- [2026-04-19 23:08:51 CEST] OAuth2 token refresh no longer depends on `/2/users/me` succeeding. If username discovery fails, `xurl` keeps the refreshed token instead of failing the request.
- [2026-04-19 23:08:51 CEST] Shortcut commands that need the current user ID now fall back to `--username` lookups when `/2/users/me` is unavailable.
- [2026-04-19 23:08:51 CEST] `GetOAuth2Header` now consistently returns a `Bearer` header even when it has to trigger a fresh OAuth2 flow.

### Enhanced

- [2026-04-19 23:08:51 CEST] OAuth2 tokens can now be retained without a discovered username label when X’s `/2/users/me` lookup is unavailable. Status output makes that state visible as `(unknown user)` instead of silently dropping the token.
- [2026-04-19 23:08:51 CEST] Repo documentation now describes the effective redirect URI as the source of callback host, port, and path, calls out explicit username authentication as the safer fallback when username discovery is unreliable, and documents the new stored `redirect_uri` behavior.
- [2026-04-19 23:08:51 CEST] Apps can now store a per-app `redirect_uri` in `~/.xurl`, `REDIRECT_URI` from the environment still takes precedence, and `xurl auth apps redirect-uri get/set` plus `auth apps update --redirect-uri` make that configuration visible and editable from the CLI.
- [2026-04-19 23:48:20 CEST] Documentation now records the confirmed X platform enrollment requirement behind `client-forbidden` / `client-not-enrolled` read failures: moving the app to the `Pay-per-use` package and the `Production` environment fixed live `/2/*` reads after OAuth had already succeeded.
