// Package idproxy は github.com/youyo/idproxy v0.4.2 の thin wrapper を提供する。
//
// kintone MCP サーバーが OIDC ベースの multi-user 化を実現するため、
// idproxy.Auth.Wrap によるリクエスト認証ミドルウェアを束ね、
// 認証済みユーザーを kintone 内部の Principal 型に正規化して context へ注入する。
//
// principal_id の構成: User.Issuer + ":" + User.Subject（仕様 provider:sub に準拠）。
//
// Public API:
//   - Env: 環境変数→idproxy.Config 構築用パラメータ
//   - BuildAuth: idproxy.Auth を構築
//   - Principal / WithPrincipal / FromContext: kintone 側の認証済みユーザー context
//   - Middleware: idproxy.Wrap 後段で User → Principal 変換
package idproxy
