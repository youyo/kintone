package store

import "time"

// AuthType は kintone 認証方式（API トークン / OAuth）を表す。
type AuthType string

const (
	// AuthTypeAPIToken は API トークン認証。PrincipalID は空文字列。
	AuthTypeAPIToken AuthType = "api-token"
	// AuthTypeOAuth は OAuth 2.0 認証。PrincipalID は "provider:sub" 形式。
	AuthTypeOAuth AuthType = "oauth"
)

// Token は認証情報の永続化単位。
//
// APIToken / AccessToken / RefreshToken はどれか 1 つ以上が設定される。
// ログ出力時は "***" でマスクし、平文で外部に出さない。
type Token struct {
	Domain       string
	PrincipalID  string
	AuthType     AuthType
	APIToken     string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	UpdatedAt    time.Time
}

// Stats は Cache / Token / SigningKey サブストアの統計値の共通型。
//
// JSON タグは `kintone cache stats` などのコマンドで直接利用される。
type Stats struct {
	Backend         string         `json:"backend"`
	Location        string         `json:"location"`
	Reachable       bool           `json:"reachable"`
	EntryCount      int64          `json:"entry_count"`
	ExpiredCount    *int64         `json:"expired_count"`
	BackendSpecific map[string]any `json:"backend_specific,omitempty"`
}
