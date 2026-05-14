English | [日本語](README.ja.md)

# kintone CLI

A CLI tool / MCP server for interacting with the kintone API.
All commands output results in JSON format, suitable for LLM pipelines and shell scripting.

## Features

- **Consistent JSON output**: success `{"ok":true,"data":{...}}` / failure `{"ok":false,"error":{...}}` — easy to integrate with `jq` or LLM pipelines
- **Three authentication modes**: API Token / OAuth 2.0 (with PKCE) / OIDC remote (idproxy embedded)
- **MCP server**: stdio and HTTP Streamable transport — operate kintone directly from LLM clients such as Claude Desktop
- **Name resolution**: refer to apps and fields not only by numeric ID but also by code / name / label (with partial match)
- **SQLite cache**: caches app and field metadata with a 1-year TTL to reduce REST calls
- **Cross-platform distribution**: Homebrew / Docker (multi-arch) / `go install` / GitHub Releases binaries

## Installation

Choose from 4 installation methods.

### 1. Homebrew (macOS / Linux)

```bash
brew install youyo/tap/kintone
```

### 2. Docker (multi-arch: amd64 / arm64)

```bash
docker pull ghcr.io/youyo/kintone:latest
docker run --rm ghcr.io/youyo/kintone:latest version
```

Mount `~/.config/kintone` and `~/.local/state/kintone` for a persistent setup:

```bash
docker run --rm -it \
  -v "$HOME/.config/kintone:/home/nonroot/.config/kintone" \
  -v "$HOME/.local/state/kintone:/home/nonroot/.local/state/kintone" \
  ghcr.io/youyo/kintone:latest api records get --app 1
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
| **API Token** | Local CLI / single-user MCP (stdio) / simple automation | `KINTONE_DOMAIN` + `KINTONE_API_TOKEN` (or config.toml) | `kintone api/ops/mcp serve` (default) |
| **OAuth 2.0** (Remote MCP only) | Per-user authorization on a hosted MCP server / multi-user | Register OAuth client (redirect_uri = MCP server's public https URL) + server-hosted callback flow (planned in M13) | Browser consent through the remote MCP web flow; server stores tokens in TokenStore |
| **OIDC remote (idproxy)** | Remote MCP server + multiple users + identity provider | OIDC Issuer / Client / Cookie Secret etc. | `kintone mcp serve --listen :8080 --auth oidc --authz oauth` |

> **Note (v0.3.0+)**: kintone OAuth requires `https` for the redirect URI, so the local CLI loopback flow (`kintone auth login --oauth`) has been removed. **Local CLI usage is API Token only**, and **OAuth is reserved for remote MCP servers**.

> **v0.4.2+: 自動カスケード認証フロー**: OIDC ログイン完了後、kintone OAuth が未完了のブラウザリクエストは自動的に `/oauth/kintone/start` へリダイレクトされます。`/login → [IdP] → /callback → /oauth/kintone/start → [kintone OAuth] → 完了` のフローがワンストップで完結します。問題が発生した場合は `KINTONE_MCP_DISABLE_OAUTH_CASCADE=1` で旧挙動（手動 `/oauth/kintone/start` アクセス）に戻せます。

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
| `KINTONE_DOMAIN` | kintone domain (e.g. `example.cybozu.com`) |
| `KINTONE_AUTH` | Authentication mode (`api-token` / `oauth`) |
| `KINTONE_API_TOKEN` | API Token |
| `KINTONE_OAUTH_CLIENT_ID` | OAuth client ID |
| `KINTONE_OAUTH_CLIENT_SECRET` | OAuth client secret (prefer environment variable over config.toml) |
| `KINTONE_OAUTH_REDIRECT_URL` | OAuth redirect URI (e.g. `http://127.0.0.1:18080/callback`). Must exactly match the registered OAuth client in kintone |
| `KINTONE_OAUTH_SCOPES` | OAuth scopes (space-separated; default: `k:app_record:read k:app_record:write k:app_settings:read k:app_settings:write k:file:read k:file:write`) |
| `KINTONE_STORE_BACKEND` | Storage backend (`memory` / `sqlite` / `redis` / `dynamodb`; default: `sqlite`) |
| `KINTONE_STORE_SQLITE_DIR` | Directory for SQLite files (default: `~/.local/state/kintone/`; contains `kintone.db` and `idproxy.db`) |
| `KINTONE_STORE_REDIS_URL` | Redis URL including DB index (`redis://` or `rediss://`; required when backend=redis) |
| `KINTONE_STORE_REDIS_TLS` | Set to `1` to force TLS upgrade for `redis://` connections (`rediss://` is always TLS) |
| `KINTONE_STORE_REDIS_PASSWORD` | Redis AUTH password (alternative to embedding in URL) |
| `KINTONE_STORE_REDIS_INSECURE_PLAINTEXT` | Set to `1` to explicitly allow plaintext `redis://` to non-localhost hosts (default: `0`; disabled for security) |
| `KINTONE_STORE_CACHE_BYPASS` | Set to `1` to disable cache only (TokenStore/SigningKey continue to work; replaces `KINTONE_CACHE_DISABLE`) |
| `KINTONE_STORE_DYNAMODB_TABLE` | DynamoDB table name (required when backend=dynamodb) |
| `KINTONE_STORE_DYNAMODB_REGION` | DynamoDB region (falls back to AWS SDK region resolution) |
| `KINTONE_MCP_SIGNING_KEY_PEM` | PKCS#8 PEM string for OIDC JWT signing key (takes priority over Storage; required for `auth=oidc` in production) |
| `KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE` | Set to `1` to allow auto-generation of signing key in Storage (dev/test only; not recommended for production) |
| `KINTONE_LOG_LEVEL` | Log level (`debug` / `info` / `warn` / `error`; default: `info`) |

## Storage Backend

kintone CLI/MCP stores authentication credentials, cache, and OIDC state in a single Storage backend.

| Backend  | Use case                       | Primary ENV                                          |
|----------|--------------------------------|------------------------------------------------------|
| memory   | dev / test                     | `KINTONE_STORE_BACKEND=memory`                       |
| sqlite   | host / single-instance (default) | `KINTONE_STORE_SQLITE_DIR=...`                     |
| redis    | k8s / Fargate scale-out        | `KINTONE_STORE_REDIS_URL=rediss://...`               |
| dynamodb | Lambda / serverless            | `KINTONE_STORE_DYNAMODB_TABLE=...`                   |

All backends share a single connection for kintone TokenStore, Cache, OIDC SigningKey, OAuth State, and idproxy session/refresh_token
(logically separated by key prefix; SQLite uses 2 separate files in the same directory).

Since M14 the OAuth Authorization Code `StateStore` is unified into the same Storage backend, so multi-replica MCP
deployments must use `sqlite` / `redis` / `dynamodb` (the `memory` backend is rejected at startup when `auth=oidc`).

### Backend configuration examples

```bash
# Memory (dev / test)
export KINTONE_STORE_BACKEND=memory

# SQLite (default)
export KINTONE_STORE_BACKEND=sqlite
export KINTONE_STORE_SQLITE_DIR=$HOME/.local/state/kintone

# Redis (k8s / Fargate)
export KINTONE_STORE_BACKEND=redis
export KINTONE_STORE_REDIS_URL=rediss://prod-redis.example.com:6380/0
export KINTONE_STORE_REDIS_PASSWORD=$REDIS_PASSWORD

# DynamoDB (Lambda / serverless)
export KINTONE_STORE_BACKEND=dynamodb
export KINTONE_STORE_DYNAMODB_TABLE=kintone-prod
export KINTONE_STORE_DYNAMODB_REGION=ap-northeast-1
```

### DynamoDB setup

Create the table before first use:

```bash
aws dynamodb create-table --table-name kintone-prod \
  --attribute-definitions \
    AttributeName=pk,AttributeType=S \
    AttributeName=gsi1pk,AttributeType=S AttributeName=gsi1sk,AttributeType=S \
    AttributeName=gsi2pk,AttributeType=S AttributeName=gsi2sk,AttributeType=S \
  --key-schema AttributeName=pk,KeyType=HASH \
  --global-secondary-indexes '[
    {"IndexName":"gsi1","KeySchema":[{"AttributeName":"gsi1pk","KeyType":"HASH"},{"AttributeName":"gsi1sk","KeyType":"RANGE"}],"Projection":{"ProjectionType":"ALL"}},
    {"IndexName":"gsi2","KeySchema":[{"AttributeName":"gsi2pk","KeyType":"HASH"},{"AttributeName":"gsi2sk","KeyType":"RANGE"}],"Projection":{"ProjectionType":"KEYS_ONLY"}}
  ]' \
  --billing-mode PAY_PER_REQUEST

aws dynamodb update-time-to-live --table-name kintone-prod \
  --time-to-live-specification 'Enabled=true,AttributeName=ttl'

# Verify with kintone CLI:
kintone store init dynamodb --table kintone-prod --region ap-northeast-1
```

Minimum IAM policy (no Scan required; Query/BatchWriteItem only):

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": [
      "dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:DeleteItem",
      "dynamodb:Query", "dynamodb:BatchWriteItem", "dynamodb:DescribeTable",
      "dynamodb:DescribeTimeToLive"
    ],
    "Resource": [
      "arn:aws:dynamodb:*:*:table/kintone-prod",
      "arn:aws:dynamodb:*:*:table/kintone-prod/index/*"
    ]
  }]
}
```

### Redis recommended ACL (minimum privilege)

```
ACL SETUSER kintone on >password \
    ~kintone:* ~idproxy:* \
    +@read +@write +@string +@hash +@scripting -@admin -@dangerous
```

### MCP secrets

`KINTONE_MCP_COOKIE_SECRET` (cookie encryption) and `KINTONE_MCP_SIGNING_KEY_PEM` (OIDC JWT signing key) are
managed independently from Storage. When `auth=oidc` is set and the SigningKey cannot be resolved, startup is rejected.

For dev/test, set `KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1` to allow auto-generation in Storage
(not recommended for production; prefer supplying `KINTONE_MCP_SIGNING_KEY_PEM` externally).

### Multi-user MCP principal_id convention

In OIDC mode, each user's principal_id is stored in the format `<issuer>:<sub>`
(e.g. `https://accounts.google.com:1234567890`). The server-hosted OAuth callback (M13)
maps the OIDC `sub` claim into the TokenStore key automatically — no manual provisioning is needed.

### Memory backend restriction

Combining `KINTONE_STORE_BACKEND=memory` with `--auth oidc` is rejected at startup
(`STORE_MEMORY_OIDC_FORBIDDEN`). The memory backend loses state on process restart and
sessions become orphaned in multi-replica deployments. To test `auth=oidc` in dev,
use `KINTONE_STORE_BACKEND=sqlite` with `KINTONE_STORE_SQLITE_DIR=$(mktemp -d)` instead.

## API Token Authentication

Specify the API Token via the `KINTONE_API_TOKEN` environment variable or the `api_token` field in config.toml.
The `X-Cybozu-API-Token` header is attached automatically.

For how to issue an API Token, see [kintone REST API Common Spec — Authentication](https://cybozu.dev/en/kintone/docs/rest-api/overview/authentication/).

## OAuth Authentication (Remote MCP server only)

kintone OAuth 2.0 Authorization Code Grant + PKCE (S256) is supported, but
**as of v0.3.0 the local CLI OAuth login flow has been removed**.

Reason: kintone OAuth requires `https` for the redirect URI (loopback http is rejected).
Running a self-signed https loopback server from a CLI binary degrades UX and does not
fit our distribution model, so OAuth is now restricted to **remote MCP servers only**.

### Auth model

| Use case | Authentication |
|---|---|
| Local CLI (`api`/`ops`/`mcp serve` stdio) | API Token |
| Remote MCP server (`mcp serve --listen ...`) | OAuth (server-hosted callback, available since M13) |

### Server-hosted OAuth flow (M13)

For a remote MCP server published at `https://mcp.example.com`, register the OAuth client as:

- **redirect URI**: `https://mcp.example.com/oauth/kintone/callback` (public https / shared by all users / exact match)
- **scopes**: `k:app_record:read k:app_record:write k:app_settings:read k:app_settings:write k:file:read k:file:write`
- **Step-by-step guide**: [cybozu developer network — Adding an OAuth client](https://cybozu.dev/en/common/docs/oauth-client/add-client/)

Required environment variables on the MCP server:

- `KINTONE_OAUTH_CLIENT_ID` / `KINTONE_OAUTH_CLIENT_SECRET`
- `KINTONE_OAUTH_REDIRECT_URL` (HTTPS, must equal `KINTONE_MCP_EXTERNAL_URL + /oauth/kintone/callback`)
- `KINTONE_MCP_EXTERNAL_URL` (shared with idproxy)
- Optional: `KINTONE_OAUTH_SCOPES`
- Optional (dev only): `KINTONE_OAUTH_ALLOW_PLAINTEXT_REDIRECT=1` permits `http://localhost`

Endpoints (`auth=oidc + authz=oauth`):

| Path | Purpose |
|------|---------|
| `GET /oauth/kintone/start` | OIDC-authenticated user is 302-redirected to kintone authorize URL with PKCE S256 + state cookie |
| `GET /oauth/kintone/callback` | Exchanges authorization code → token, stores in TokenStore (`Domain + PrincipalID + AuthType=oauth`) |

Flow:

1. User signs in to the MCP server with OIDC (`--auth oidc`)
2. When an MCP tool call detects the user has no kintone token, the server returns an `AUTH_REQUIRED` envelope containing `details.authorize_url`
3. The LLM client surfaces the URL to the user; the user opens it in a browser
4. The MCP server's `/oauth/kintone/start` redirects to kintone's authorize endpoint
5. After kintone approval, `/oauth/kintone/callback` exchanges the code and stores the access/refresh tokens
6. Subsequent MCP calls go through `PrincipalAPIFactory`, which fetches the per-user token automatically; refresh is performed transparently

Security:

- CSRF triple-check: idproxy OIDC Principal + `kintone_oauth_state` cookie + state-map PrincipalID match
- state TTL: 10 minutes, one-shot consumption (OAuth 2.0 spec)
- state map is unified into the Storage backend (memory / sqlite / redis / dynamodb) since M14, so multi-replica MCP deployments are fully supported (the `memory` backend is rejected at startup when `auth=oidc`)

> Single domain per server (multi-domain routing is a future enhancement).

> The CLI no longer offers a way to obtain OAuth tokens directly. `kintone auth status` / `kintone auth logout` remain as auxiliary tools that inspect or delete tokens stored by the remote MCP server.

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
- `kintone.db` (SQLite backend) is stored with permission 0600 (plaintext)
- PKCE (S256) and CSRF state verification are performed (`crypto/rand` for generation, `subtle.ConstantTimeCompare` for verification)

## API Subcommands (`kintone api ...`)

Transparent pass-through commands for the kintone REST API. Output is always JSON, designed for LLM / `jq` integration.

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

# Specify App by code / name / partial (name resolution)
$ kintone api records get --app-ref sales --query 'createdAt > LAST_WEEK()'
{"ok":true,"data":{"records":[{...}]}}
```

| Flag | Type | Required | Description |
|--------|---|------|------|
| `--app` | int64 | one of | kintone app ID (numeric, mutually exclusive with `--app-ref`) |
| `--app-ref` | string | one of | App reference (numeric string / code / name / partial, mutually exclusive with `--app`) |
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

### App + fields combined

```bash
$ kintone api app describe --app 1 --lang ja
{"ok":true,"data":{"app":{"app_id":"1","name":"テスト",...},"fields":{...},"revision":"5"}}
```

Combines `app.json` and `app/form/fields.json` so an LLM can understand the full app structure in a single call.

## Ops Subcommands (`kintone ops ...`)

Semantically enriched record CRUD and app description for LLMs. Write operations support `--dry-run` to inspect the request body before sending.

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
| `--app-ref` | string | one of | App reference (numeric string / code / name / partial, mutually exclusive with `--app`) |
| `--record-json` | string | one of | Single record JSON |
| `--records-json` | string | one of | Multiple records JSON array |
| `--dry-run` | bool | - | Output the request body without calling the API |

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
| `--app-ref` | string | one of | App reference (numeric string / code / name / partial, mutually exclusive with `--app`) |
| `--id` | int64 | one of | Target record ID |
| `--update-key-field` | string | one of | updateKey: field code (mutually exclusive with `--update-key-field-ref`) |
| `--update-key-field-ref` | string | one of | updateKey: field reference (label / partial) |
| `--update-key-value` | string | one of | updateKey: value (required when using updateKey path) |
| `--record-json` | string | required | Update content JSON |
| `--revision` | int64 | - | Revision for optimistic locking |
| `--dry-run` | bool | - | Output request body without calling the API |

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
| `--app-ref` | string | one of | App reference (numeric string / code / name / partial, mutually exclusive with `--app`) |
| `--id` | int64 (repeatable) | required | Target record ID(s) (`--id 1 --id 2`) |
| `--revision` | int64 (repeatable) | - | Revisions for optimistic locking (same count as `--id`) |
| `--dry-run` | bool | - | Output request body without calling the API |

### App describe (also available under ops)

Equivalent to `kintone api app describe`. Re-published under the `ops` namespace so LLMs can discover it there.

```bash
$ kintone ops app describe --app 1 --lang ja
{"ok":true,"data":{"app":{"app_id":"1","name":"テスト",...},"fields":{...},"revision":"5"}}
```

## Name Resolution (Resolver)

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

The resolver internally calls `ListApps` / `GetFormFields` through the SQLite cache.
With a 1-year TTL, the same ref only triggers one REST call regardless of how many times it is resolved.

To pick up renames, run `kintone cache clear` to force a refresh.

## Cache Management (`kintone cache ...`)

Caches kintone API app / field information in SQLite to reduce repeated requests.

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

### Supported combinations (M15)

| Transport | `--auth` | `--authz`   | Status                                                      |
|-----------|----------|-------------|-------------------------------------------------------------|
| stdio     | `none`   | `api-token` | OK (default)                                                |
| stdio     | `none`   | `oauth`     | **USAGE error** (OAuth requires HTTP for per-request principal binding) |
| stdio     | `oidc`   | any         | USAGE error (OIDC requires HTTP transport)                  |
| HTTP      | `none`   | `api-token` | OK (trusted LAN)                                            |
| HTTP      | `oidc`   | `api-token` | OK (multi-user with shared API Token)                       |
| HTTP      | `oidc`   | `oauth`     | OK (multi-user with per-user kintone OAuth)                 |

### stdio + API Token

```bash
$ KINTONE_DOMAIN=example.cybozu.com \
  KINTONE_AUTH=api-token \
  KINTONE_API_TOKEN=xxxx \
  kintone mcp serve
```

### HTTP + OIDC + multi-user

When `auth=oidc`, [github.com/youyo/idproxy](https://github.com/youyo/idproxy) is embedded.
It validates an OIDC-based Bearer JWT per request and extracts `principal_id = "<issuer>:<sub>"`.
Upstream kintone OAuth tokens are obtained on demand via the server-hosted OAuth callback flow
(planned in M13): the user is redirected to kintone for consent on first MCP call, and the server
saves the resulting token into the TokenStore keyed by the OIDC `sub`.

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

> The idproxy SigningKey is resolved from `KINTONE_MCP_SIGNING_KEY_PEM` (env) or persisted in Storage. When `auth=oidc` and the key cannot be resolved, startup is rejected (`SIGNING_KEY_REQUIRED`). For dev, set `KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1` to allow auto-generation.

### Six tools provided

| Tool | Description |
|---------|------|
| `apps_search` | Search apps by ids/codes/name/space_ids/limit/offset |
| `app_describe` | Get basic app info + form field definitions (`app` or `app_ref`) |
| `records_query` | Query records using kintone query language (query / fields / total_count, `app` or `app_ref`) |
| `record_create` | Create records (record / records mutually exclusive, up to 100, `app` or `app_ref`) |
| `record_update` | Update a single record (id / update_key_* mutually exclusive, optimistic locking, `app`+`app_ref`, `update_key_field`+`update_key_field_ref` mutually exclusive) |
| `record_delete` | Delete multiple records (optional revisions, `app` or `app_ref`) |

All tools except `apps_search` accept either `app: number` or `app_ref: string` (mutually exclusive, exactly one required).
`record_update` also accepts `update_key_field_ref: string` (resolves field code by label / partial).

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

> stdio mode supports only `api-token` authentication. `authz=oauth` on stdio is
> rejected at startup with a USAGE error (OAuth requires per-request principal
> binding which only HTTP transport provides). Use `--listen <addr> --auth oidc
> --authz oauth` for multi-user OAuth, described above.

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

Exceptions: `kintone completion` and `kintone version --short` emit plain strings for shell / human consumption.

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

## License

[MIT License](LICENSE)
