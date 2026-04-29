English | [日本語](README.ja.md)

# kintone CLI

A CLI tool / MCP server for interacting with the kintone API.
All commands output results in JSON format, suitable for LLM pipelines and shell scripting.

> **Status**: All 11 milestones complete. Ready for release (push a tag to automatically distribute via GitHub Releases / Homebrew / ghcr.io).

## Installation

Choose from 4 installation methods.

### 1. Homebrew (macOS / Linux)

```bash
brew install youyo/tap/kintone
```

> The Homebrew Tap repository `youyo/homebrew-tap` must be public before using this method (auto-updated after each release).

### 2. Docker (multi-arch: amd64 / arm64)

```bash
docker pull ghcr.io/youyo/kintone:latest
docker run --rm ghcr.io/youyo/kintone:latest version
```

Mount `~/.config/kintone` and `~/.cache/kintone` for a persistent setup:

```bash
docker run --rm -it \
  -v "$HOME/.config/kintone:/home/nonroot/.config/kintone" \
  -v "$HOME/.cache/kintone:/home/nonroot/.cache/kintone" \
  ghcr.io/youyo/kintone:latest api records --app 1
```

### 3. `go install`

```bash
go install github.com/youyo/kintone/cmd/kintone@latest
```

### 4. GitHub Releases binary

Download the tar.gz / zip for your OS and arch from [Releases](https://github.com/youyo/kintone/releases).
Verify integrity with the SHA256 checksum file (`checksums.txt`).

### Build from source (for developers)

```bash
git clone https://github.com/youyo/kintone.git
cd kintone
go build -o /usr/local/bin/kintone ./cmd/kintone
```

## Authentication Overview

| Method | Use case | Required setup | Command |
|---|---|---|---|
| **API Token** | One-off CLI / single-user MCP / simple automation | `KINTONE_DOMAIN` + `KINTONE_API_TOKEN` (or config.toml) | `kintone api/ops/mcp serve` (default) |
| **OAuth 2.0** | Per-user authorization / multi-user | Register OAuth client + `kintone auth login --oauth --principal-id <id>` | `kintone api/ops ...` (uses stored token automatically) |
| **OIDC remote (idproxy)** | Remote MCP server + multiple users + identity provider | OIDC Issuer / Client / Cookie Secret etc. | `kintone mcp serve --listen :8080 --auth oidc --authz oauth` |

See the "API Token Authentication", "OAuth Authentication", and "MCP Server" sections below for details.

## Usage

### Check version

```bash
$ kintone version
{"ok":true,"data":{"version":"0.1.0"}}

$ kintone version --short
0.1.0
```

### Help

```bash
$ kintone --help
```

### Configuration (config)

Configuration is resolved in the following priority order:

1. CLI flags (`--profile`, `--config`)
2. Environment variables (`KINTONE_*`)
3. `~/.config/kintone/config.toml`

#### Initialize config file

```bash
$ kintone config init
{"ok":true,"data":{"path":"/Users/foo/.config/kintone/config.toml","created":true}}
```

Returns an error (`CONFIG_ALREADY_EXISTS`) if the file already exists. Use `--force` to overwrite.
The path can be changed with `--config` or `KINTONE_CONFIG_PATH`.

#### Example config file

```toml
[default_profile]
name = "default"

[profiles.default]
domain = "example.cybozu.com"
auth   = "api-token"

[profiles.dev]
domain = "dev.cybozu.com"
auth   = "oauth"
```

> Note: Do not write sensitive values such as API Token or OAuth client secret in config.toml.
> Pass them via environment variables (e.g. `KINTONE_API_TOKEN`).

#### Show current configuration

```bash
$ kintone config show
{"ok":true,"data":{"profile":"default","domain":"example.cybozu.com","auth":"api-token","api_token":"***","config_path":"/Users/foo/.config/kintone/config.toml","cache_path":"","source":{"profile":"file","domain":"file","auth":"file"}}}
```

`api_token` is masked (`***`) as a sensitive value. Each field in `source` shows where the value came from (`cli` / `env` / `file` / `default`).

To use a specific profile:

```bash
$ kintone config show --profile dev
```

#### Environment variables

| Variable | Purpose |
|------|------|
| `KINTONE_PROFILE` | Profile name to use |
| `KINTONE_CONFIG_PATH` | Path to config.toml |
| `KINTONE_CACHE_PATH` | Path to cache db (default: `~/.cache/kintone/cache.db`; container: `/data/kintone/cache.db`) |
| `KINTONE_CACHE_DISABLE` | Set to `1` to disable the SQLite cache (always calls the API directly) |
| `KINTONE_DOMAIN` | kintone domain (e.g. `example.cybozu.com`) |
| `KINTONE_AUTH` | Authentication mode (`api-token` / `oauth`) |
| `KINTONE_API_TOKEN` | API Token |
| `KINTONE_OAUTH_CLIENT_ID` | OAuth client ID |
| `KINTONE_OAUTH_CLIENT_SECRET` | OAuth client secret (prefer environment variable over config.toml) |
| `KINTONE_OAUTH_REDIRECT_URL` | OAuth redirect URI (e.g. `http://127.0.0.1:18080/callback`). Must exactly match the registered OAuth client in kintone |
| `KINTONE_OAUTH_SCOPES` | OAuth scopes (space-separated; default: `k:app_record:read k:app_record:write k:app_settings:read k:app_settings:write k:file:read k:file:write`) |

## API Token Authentication

Specify the API Token via the `KINTONE_API_TOKEN` environment variable or the `api_token` field in config.toml.
The `internal/auth` package automatically attaches the `X-Cybozu-API-Token` header.

## OAuth Authentication (M09)

Supports kintone's OAuth 2.0 Authorization Code Grant + PKCE flow.
Set `KINTONE_AUTH=oauth` to use OAuth access tokens instead of an API Token.
Token expiration (typically 1h) is detected automatically and transparently refreshed using the refresh token.

### Prerequisites

1. Register an OAuth client in the kintone admin panel and set the redirect URI to `http://127.0.0.1:<port>/callback`
2. Set the client ID / secret / redirect URI as environment variables:

```bash
export KINTONE_DOMAIN=example.cybozu.com
export KINTONE_AUTH=oauth
export KINTONE_OAUTH_CLIENT_ID=your-client-id
export KINTONE_OAUTH_CLIENT_SECRET=your-client-secret
export KINTONE_OAUTH_REDIRECT_URL=http://127.0.0.1:18080/callback
```

### Login (Authorization Code Flow)

```bash
$ kintone auth login --oauth --principal-id oauth:alice
```

- A browser opens the kintone authorization page
- After the user consents, the browser is redirected to `http://127.0.0.1:<port>/callback` and an access token is obtained
- The token is stored in `~/.cache/kintone/tokens.db` (file permission 0600)

In environments without a browser (SSH / CI), use `--no-browser` to print the authorization URL to stderr:

```bash
$ kintone auth login --oauth --principal-id oauth:alice --no-browser
```

For multiple users on the same domain, specify `--principal-id` individually:

```bash
$ kintone auth login --oauth --principal-id oauth:bob
```

> Note: `--principal-id` is the key in the TokenStore. Different users on the same domain must use different values.
> Automatic retrieval will be introduced in M10 (OIDC support).

### Check login status

```bash
$ kintone auth status
{"ok":true,"data":{"entries":[{"principal_id":"oauth:alice","expires_at":"2026-04-29T10:00:00Z","has_refresh_token":true,"scope":"k:app_record:read k:app_record:write...","masked_token":"abcd...wxyz"}]}}
```

To filter by a specific user:

```bash
$ kintone auth status --principal-id oauth:alice
```

### Logout

Delete a specific user's token:

```bash
$ kintone auth logout --principal-id oauth:alice
{"ok":true,"data":{"deleted":1}}
```

Delete all OAuth tokens for the domain:

```bash
$ kintone auth logout --all
{"ok":true,"data":{"deleted":3}}
```

### Security notes

- Manage `KINTONE_OAUTH_CLIENT_SECRET` via environment variables (storing it in config.toml is not recommended)
- `oauth_client_secret` is masked as `***` in `kintone config show` output
- Access tokens in `kintone auth status` output are masked as first 4 chars + `...` + last 4 chars
- tokens.db is stored with permission 0600 but in plain text (encryption planned for M11)
- PKCE (S256) and CSRF state verification are performed (`crypto/rand` for generation, `subtle.ConstantTimeCompare` for verification)

## kintoneapi Client

The `internal/kintoneapi` package is a thin `net/http` wrapper.

- `Client`: REST client holding base URL / auth / retry configuration
- `Transport`: `http.RoundTripper` wrapper (attaches auth headers, parses errors)
- `APIError`: Structured kintone standard error (`code` / `id` / `message` / `HTTPStatus` / `RetryAfter`)
- Endpoints: `GET /k/v1/records.json`, `/k/v1/record.json`, `/k/v1/app.json`, `/k/v1/app/form/fields.json`
- Supports Retry-After header (automatically parses wait time on 429 rate limit responses)

## API Subcommands (`kintone api ...`)

Transparent pass-through commands for the kintone REST API. Output is always JSON, designed for LLM / `jq` integration.

> Internal structure: The `service/api` layer passes through REST endpoints 1:1, and the `service/operations` layer composes and formats responses for LLM use. The CLI never imports `kintoneapi` directly — it always goes through the service layer.

Set domain / API Token via environment variables first:

```bash
export KINTONE_DOMAIN=example.cybozu.com
export KINTONE_AUTH=api-token
export KINTONE_API_TOKEN=xxxxxxxxxxxxxxxxxxxx
```

### List records

```bash
$ kintone api records get --app 1 --query 'name = "foo"' --field name --field age --total-count
{"ok":true,"data":{"records":[{...}],"total_count":3}}

# Specify App by code / name / partial (M08 name resolution)
$ kintone api records get --app-ref sales --query 'createdAt > LAST_WEEK()'
{"ok":true,"data":{"records":[{...}]}}
```

| Flag | Type | Required | Description |
|--------|---|------|------|
| `--app` | int64 | one of | kintone app ID (numeric, mutually exclusive with `--app-ref`) |
| `--app-ref` | string | one of | App reference (numeric string / code / name / partial, mutually exclusive with `--app`, M08) |
| `--query` | string | - | kintone query language |
| `--field` | string (repeatable) | - | Field codes to include in the response |
| `--total-count` | bool | - | Include `total_count` when true |

`--app` and `--app-ref` are **mutually exclusive; exactly one is required**.

> Specify `--field` by **repeating the flag** (`--field name --field age`). Comma-separated values are not supported.

### Get a single record

```bash
$ kintone api record get --app 1 --id 5
{"ok":true,"data":{"record":{...}}}
```

### App info (snake_case unified)

```bash
$ kintone api app get --app 1
{"ok":true,"data":{"app_id":"1","code":"myapp","name":"テスト",...}}
```

### Field definitions

```bash
$ kintone api app fields --app 1 --lang ja
{"ok":true,"data":{"properties":{"name":{"type":"SINGLE_LINE_TEXT",...}},"revision":"5"}}
```

### App + fields combined (via operations)

```bash
$ kintone api app describe --app 1 --lang ja
{"ok":true,"data":{"app":{"app_id":"1","name":"テスト",...},"fields":{...},"revision":"5"}}
```

Combines `app.json` and `app/form/fields.json` so an LLM can understand the full app structure in a single call.

## Ops Subcommands (`kintone ops ...`)

Semantically enriched record CRUD and app description for LLMs. Write operations support `--dry-run` to inspect the request body before sending.

> Internal structure: The `service/operations` layer calls `kintoneapi` through `service/api`.
> The CLI never imports `kintoneapi` directly — it always goes through the service layer.
> Write operations (POST/PUT/DELETE) have **retries disabled by default** (to avoid duplicate creation).

### Create records

Single record:
```bash
$ kintone ops record create --app 1 --record-json '{"name":{"value":"foo"}}'
{"ok":true,"data":{"ids":[100],"revisions":[1]}}
```

Multiple records:
```bash
$ kintone ops record create --app 1 --records-json '[{"name":{"value":"a"}},{"name":{"value":"b"}}]'
{"ok":true,"data":{"ids":[101,102],"revisions":[1,1]}}
```

Dry-run (inspect request body without calling the API):
```bash
$ kintone ops record create --app 1 --record-json '{"name":{"value":"foo"}}' --dry-run
{"ok":true,"data":{"dry_run":true,"method":"POST","path":"/k/v1/records.json","body":{"app":1,"records":[{"name":{"value":"foo"}}]}}}
```

| Flag | Type | Required | Description |
|--------|---|------|------|
| `--app` | int64 | one of | kintone app ID (mutually exclusive with `--app-ref`) |
| `--app-ref` | string | one of | App reference (numeric string / code / name / partial, mutually exclusive with `--app`, M08) |
| `--record-json` | string | one of | Single record JSON |
| `--records-json` | string | one of | Multiple records JSON array |
| `--dry-run` | bool | - | Output the request body without calling the API |

`--app` and `--app-ref` are **mutually exclusive; exactly one is required**.
`--record-json` and `--records-json` are **mutually exclusive; exactly one is required**.

### Update a record

By ID:
```bash
$ kintone ops record update --app 1 --id 7 --record-json '{"name":{"value":"updated"}}'
{"ok":true,"data":{"revision":3}}
```

By updateKey (unique field):
```bash
$ kintone ops record update --app 1 --update-key-field code --update-key-value A1 --record-json '{"name":{"value":"updated"}}'
```

Optimistic locking (`--revision`):
```bash
$ kintone ops record update --app 1 --id 7 --revision 2 --record-json '{"name":{"value":"x"}}'
```

| Flag | Type | Required | Description |
|--------|---|------|------|
| `--app` | int64 | one of | kintone app ID (mutually exclusive with `--app-ref`) |
| `--app-ref` | string | one of | App reference (numeric string / code / name / partial, mutually exclusive with `--app`, M08) |
| `--id` | int64 | one of | Target record ID |
| `--update-key-field` | string | one of | updateKey: field code (mutually exclusive with `--update-key-field-ref`) |
| `--update-key-field-ref` | string | one of | updateKey: field reference (label / partial, M08) |
| `--update-key-value` | string | one of | updateKey: value (required when using updateKey path) |
| `--record-json` | string | required | Update content JSON |
| `--revision` | int64 | - | Revision for optimistic locking |
| `--dry-run` | bool | - | Output request body without calling the API |

`--app` and `--app-ref` are **mutually exclusive; exactly one is required**.
`--id` and `--update-key-*` are **mutually exclusive; exactly one is required**.
`--update-key-field` and `--update-key-field-ref` are **mutually exclusive**.

When `--update-key-field-ref` is specified, the field code is resolved by label / partial after the app ID is resolved.
On ambiguous results, `RESOLVER_FIELD_AMBIGUOUS` is returned with candidates in `details.candidates`.

### Delete records

```bash
$ kintone ops record delete --app 1 --id 7 --id 8
{"ok":true,"data":{"deleted":2}}
```

With revisions (optimistic locking):
```bash
$ kintone ops record delete --app 1 --id 7 --id 8 --revision 3 --revision 4
```

Dry-run:
```bash
$ kintone ops record delete --app 1 --id 7 --id 8 --dry-run
{"ok":true,"data":{"dry_run":true,"method":"DELETE","path":"/k/v1/records.json","body":{"app":1,"ids":[7,8]}}}
```

| Flag | Type | Required | Description |
|--------|---|------|------|
| `--app` | int64 | one of | kintone app ID (mutually exclusive with `--app-ref`) |
| `--app-ref` | string | one of | App reference (numeric string / code / name / partial, mutually exclusive with `--app`, M08) |
| `--id` | int64 (repeatable) | required | Target record ID(s) (`--id 1 --id 2`) |
| `--revision` | int64 (repeatable) | - | Revisions for optimistic locking (same count as `--id`) |
| `--dry-run` | bool | - | Output request body without calling the API |

### App describe (also available under ops)

Equivalent to `kintone api app describe`. Re-published under the `ops` namespace so LLMs can discover it there.

```bash
$ kintone ops app describe --app 1 --lang ja
{"ok":true,"data":{"app":{"app_id":"1","name":"テスト",...},"fields":{...},"revision":"5"}}
```

## Name Resolution (Resolver / M08)

You can pass **non-numeric references** to the `--app` argument (and `--update-key-field`) in CLI / MCP.

### App resolution order

1. **Direct ID**: A numeric string like `"42"` is used as the App ID directly
2. **Exact code match**: Searches via `ListApps?codes[]=ref`
3. **Exact name match**: Filters results from `ListApps?name=ref` where `Name == ref`
4. **Partial name match**: Filters the same response for entries where `Name` contains `ref`

The first match wins (no fallback — predictability first).
On ambiguous results (multiple matches), `RESOLVER_APP_AMBIGUOUS` is returned with all candidates in `details.candidates`.

### Field resolution order

1. **Exact code match**: Key match in `properties`
2. **Exact label match**: Full scan of properties
3. **Partial label match**: Extracted via `strings.Contains`

### Examples

```bash
# Resolve app by code
$ kintone api records get --app-ref sales --query 'createdAt > LAST_WEEK()'
{"ok":true,"data":{"records":[...]}}

# Partial name match returns multiple results → ambiguous
$ kintone api app describe --app-ref 営業
{"ok":false,"error":{"code":"RESOLVER_APP_AMBIGUOUS","message":"...","details":{"kind":"app","ref":"営業","candidates":[{"id":"42","code":"sales","name":"営業 A"},{"id":"55","code":"sales2","name":"営業 B"}]}}}

# Resolve field by label and update via updateKey
$ kintone ops record update --app-ref sales --update-key-field-ref 顧客名 --update-key-value 山田 --record-json '{"phone":{"value":"080-..."}}'
```

### Cache integration

The Resolver calls `ListApps` / `GetFormFields` through `service/api.API` (wrapped by CachingAPI from M07).
With a 1-year TTL, the same ref only triggers one REST call regardless of how many times it is resolved.

When names change:
- The old name continues to resolve for up to 1 year. Run `kintone cache clear --scope=apps` to force a refresh.

### Backward compatibility

The existing `--app <int>` numeric flag continues to work unchanged.
The MCP `app: number` argument is still accepted (`Required` removed, `app_ref: string` added).

---

## Cache Management (`kintone cache ...`)

Caches kintone API app / field information in SQLite to reduce repeated requests.
The TokenStore safely stores and manages OAuth access tokens.

### Cache statistics

```bash
$ kintone cache stats
{"ok":true,"data":{"db_path":"/Users/foo/.cache/kintone/cache.db","exists":true,"size_bytes":49152,"entry_count":12,"expired_count":0}}
```

Returns `exists: false` statistics if the DB file does not exist.

### Clear cache

```bash
$ kintone cache clear
{"ok":true,"data":{"cleared":true,"deleted_count":12}}
```

Use the `--key` flag to delete only entries matching a specific key prefix (omit to delete all entries).

| Flag | Type | Required | Description |
|--------|---|------|------|
| `--key` | string | - | Key prefix to delete (omit to delete all) |

## Shell Completion (`kintone completion`)

Generates completion scripts for bash / zsh / fish / powershell.

> **Output convention exception**: Completion output is intended to be directly `source`d / `Invoke-Expression`d by the shell,
> so it is printed as a plain script to stdout rather than wrapped in a JSON envelope (`{"ok":true,...}`).
> This is explicitly noted as an exception, on par with `version --short`.

### bash

```bash
# Linux
kintone completion bash | sudo tee /etc/bash_completion.d/kintone > /dev/null

# macOS (Homebrew)
kintone completion bash > "$(brew --prefix)/etc/bash_completion.d/kintone"
```

### zsh

```bash
kintone completion zsh > "${fpath[1]}/_kintone"
# Reload
autoload -U compinit && compinit
```

### fish

```bash
kintone completion fish > ~/.config/fish/completions/kintone.fish
```

### PowerShell

Add to `$PROFILE`:

```powershell
kintone completion powershell | Out-String | Invoke-Expression
```

## MCP Server (`kintone mcp serve`)

Starts an MCP (Model Context Protocol) server for operating kintone from LLM clients such as Claude Desktop.

### Modes

- Without `--listen`: **stdio JSON-RPC** (default; for child process launch from Claude Desktop etc.)
- `--listen :8080`: **HTTP / Streamable** (remote MCP; shared across multiple clients)

### Authentication flags

| Flag / Env var | Values | Default | Role |
|---|---|---|---|
| `--listen` / `KINTONE_MCP_LISTEN_ADDR` | `host:port` | empty | Empty for stdio, value for HTTP |
| `--auth` / `KINTONE_MCP_AUTH_MODE` | `none` / `oidc` | `none` | Front-end authentication for incoming requests |
| `--authz` / `KINTONE_MCP_AUTHZ_MODE` | `api-token` / `oauth` | `api-token` | Authentication to upstream kintone |

### stdio + API Token (existing, backward-compatible)

```bash
$ KINTONE_DOMAIN=example.cybozu.com \
  KINTONE_AUTH=api-token \
  KINTONE_API_TOKEN=xxxx \
  kintone mcp serve
```

### HTTP + OIDC + multi-user (from M10)

When `auth=oidc`, [github.com/youyo/idproxy](https://github.com/youyo/idproxy) v0.4.2 is embedded.
It validates an OIDC-based Bearer JWT per request and extracts `principal_id = "<issuer>:<sub>"`.
Each user must pre-register their upstream kintone OAuth token via
`kintone auth login --oauth --principal-id "<issuer>:<sub>"` to the TokenStore.

```bash
$ KINTONE_DOMAIN=example.cybozu.com \
  KINTONE_AUTH=oauth \
  KINTONE_OAUTH_CLIENT_ID=... \
  KINTONE_OAUTH_CLIENT_SECRET=... \
  \
  KINTONE_MCP_OIDC_ISSUER=https://accounts.google.com \
  KINTONE_MCP_OIDC_CLIENT_ID=... \
  KINTONE_MCP_OIDC_CLIENT_SECRET=... \
  KINTONE_MCP_EXTERNAL_URL=https://mcp.example.com \
  KINTONE_MCP_COOKIE_SECRET=$(openssl rand -hex 32) \
  \
  kintone mcp serve --listen :8080 --auth oidc --authz oauth
```

Endpoints:
- `POST /mcp` — Streamable HTTP transport (main MCP client entry point)
- `/login`, `/callback`, `/select`, `/.well-known/*`, `/authorize`, `/token`, `/register` — reserved by idproxy

> **MVP scope**: In M10, the idproxy SigningKey is generated ephemerally at startup (issued JWTs are invalidated on restart). Persistent key support is planned for M11+.
> **Provisioning**: Each user's kintone refresh_token must be registered in the TokenStore via CLI login before use. Automatic OAuth prompting from within MCP is planned for M11+.


6 tools are provided:

| Tool | Description |
|---------|------|
| `apps_search` | Search apps by ids/codes/name/space_ids/limit/offset |
| `app_describe` | Get basic app info + form field definitions (`app` or `app_ref`) |
| `records_query` | Query records using kintone query language (query / fields / total_count, `app` or `app_ref`) |
| `record_create` | Create records (record / records mutually exclusive, up to 100, `app` or `app_ref`) |
| `record_update` | Update a single record (id / update_key_* mutually exclusive, optimistic locking, `app`+`app_ref`, `update_key_field`+`update_key_field_ref` mutually exclusive) |
| `record_delete` | Delete multiple records (optional revisions, `app` or `app_ref`) |

All tools except `apps_search` have had **`app_ref: string` added since M08**.
`app: number` and `app_ref: string` are mutually exclusive (exactly one required).
`record_update` also has `update_key_field_ref: string` added (resolves field code by label / partial).

Each tool's output stores the same JSON envelope as the CLI (`{"ok":true,"data":{...}}` /
`{"ok":false,"error":{...}}`) in `CallToolResult.Content[0].Text`.
LLM clients can `JSON.parse` the result and work with the same semantics as the CLI.

### Claude Desktop configuration example (macOS)

`~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "kintone": {
      "command": "kintone",
      "args": ["mcp", "serve"],
      "env": {
        "KINTONE_DOMAIN": "example.cybozu.com",
        "KINTONE_AUTH": "api-token",
        "KINTONE_API_TOKEN": "xxxxxxxxxxxxxxxxxxxx"
      }
    }
  }
}
```

> stdio mode supports `api-token` / `oauth` (single user) authentication.
> HTTP + OIDC multi-user remote MCP is supported from M10 (see above).
> Single-user OAuth via CLI is available with `kintone auth login --oauth` (M09 and later).

## JSON Output Convention

All commands write to stdout in the following format.

**On success**:
```json
{"ok":true,"data":{...}}
```

**On failure**:
```json
{"ok":false,"error":{"code":"USAGE","message":"..."}}
```

Example with `jq`:

```bash
$ kintone version | jq -r '.data.version'
0.1.0
```

## Roadmap

See [plans/kintone-roadmap.md](plans/kintone-roadmap.md) for details.
All 11 milestones are complete.

## Release procedure (maintainers)

Pushing a tag triggers `.github/workflows/release.yml`, which distributes artifacts to
GitHub Releases / Homebrew Tap / ghcr.io.

```bash
# 1. Final check on main
git checkout main && git pull
go test -race ./... && golangci-lint run ./...

# 2. Create and push tag
git tag v0.1.0
git push origin v0.1.0
```

One-time setup:

- Create the `youyo/homebrew-tap` repository on GitHub (can be empty)
- Register `HOMEBREW_TAP_GITHUB_TOKEN` in repository Settings > Secrets > Actions
  (a Personal Access Token with `repo` scope for the Tap repository)
- Pushing to ghcr.io works with `GITHUB_TOKEN`'s `packages: write` permission (no additional setup needed)

## License

[MIT License](LICENSE)
