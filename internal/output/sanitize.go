package output

import (
	"net/url"
	"strings"
)

// SanitizeURL は URL の userinfo password・クエリ password を *** にマスクする。
//
// 主な用途は Redis URL（例: `redis://user:secret@host:6379/0`）や
// DynamoDB endpoint URL の Location 表示で、ログ・JSON envelope に出す前に
// クレデンシャルを取り除くこと。
//
// 規則:
//   - userinfo に password がある場合: "user:secret@..." → "user:***@..."
//   - userinfo が username のみの場合: そのまま保持
//   - クエリ "password=xxx" がある場合: "***" に置換
//   - URL パースに失敗したら元文字列をそのまま返す（best-effort）
//
// 注意: url.URL.String() は userinfo の特殊文字を escape するため、`***` のような
// マーカ文字を含めると `%2A%2A%2A` に変換されてしまう。本実装では String() の
// 出力後に "%2A%2A%2A" → "***" を文字列置換することで「人間が読める」
// マスク表現を保つ（ホスト・パスにこの sequence が含まれる正規 URL は事実上ない）。
func SanitizeURL(raw string) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	masked := false
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "***")
			masked = true
		}
	}
	if q := u.Query(); q.Get("password") != "" {
		q.Set("password", "***")
		u.RawQuery = q.Encode()
		masked = true
	}
	out := u.String()
	if masked {
		// url.URL.String() は "*" を %2A に escape するため、可読化のため逆変換する
		out = strings.ReplaceAll(out, "%2A%2A%2A", "***")
	}
	return out
}
