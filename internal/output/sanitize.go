package output

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
)

// ClassifyBackendError は backend エラーを cause_class 文字列に分類する。
//
// raw error message は出力しない（資格情報漏洩防止）。
// 返り値は "network" / "auth" / "timeout" / "unknown" のいずれか。
//
// 判定優先順位:
//  1. context.DeadlineExceeded → "timeout"
//  2. *net.OpError → "network"
//  3. エラーメッセージ文字列マッチ（大文字小文字無視）
//     - "noauth" / "wrongpass" / "access denied" / "unauthorized" → "auth"
//     - "timeout" / "deadline" → "timeout"
//     - "connection refused" / "no such host" / "network" → "network"
//  4. その他 → "unknown"
func ClassifyBackendError(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return "network"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "noauth") || strings.Contains(msg, "wrongpass") ||
		strings.Contains(msg, "access denied") || strings.Contains(msg, "unauthorized"):
		return "auth"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "timeout"
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network"):
		return "network"
	}
	return "unknown"
}

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
