---
name: xurl
description: A curl-like CLI tool for making authenticated requests to the X (Twitter) API. Use this skill when you need to post tweets, reply, quote, search, read posts, manage followers, send DMs, send or read end-to-end encrypted XChat messages, upload media, or interact with any X API v2 endpoint. Supports multiple apps, OAuth 2.0, OAuth 1.0a, and app-only auth.
---

# xurl — Agent Skill Reference

`xurl` is a CLI tool for the X API. It supports both **shortcut commands** (human/agent‑friendly one‑liners) and **raw curl‑style** access to any v2 endpoint. All commands return JSON to stdout.

---

## Prerequisites

This skill requires the `xurl` CLI utility: <https://github.com/xdevplatform/xurl>.

Before using any command you must be authenticated. Run `xurl auth status` to check.

### Secret Safety (Mandatory)

- Never read, print, parse, summarize, upload, or send anything under `~/.xurl/` (or copies of it) to the LLM context.
- `~/.xurl/keys.yml` contains XChat **private encryption keys** — the strictest no-read rule applies.
- Never pass `--pin` inline in agent/LLM sessions (`xurl chat keys restore --pin ...` leaks the recovery PIN to context and shell history). Run `xurl chat keys restore` without the flag so the PIN is prompted without echo, or have the user run it manually.
- Never ask the user to paste credentials/tokens into chat.
- The user must fill `~/.xurl/auth.yml` with required secrets manually on their own machine.
- Do not recommend or execute auth commands with inline secrets in agent/LLM sessions.
- Warn that using CLI secret options in agent sessions can leak credentials (prompt/context, logs, shell history).
- Never use `--verbose` / `-v` in agent/LLM sessions; it can expose sensitive headers/tokens in output.
- Never run `xurl token` in agent/LLM sessions: it prints a live OAuth2 access token to stdout, which is a credential and must not enter the LLM context.
- `xurl mcp` is for configuring an MCP client (it bridges stdio↔HTTP and injects the bearer token); it is not something to invoke directly from an agent/LLM session.
- Sensitive flags that must never be used in agent commands: `--bearer-token`, `--consumer-key`, `--consumer-secret`, `--access-token`, `--token-secret`, `--client-id`, `--client-secret`.
- To verify whether at least one app with credentials is already registered, run: `xurl auth status`.

### Register an app (recommended)

App credential registration must be done manually by the user outside the agent/LLM session.
After credentials are registered, authenticate against the app that holds those credentials:

```bash
xurl auth oauth2 --app APP_NAME
```

You can also run `xurl auth default APP_NAME` first and then use `xurl auth oauth2`.

On a remote/headless machine (no reachable browser callback), add `--headless`: `xurl auth oauth2 --app APP_NAME --headless` prints the authorization URL and reads the pasted redirect URL (or code) back, so no localhost callback is needed.

For multiple pre-configured apps, switch between them:
```bash
xurl auth default prod-app          # set default app
xurl auth default prod-app alice    # set default app + user
xurl --app dev-app /2/users/me      # one-off override
xurl auth apps redirect-uri get prod-app
xurl auth apps redirect-uri set prod-app http://localhost:8080/callback
```

### Other auth methods

Examples with inline secret flags are intentionally omitted. If OAuth1 or app-only auth is needed, the user must run those commands manually outside agent/LLM context.

Tokens are persisted to `~/.xurl/auth.yml` in YAML format (a legacy single-file `~/.xurl` is migrated automatically). Each app has its own isolated tokens and may also store a `redirect_uri`. `REDIRECT_URI` in the environment still takes precedence over the stored app value. Do not read this file (or anything under `~/.xurl/`) through the agent/LLM. Once authenticated, every command below will auto‑attach the right `Authorization` header.

---

## Quick Reference

| Action | Command |
|---|---|
| Post | `xurl post "Hello world!"` |
| Reply | `xurl reply POST_ID "Nice post!"` |
| Quote | `xurl quote POST_ID "My take"` |
| Delete a post | `xurl delete POST_ID` |
| Read a post | `xurl read POST_ID` |
| Search posts | `xurl search "QUERY" -n 10` |
| Who am I | `xurl whoami` |
| Look up a user | `xurl user @handle` |
| List a user's posts | `xurl posts @handle -n 10` |
| Home timeline | `xurl timeline -n 20` |
| Mentions | `xurl mentions -n 10` |
| Like | `xurl like POST_ID` |
| Unlike | `xurl unlike POST_ID` |
| Repost | `xurl repost POST_ID` |
| Undo repost | `xurl unrepost POST_ID` |
| Bookmark | `xurl bookmark POST_ID` |
| Remove bookmark | `xurl unbookmark POST_ID` |
| List bookmarks | `xurl bookmarks -n 10` |
| List likes | `xurl likes -n 10` |
| Follow | `xurl follow @handle` |
| Unfollow | `xurl unfollow @handle` |
| List following | `xurl following -n 20` |
| List followers | `xurl followers -n 20` |
| Block | `xurl block @handle` |
| Unblock | `xurl unblock @handle` |
| Mute | `xurl mute @handle` |
| Unmute | `xurl unmute @handle` |
| Send DM | `xurl dm @handle "message"` |
| List DMs | `xurl dms -n 10` |
| Upload media | `xurl media upload path/to/file.mp4` |
| Media status | `xurl media status MEDIA_ID` |
| **Encrypted Chat (XChat)** | |
| Chat key status | `xurl chat keys status` |
| Restore chat keys | `xurl chat keys restore` (PIN prompted; never pass `--pin` in agent sessions) |
| Import chat keys | `xurl chat keys import` (blob prompted; avoid passing it as an argument) |
| List chat inbox | `xurl chat conversations` |
| Read a conversation | `xurl chat read @handle -n 50` |
| Send encrypted message | `xurl chat send @handle "message"` |
| Listen for new messages | `xurl chat listen @handle` |
| Rotate a conversation key | `xurl chat rotate CONV --yes` (write op — see notes) |
| Send with an attachment | `xurl chat send CONV "text" --file path/to/img.png` |
| Reply to a message | `xurl chat send CONV "text" --reply-to SEQUENCE_ID` |
| Download an attachment | `xurl chat download CONV MEDIA_HASH_KEY -o out.png` |
| Add group members | `xurl chat add-members GROUP @user --yes` (write op) |
| Mark read (explicit) | `xurl chat mark-read CONV` |
| Typing indicator (explicit) | `xurl chat typing CONV` |
| **App Management** | |
| Register app | Manual, outside agent (do not pass secrets via agent) |
| List apps | `xurl auth apps list` |
| Update app config | Manual, outside agent (do not pass secrets via agent) |
| View app redirect URI | `xurl auth apps redirect-uri get [NAME]` |
| Set app redirect URI | `xurl auth apps redirect-uri set NAME URI` |
| Remove app | `xurl auth apps remove NAME` |
| Set default (interactive) | `xurl auth default` |
| Set default (command) | `xurl auth default APP_NAME [USERNAME]` |
| Use app per-request | `xurl --app NAME /2/users/me` |
| Auth status | `xurl auth status` |

> **Post IDs vs URLs:** Anywhere `POST_ID` appears above you can also paste a full post URL (e.g. `https://x.com/user/status/1234567890`) — xurl extracts the ID automatically.

> **Usernames:** Leading `@` is optional. `@elonmusk` and `elonmusk` both work.

---

## Command Details

### Posting

```bash
# Simple post
xurl post "Hello world!"

# Post with media (upload first, then attach)
xurl media upload photo.jpg          # → note the media_id from response
xurl post "Check this out" --media-id MEDIA_ID

# Multiple media
xurl post "Thread pics" --media-id 111 --media-id 222

# Reply to a post (by ID or URL)
xurl reply 1234567890 "Great point!"
xurl reply https://x.com/user/status/1234567890 "Agreed!"

# Reply with media
xurl reply 1234567890 "Look at this" --media-id MEDIA_ID

# Quote a post
xurl quote 1234567890 "Adding my thoughts"

# Delete your own post
xurl delete 1234567890
```

### Reading

```bash
# Read a single post (returns author, text, metrics, entities)
xurl read 1234567890
xurl read https://x.com/user/status/1234567890

# Search recent posts (default 10 results)
xurl search "golang"
xurl search "from:elonmusk" -n 20
xurl search "#buildinpublic lang:en" -n 15
```

### User Info

```bash
# Your own profile
xurl whoami

# Look up any user
xurl user elonmusk
xurl user @XDevelopers

# List a user's recent posts (by @username)
xurl posts elonmusk
xurl posts @XDevelopers -n 25
```

### Timelines & Mentions

```bash
# Home timeline (reverse chronological)
xurl timeline
xurl timeline -n 25

# Your mentions
xurl mentions
xurl mentions -n 20
```

### Engagement

```bash
# Like / unlike
xurl like 1234567890
xurl unlike 1234567890

# Repost / undo
xurl repost 1234567890
xurl unrepost 1234567890

# Bookmark / remove
xurl bookmark 1234567890
xurl unbookmark 1234567890

# List your bookmarks / likes
xurl bookmarks -n 20
xurl likes -n 20
```

### Social Graph

```bash
# Follow / unfollow
xurl follow @XDevelopers
xurl unfollow @XDevelopers

# List who you follow / your followers
xurl following -n 50
xurl followers -n 50

# List another user's following/followers
xurl following --of elonmusk -n 20
xurl followers --of elonmusk -n 20

# Block / unblock
xurl block @spammer
xurl unblock @spammer

# Mute / unmute
xurl mute @annoying
xurl unmute @annoying
```

### Direct Messages

```bash
# Send a DM
xurl dm @someuser "Hey, saw your post!"

# List recent DM events
xurl dms
xurl dms -n 25
```

### Encrypted Chat (XChat)

`xurl chat` is an end-to-end encrypted XChat client: encryption and decryption happen locally via the chat-xdk crypto library, so the server only sees ciphertext. Requires OAuth2 user auth with `dm.read` + `dm.write` scopes, and is available on macOS (Intel/Apple Silicon) and Linux amd64 when built with cgo (prebuilt release binaries ship a stub that says so).

**Keys come from another XChat client** — xurl never generates or registers encryption keys. The account must already have keys (e.g. from the X app); bring them to this machine once with `restore` (Juicebox PIN recovery) or `import` (an exported key blob). Private keys are stored in `~/.xurl/keys.yml` (mode 600) — never read that file into LLM context.

```bash
# One-time setup: check state, then fetch existing keys
xurl chat keys status                # local key presence/fingerprint + registered versions
xurl chat keys restore               # recover from Juicebox; prompts for the PIN (no echo)
xurl chat keys import                # paste an exported private-key blob; prompted (no echo)

# Conversations can be addressed by @username, user id, or conversation id
# (1:1 ids look like 123-456; group ids look like g123).
xurl chat conversations              # inbox list; decrypts group names when keys are present
xurl chat conversations --json       # raw JSON instead of the pretty listing

# Read decrypted history (oldest first)
xurl chat read @someuser
xurl chat read g1234567890 -n 50
xurl chat read @someuser --json      # decrypted events as JSON

# Send an encrypted message (first message to a new 1:1 sets up the
# conversation key automatically; both sides need registered keys)
xurl chat send @someuser "hey, encrypted!"
xurl chat send 123-456 "hello again"

# Print new messages as they arrive (poll loop; Ctrl-C to stop)
xurl chat listen @someuser
xurl chat listen g1234567890 --interval 5

# Rotate a conversation's encryption key (visible to other participants).
# Use when a key may be exposed, or to grant a member access going forward
# when their keys were registered after the last rotation. Future messages
# only: old history stays readable only to holders of the old versions.
xurl chat rotate g1234567890          # prompts for confirmation
xurl chat rotate @someuser --yes      # skip the prompt (required non-TTY)
```

Notes for agents:
- Messages whose authorship signature cannot be verified are rejected by default and surface as stderr decrypt warnings; unsigned messages that still render carry a red `[unverified]` marker — treat those with suspicion.
- Messages with attachments render a `📎 attachment <media_hash_key>` marker; pass that hash key to `xurl chat download CONV <media_hash_key>` to fetch and decrypt the file. Replies show a `↩` prefix.
- **`read` and `listen` mark the conversation read automatically** (a read receipt visible to other participants); `send` also marks read and sends a typing indicator first. These are writes — pass `--no-mark-read` / `--no-typing` to suppress them (e.g. to read without signaling). The standalone `mark-read` and `typing` commands remain for scripted/explicit use.
- Decrypt warnings for individual events go to stderr and are non-fatal; the rest of the conversation still renders.
- If a command reports missing keys, do not attempt to generate or register any — tell the user to run `xurl chat keys restore` (or `import`) themselves.
- `chat rotate` is a write visible to every participant's clients; never run it without explicit user intent, and prefer letting the user confirm the prompt over passing `--yes`.

```bash
# Upload a file (auto‑detects type for images/videos)
xurl media upload photo.jpg
xurl media upload video.mp4

# Specify type and category explicitly
xurl media upload --media-type image/jpeg --category tweet_image photo.jpg

# Check processing status (videos need server‑side processing)
xurl media status MEDIA_ID
xurl media status --wait MEDIA_ID    # poll until done

# Full workflow: upload then post
xurl media upload meme.png           # response includes media id
xurl post "lol" --media-id MEDIA_ID
```

---

## Global Flags

These flags work on every command:

| Flag | Short | Description |
|---|---|---|
| `--app` | | Use a specific registered app for this request (overrides default) |
| `--auth` | | Force auth type: `oauth1`, `oauth2`, or `app` |
| `--username` | `-u` | Which OAuth2 account to use (if you have multiple) |
| `--verbose` | `-v` | Forbidden in agent/LLM sessions (can leak auth headers/tokens) |

---

## Raw API Access

The shortcut commands cover the most common operations. For anything else, use xurl's raw curl‑style mode — it works with **any** X API v2 endpoint:

```bash
# GET request (default)
xurl /2/users/me

# POST with JSON body
xurl -X POST /2/tweets -d '{"text":"Hello world!"}'

# PUT, PATCH, DELETE
xurl -X DELETE /2/tweets/1234567890

# Custom headers
xurl -H "Content-Type: application/json" /2/some/endpoint

# Force streaming mode
xurl -s /2/tweets/search/stream

# Full URLs also work
xurl https://api.x.com/2/users/me
```

---

## Streaming

Streaming endpoints are auto‑detected. Known streaming endpoints include:
- `/2/tweets/search/stream`
- `/2/tweets/sample/stream`
- `/2/tweets/sample10/stream`

You can force streaming on any endpoint with `-s`:
```bash
xurl -s /2/some/endpoint
```

---

## Output Format

All commands return **JSON** to stdout, pretty‑printed with syntax highlighting. The output structure matches the X API v2 response format. A typical response looks like:

```json
{
  "data": {
    "id": "1234567890",
    "text": "Hello world!"
  }
}
```

Errors are also returned as JSON:
```json
{
  "errors": [
    {
      "message": "Not authorized",
      "code": 403
    }
  ]
}
```

---

## Common Workflows

### Post with an image
```bash
# 1. Upload the image
xurl media upload photo.jpg
# 2. Copy the media_id from the response, then post
xurl post "Check out this photo!" --media-id MEDIA_ID
```

### Reply to a conversation
```bash
# 1. Read the post to understand context
xurl read https://x.com/user/status/1234567890
# 2. Reply
xurl reply 1234567890 "Here are my thoughts..."
```

### Search and engage
```bash
# 1. Search for relevant posts
xurl search "topic of interest" -n 10
# 2. Like an interesting one
xurl like POST_ID_FROM_RESULTS
# 3. Reply to it
xurl reply POST_ID_FROM_RESULTS "Great point!"
```

### Check your activity
```bash
# See who you are
xurl whoami
# Check your mentions
xurl mentions -n 20
# Check your timeline
xurl timeline -n 20
```

### Set up multiple apps
```bash
# App credentials must already be configured manually outside agent/LLM context.
# Authenticate users on each pre-configured app
xurl auth default prod
xurl auth oauth2                       # authenticates on prod app

xurl auth default staging
xurl auth oauth2                       # authenticates on staging app

# Switch between them
xurl auth default prod alice           # prod app, alice user
xurl --app staging /2/users/me         # one-off request against staging
```

---

## Error Handling

- Non‑zero exit code on any error.
- API errors are printed as JSON to stdout (so you can still parse them).
- Auth errors suggest re‑running `xurl auth oauth2` or checking your tokens.
- If a command requires your user ID (like, repost, bookmark, follow, etc.), xurl will automatically fetch it via `/2/users/me`. When that endpoint is unreliable, use `--username USERNAME` or authenticate with `xurl auth oauth2 --app APP_NAME USERNAME` so xurl can fall back to username lookup.
- If X returns `client-forbidden` / `client-not-enrolled` after successful auth, check the app’s X developer-console package and environment. In current testing, moving the app to `Pay-per-use` and `Production` fixed `/2/*` read failures without changing local `xurl` auth data.

---

## Notes

- **Rate limits:** The X API enforces rate limits per endpoint. If you get a 429 error, wait and retry. Write endpoints (post, reply, like, repost) have stricter limits than read endpoints.
- **Scopes:** OAuth 2.0 tokens are requested with broad scopes. If you get a 403 on a specific action, your token may lack the required scope — re‑run `xurl auth oauth2` to get a fresh token.
- **Token refresh:** OAuth 2.0 tokens auto‑refresh when expired. No manual intervention needed.
- **Multiple apps:** Each app has its own isolated credentials, tokens, and optional stored `redirect_uri`. Configure credentials manually outside agent/LLM context, then switch with `xurl auth default` or `--app`.
- **Redirect URI precedence:** The effective redirect URI resolves from `REDIRECT_URI` in the environment first, then the app's stored `redirect_uri` in `~/.xurl/auth.yml`, then the built-in default.
- **Redirect URI management:** Use `xurl auth apps redirect-uri get [NAME]`, `xurl auth apps redirect-uri set NAME URI`, or `xurl auth apps update NAME --redirect-uri URI` to inspect and manage the stored per-app callback value.
- **X platform enrollment:** A successful OAuth callback does not guarantee `/2/*` reads will work. If you see `client-not-enrolled`, verify the app is in the correct X package/environment. Current confirmed fix: `Apps` -> `Manage apps` -> `Move to package` -> choose `Pay-per-use`, then move the app to `Production`.
- **Multiple accounts:** You can authenticate multiple OAuth 2.0 accounts per app and switch between them with `--username` / `-u` or set a default with `xurl auth default APP USER`.
- **Default user:** When no `-u` flag is given, xurl uses the default user for the active app (set via `xurl auth default`). If no default user is set, it uses the first available token.
- **Token storage:** `~/.xurl` is a directory; `~/.xurl/auth.yml` holds each app's credentials and tokens. Never read or send anything under `~/.xurl/` to LLM context.
- **Chat key storage:** `~/.xurl/keys.yml` holds XChat **private encryption keys** per user (mode 600). Losing it means losing the ability to decrypt on this machine (recoverable via `xurl chat keys restore` if a Juicebox PIN backup exists). Never read or send this file to LLM context.
- **Chat key registration:** xurl performs none — no public-key registration and no Juicebox writes. Only keys already registered by another XChat client can be restored or imported; unregistered keys are rejected.
- **Access tokens:** `xurl token` prints a valid (refreshed) OAuth2 access token for the active app to stdout, refreshing and persisting it if expired. It never opens a browser. The output is a secret — use it only in the user's own scripts, never in agent/LLM sessions.
- **MCP bridge:** `xurl mcp [URL]` bridges a stdio MCP client to a remote Streamable HTTP MCP server (default `https://api.x.com/mcp`), injecting `Authorization: Bearer <token>` and refreshing the token automatically. On first run with no cached token it opens the browser for a one-time OAuth2 login using the `CLIENT_ID`/`CLIENT_SECRET` from its environment (the handshake waits for it, so set a generous `startup_timeout_sec`); on a headless host, authenticate out-of-band first with `xurl auth oauth2 --headless`. Configure it in an MCP client via the npm launcher: `{"command":"npx","args":["-y","@xdevplatform/xurl","mcp","https://api.x.com/mcp"],"env":{"CLIENT_ID":"...","CLIENT_SECRET":"..."},"startup_timeout_sec":300}`.
