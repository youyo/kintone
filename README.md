# kintone MCP

> **Cloudflare Workers 版へ作り直し中です。** 以前の Go 製 CLI + MCP サーバー(v0.5.6)は `archive/go` ブランチ・タグに保存しています。
> 完全な README(製品説明・移行案内・設定手順)は M05 で書きます。

kintone を操作する Remote MCP サーバー(Cloudflare Workers)。MCP クライアント(Cloudflare MCP Portal など)が OAuth で接続し、
**利用者ごとの kintone OAuth で認可された身元**で kintone REST API を呼びます。

- 仕様: `docs/specs/kintone-mcp-spec.md`、設計判断: `docs/adr/`、進め方: `plans/kintone-mcp-roadmap.md`
- 現在の状態(M01): 認可の骨格のみ。ツールは `kintone_apps_search`(`limit` だけ)の 1 つ
- 利用者の識別子は **Cloudflare Access の email(必須)**。Access の self-hosted アプリを作り、保護するパスは **`/authorize` と `/callback` の 2 つだけ**(`/mcp` `/token` `/register` `/.well-known/*` `/healthz` は含めない。Access の Managed OAuth は使わない)。アプリの **AUD タグ**と**チームドメイン**を `ACCESS_AUD` / `ACCESS_TEAM_DOMAIN` に設定する(どちらも必須。未設定の Worker は動かない)
- kintone に登録するリダイレクト URI は `<PUBLIC_BASE_URL>/callback`

## 開発

```sh
mise install
mise run install   # 依存をロックファイルどおりに入れる
mise run lint
mise run test      # unit + e2e (kintone と Access の JWKS は fetch のモック)
mise run build     # wrangler deploy --dry-run
```

ライセンス: MIT
