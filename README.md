# kintone MCP

English | [日本語](README_ja.md)

A **Remote MCP server** for kintone, running on Cloudflare Workers. MCP clients (Claude, Cloudflare MCP Portal, and others) connect over OAuth, and the server calls the kintone REST API **as the individual user, using that user's own kintone OAuth authorization**. You can search apps, read field definitions, and query, create, update, and delete records.

> **If you used the Go version (CLI + MCP server, up to v0.5.6)**: this release is a rewrite and contains **breaking changes**. The CLI has been discontinued. See [Notice for Go version users](#notice-for-go-version-users).

> **Note on documentation language**: the documents linked from this README (the runbook, the specification, the ADRs, the manual-verification notes, and the setup skill) are written in Japanese.

- Per-user permissions: kintone tokens are issued per user through OAuth, so each person sees only the apps and records their own kintone permissions allow. No shared API token is used.
- User identity and entry-point protection: users are identified by the email in **Cloudflare Access** (a self-hosted application). Access protects only the authorization entry points, `/authorize` and `/callback`.
- Address apps by name: besides the app ID, you can specify an app by its app code or app name (reads also accept a partial name match). Writes accept **exact matches only**, to prevent hitting the wrong app.
- Write safeguards: writes are never retried, input is validated before anything is sent, and a resend of `kintone_record_create` within 5 minutes does not create duplicates.
- Stateless MCP (no Durable Objects). The only storage is Cloudflare KV.

## Tools

The server provides **6 tools** in two toolsets (`app` and `record`). Every tool name starts with `kintone_`.

| toolset | count | tools |
|---|---|---|
| `app` | 2 | `kintone_apps_search`, `kintone_app_describe` |
| `record` | 4 | `kintone_records_query`, `kintone_record_create`, `kintone_record_update`, `kintone_record_delete` |

| tool | category | description |
|---|---|---|
| `kintone_apps_search` | ReadOnly | Search apps by ID, code, name, or space. Returns one page per call. |
| `kintone_app_describe` | ReadOnly | Get an app's information and its field definitions. |
| `kintone_records_query` | ReadOnly | Query records with a kintone query string (put `limit` / `offset` in the query; the total count is also available). |
| `kintone_record_create` | WriteNonIdempotent | Create 1 to 100 records. A resend with the same arguments within 5 minutes returns the previous result. |
| `kintone_record_update` | WriteIdempotent | Update a record, identified by its ID or by the value of a unique field. Revision-based optimistic locking is supported. |
| `kintone_record_delete` | Destructive | Delete 1 to 100 records. |

- The category is one of the four MCP annotation combinations (`readOnlyHint` / `destructiveHint` / `idempotentHint`).
- Read tools accept `max_response_tokens` (default 50000). If a response would exceed it, the server does not truncate; it returns `response_too_large`.
- Specify the app with exactly one of `app` (ID) or `app_ref` (ID, code, or name). For writes, `app_ref` accepts exact matches only; if only a partial match exists, the server returns the candidates and performs no write.
- You can narrow the tools with a query string on the connection URL: `https://<domain>/mcp?toolsets=app,record`, or `?read_only=true` (read tools only).
- Output is JSON in a text content block. Errors are returned with `isError: true` and `{"schema_version":"1","error":{"code":"...","message":"...","retryable":...}}`. The full specification is [docs/specs/kintone-mcp-spec.md](docs/specs/kintone-mcp-spec.md) (in Japanese).
- Each deployment targets a single cybozu.com host (one deployment = one host). Apps in guest spaces are not supported.

## Getting started (for operators)

The procedure for running this on your own Cloudflare account and cybozu.com is written in **[docs/runbook.md](docs/runbook.md)** (in Japanese), to be followed from top to bottom. You will need:

1. A publicly reachable hostname (a custom domain, or `workers.dev`)
2. An **OAuth client** registered in the cybozu.com administration console (the redirect URI is `https://<domain>/callback`)
3. A Cloudflare Access **self-hosted application** (it protects only two paths: `/authorize` and `/callback`)
4. A Cloudflare KV namespace, plus GitHub Actions Variables and Secrets

To have an agent (Claude Code / Codex) walk you through it, use the skill `kintone-mcp-setup` (`.claude/skills/kintone-mcp-setup/SKILL.md`, written in Japanese). It guides you from creating the downstream repository (fork or mirror). You can start by telling an agent opened in a clone of this repository (the upstream) that you want to run kintone MCP for your organization. When you start a new session in the directory of your downstream repository, send this first message (the skill is written in Japanese, so the message is too):

```text
このリポジトリは kintone MCP の下流リポジトリです。`.claude/skills/kintone-mcp-setup/SKILL.md` のスキル kintone-mcp-setup に従って、Step 0 から一緒にセットアップを進めてください。秘密値は私が自分のターミナルで入れます。
```

(In English: "This repository is a downstream repository of kintone MCP. Follow the kintone-mcp-setup skill in `.claude/skills/kintone-mcp-setup/SKILL.md` and walk me through the setup from Step 0. I will enter secret values myself in my own terminal.")

This repository is the **upstream**. `wrangler.jsonc` stays a shared configuration with dummy values, and no account-specific values (Worker name, KV id, domain, kintone host, Access values, client id / secret) are ever written to it. A downstream repository (a fork or a mirror) generates `wrangler.deploy.jsonc` from its GitHub Variables and Secrets and deploys from there (`mise run deploy:render` / `mise run deploy`; the design is in [ADR 0012](docs/adr/0012-upstream-downstream-deploy.md), in Japanese). **While the Variable `KINTONE_MCP_WORKER_NAME` is empty, as it is in the upstream, the deploy job does nothing.**

## How it works

```
MCP client ── OAuth 2.1 (DCR, PKCE) and MCP ──▶ Cloudflare Worker ──▶ https://<subdomain>.cybozu.com/k/v1/...
                 * only /authorize and /callback, which the browser visits, go through Cloudflare Access
```

The Worker is both an OAuth authorization server and an MCP server (`@cloudflare/workers-oauth-provider` + Hono). When a user connects for the first time, they sign in to Access in the browser and then consent on kintone's authorization screen. The Worker encrypts the kintone tokens and stores them in KV, and hands the MCP client a separate token issued by the Worker itself. For the detailed design, see [docs/adr/](docs/adr/); for the specification, [docs/specs/kintone-mcp-spec.md](docs/specs/kintone-mcp-spec.md); for the assumptions that can only be confirmed on a real environment, [docs/manual-verification.md](docs/manual-verification.md). All of these are in Japanese.

Known limitations (an excerpt; the full list is §11 of the specification):

- The email from Access identifies "the person who signed in to Access", which is not necessarily the account authorized in kintone (kintone has no API that returns the owner of a token). Caches and similar state are therefore separated per grant.
- If your cybozu.com environment uses IP address restrictions, you probably cannot use this server, because the Worker's outbound IP addresses cannot be allowlisted.
- `kintone_records_query` does not paginate automatically (`offset` is limited to 10,000).
- The idempotency cache works within the limits of KV's eventual consistency, so two nearly simultaneous calls can both be executed.

## Notice for Go version users

The previous kintone CLI + MCP server (written in Go, up to v0.5.6) has been **rewritten** in this release. It is now a single Remote MCP server on Cloudflare Workers, and the **CLI has been discontinued**.

**The Go version remains available.**

- The source code is preserved on the **`archive/go` branch and tag** (`git checkout archive/go`).
- **The v0.5.6 Homebrew formula (`brew install youyo/tap/kintone`) and the ghcr image (`ghcr.io/youyo/kintone`) keep working as they are.** There is no need to hurry your migration to this release.

If you move to the Workers version, be aware of the following **breaking changes**.

| Item | Go version (up to v0.5.6) | Workers version (this release) |
|---|---|---|
| Form of delivery | CLI (`kintone ...`) + MCP server (stdio / HTTP). Homebrew, Docker, `go install` | **Remote MCP server only** (Cloudflare Workers). The CLI, stdio, Docker, and Homebrew are **discontinued** |
| Tool names | `apps_search` `app_describe` `records_query` `record_create` `record_update` `record_delete` | **Prefixed with `kintone_`**: `kintone_apps_search` `kintone_app_describe` `kintone_records_query` `kintone_record_create` `kintone_record_update` `kintone_record_delete`. Argument names follow the Go version (`app` / `app_ref`, and so on) |
| Output format | Success: `{"ok":true,"data":{...}}`. Failure: `{"ok":false,"error":{...}}` | Success has **no `ok` / `data` wrapper; the output is the kintone data itself** (anything derived is in snake_case). Failure is **`isError: true`** with `{"schema_version":"1","error":{"code","message","retryable","details"?}}`. Clients that assume `{"ok":...}` will break |
| Error codes | Uppercase (`KINTONE_UNAUTHORIZED`, `RESOLVER_APP_NOT_FOUND`, and so on) | **Codes are now lowercase snake_case** (`authentication_error`, `permission_error`, `not_found`, `argument_error`, `rate_limited`, `api_error`, `resolver_app_not_found`, and so on; the full list is in §6 of the specification). The mapping from Go version codes is the "Go version code" column of the table in §6. There are also new codes, such as `resolver_app_exact_match_required`, returned when a write's `app_ref` matches only partially |
| Authentication | API token / OAuth (the CLI loopback is already removed) / OIDC (idproxy). Tokens stored in SQLite, Redis, or DynamoDB | **Per-user kintone OAuth** (the Worker is the OAuth authorization server; tokens are encrypted and stored in Cloudflare KV) plus **user identification by the Cloudflare Access email** (`/authorize` and `/callback` are protected by an Access self-hosted application). OIDC (idproxy) is discontinued |
| **API token** | `X-Cybozu-API-Token` (`KINTONE_API_TOKEN`) | **Discontinued.** A shared API token cannot operate the server; each user authorizes through kintone OAuth |
| OAuth redirect URI | `<external URL>/oauth/kintone/callback` | **`<PUBLIC_BASE_URL>/callback`**. You must register the OAuth client again (or change its URI) in cybozu.com. The scopes are the same six as in the Go version. In the registered OAuth client's "user settings" (STEP2), allow the users who will use it |
| Configuration | `KINTONE_DOMAIN` `KINTONE_API_TOKEN` `KINTONE_AUTH` `KINTONE_OAUTH_CLIENT_ID` / `_SECRET` / `_REDIRECT_URL` `KINTONE_STORE_*` `KINTONE_MCP_*` (OIDC, signing key, cookies) `KINTONE_CONFIG_PATH` `KINTONE_PROFILE`, and `config.toml` | **Cloudflare Variables / Secrets and the wrangler configuration**: `KINTONE_HOST` (the successor of `KINTONE_DOMAIN`; the full host `<subdomain>.cybozu.com`), `PUBLIC_BASE_URL`, `ACCESS_TEAM_DOMAIN`, `ACCESS_AUD`, `KINTONE_CLIENT_ID` / `KINTONE_CLIENT_SECRET` (Secrets), `KINTONE_OAUTH_PKCE` (optional), and the KV binding `OAUTH_KV`. File-based configuration, profiles, and SQLite / Redis / DynamoDB are discontinued. See the settings table in [docs/runbook.md](docs/runbook.md) for the full list |
| Name resolution and caching | The cache is user-independent, has a one-year TTL, and is invalidated with `cache clear`. Name search covers up to 100 pages | The cache is **per user (grant)** with a 600-second TTL. Name search covers up to 10 pages (beyond that, `resolver_app_list_too_large`). **A write's `app_ref` and `update_key_field_ref` accept exact matches only** |
| Writes | Never retried | Never retried (unchanged). `kintone_record_create` does not duplicate a resend of the same arguments within 5 minutes (idempotency cache) |
| Other | | The `?toolsets=` / `?read_only=true` URL query, `max_response_tokens`, guest spaces are out of scope, one deployment = one host |

The Go version's code (`archive/go`) and the old specifications and plans can be traced from this branch's history. For questions about migrating or for bug reports, please open an issue in this repository.

## Development

```sh
mise install
mise run install        # install dependencies exactly as locked
mise run lint           # Biome + type check
mise run test           # unit + e2e (kintone and the Access JWKS are fetch mocks; node-e2e runs the generator script as a real process + wrangler dry-run)
mise run build          # wrangler deploy --dry-run
mise run deploy:check   # generate the deploy configuration and dry-run it (dummy values; changes nothing on Cloudflare)
```

- Conventions: [CLAUDE.md](CLAUDE.md). Plan: [plans/kintone-mcp-roadmap.md](plans/kintone-mcp-roadmap.md) (in Japanese).
- `mise run verify:oauth -- https://<domain>/mcp`: an end-to-end OAuth check against a deployed Worker ([docs/manual-verification.md](docs/manual-verification.md), in Japanese).
- Deploying, registering secrets, and creating the KV namespace are done by the operator following [docs/runbook.md](docs/runbook.md) (tests and agents do not run them).

## Trademarks and disclaimer

**kintone is a trademark (registered trademark) of Cybozu, Inc. This project is unofficial open-source software unaffiliated with Cybozu, Inc., and it is not provided, endorsed, or guaranteed by Cybozu, Inc.** All other company and product names are trademarks or registered trademarks of their respective owners.

## License

MIT
