// Package redis は store パッケージの Redis backend 実装。
//
// kintone 単一プロセスから redis.UniversalClient を 1 つだけ生成し、
// その client を kintone（prefix: "kintone:"）と idproxy（prefix: "idproxy:"）の
// 両方の sub-store で共有する。client の Close は Container が所有・管理する。
//
// セキュリティ:
//   - rediss:// scheme は常に TLS 接続（KINTONE_STORE_REDIS_TLS=0 でも downgrade 不可）
//   - redis:// scheme + KINTONE_STORE_REDIS_TLS=true は upgrade して TLS 接続
//   - 非 loopback への redis:// 平文接続は KINTONE_STORE_REDIS_INSECURE_PLAINTEXT=1
//     のオプトインがない限り [store.ErrPlaintextForbidden] で拒否する
package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/store"
)

// pingTimeout は startup 時の疎通確認 Ping のタイムアウト。
const pingTimeout = 5 * time.Second

// Open は store.Opener として呼び出される Redis backend のエントリーポイント。
//
// cfg.RedisURL が空の場合は loopback 既定（"redis://127.0.0.1:6379/0"）を使う。
// TLS 制御 / loopback 判定 / Ping 確認を行い、Container を返す。
func Open(cfg *store.Config) (store.Container, error) {
	if cfg == nil {
		return nil, errors.New("store/redis: nil config")
	}
	rawURL := cfg.RedisURL
	if rawURL == "" {
		rawURL = "redis://127.0.0.1:6379/0"
	}

	opts, err := goredis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("store/redis: parse URL: %w", err)
	}

	// TLS 制御:
	//   - rediss:// で ParseURL は既に TLSConfig を設定する
	//   - redis:// + cfg.RedisTLS=true → TLS upgrade
	//   - rediss:// × cfg.RedisTLS=false → TLS 維持 + WARN
	scheme := strings.ToLower(schemeOf(rawURL))
	switch {
	case scheme == "rediss":
		if !cfg.RedisTLS {
			output.Logger().Warn(
				"ignoring KINTONE_STORE_REDIS_TLS=0 for rediss:// URL",
				"location", output.SanitizeURL(rawURL),
			)
		}
		// rediss:// は ParseURL が TLSConfig を設定済み。downgrade はしない。
		if opts.TLSConfig == nil {
			opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
	case scheme == "redis" && cfg.RedisTLS:
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	// Password override（環境変数指定があれば優先）
	if cfg.RedisPassword != "" {
		opts.Password = cfg.RedisPassword
	}

	// 平文 redis:// で非 loopback への接続を制限
	if scheme == "redis" && opts.TLSConfig == nil {
		if !isLoopback(opts.Addr) && !cfg.RedisInsecurePlaintext {
			return nil, fmt.Errorf("%w: redis:// to non-loopback %q requires KINTONE_STORE_REDIS_INSECURE_PLAINTEXT=1 (or use rediss://)", store.ErrPlaintextForbidden, opts.Addr)
		}
	}

	client := goredis.NewClient(opts)

	// 起動時の疎通確認
	pingCtx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("%w: %v", store.ErrConnectionFailed, err)
	}

	return NewContainer(client, rawURL), nil
}

// schemeOf は URL の scheme 部分を返す。"redis://..." → "redis"。
// 失敗時は空文字列。
func schemeOf(rawURL string) string {
	idx := strings.Index(rawURL, "://")
	if idx <= 0 {
		return ""
	}
	return rawURL[:idx]
}

// isLoopback は addr ("host:port" 形式) が loopback アドレスを指すかを判定する。
//
// "127.0.0.1" / "::1" / "localhost" のいずれかを含む host を loopback と扱う。
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
