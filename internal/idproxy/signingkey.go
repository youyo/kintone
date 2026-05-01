// Package idproxy: SigningKey 解決ロジックと backend × authMode の禁止組合せ検証。
//
// Phase 3 では BuildAuth のシグネチャは変更せず、ResolveSigningKey を独立関数として
// 提供する。実際の配線（BuildAuth から呼ぶ）は Phase 6 Wave B で行う。
package idproxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/store"
)

// AuthMode は MCP 認証モード（none / oidc）。
type AuthMode string

// AuthMode 定数。env / CLI からの文字列値と一致する。
const (
	AuthModeNone AuthMode = "none"
	AuthModeOIDC AuthMode = "oidc"
)

// ErrSigningKeyRequired はランタイムフェイルファスト用 sentinel。
//
// auth=oidc 環境で env の KINTONE_MCP_SIGNING_KEY_PEM も
// KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE オプトインも提供されていない場合に返る。
var ErrSigningKeyRequired = errors.New("idproxy: signing key required for auth=oidc")

// ValidateBackendForAuthMode は auth=oidc + memory backend の禁止組合せを検出する。
//
// 返り値が non-nil なら startup を拒否する。store.ErrMemoryOIDCForbidden を errors.Is で判定可能。
// auth=none の場合は backend を問わず nil を返す（memory も許可）。
func ValidateBackendForAuthMode(authMode AuthMode, backend string) error {
	if authMode == AuthModeOIDC && backend == store.BackendMemory {
		return fmt.Errorf("%w: auth=oidc requires non-memory backend (memory state is not durable across process restart)", store.ErrMemoryOIDCForbidden)
	}
	return nil
}

// ResolveSigningKey は env > Storage > ephemeral の順序で SigningKey を解決する。
//
// 引数:
//   - ctx: context
//   - pemEnv: KINTONE_MCP_SIGNING_KEY_PEM の値（空文字列なら未指定扱い）
//   - autoGenerate: KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1 のとき true
//   - authMode: "oidc" / "none"
//   - backend: store backend 名（"memory" / "sqlite" / "redis" / "dynamodb"）
//   - container: store.Container（nil 許容: store を使わない経路で）
//
// 解決順序（各段階でヒットしたら即 return、fallback しない）:
//
//  0. ValidateBackendForAuthMode の先取りチェック（ErrMemoryOIDCForbidden）
//  1. pemEnv != "" → PKCS#8 PEM パース → 採用（パース失敗は fail-fast）
//  2. authMode == "oidc" かつ autoGenerate == false → ErrSigningKeyRequired
//  3. container != nil → container.SigningKey().LoadOrCreate(ctx) → 採用
//     （autoGenerate==true のときのみ slog.Warn で平文永続化を警告）
//  4. container == nil かつ authMode == "none" → ephemeral 生成 + slog.Warn
//  5. それ以外（authMode=="oidc" + container==nil + autoGenerate==true）→ ErrSigningKeyRequired
func ResolveSigningKey(ctx context.Context, pemEnv string, autoGenerate bool, authMode AuthMode, backend string, container store.Container) (*ecdsa.PrivateKey, error) {
	// Step 0: backend × authMode 禁止組合せチェック
	if err := ValidateBackendForAuthMode(authMode, backend); err != nil {
		return nil, err
	}

	// Step 1: env が最優先（PEM 中身はエラーメッセージに含めない）
	if pemEnv != "" {
		key, err := parsePKCS8PEM(pemEnv)
		if err != nil {
			return nil, fmt.Errorf("idproxy: parse KINTONE_MCP_SIGNING_KEY_PEM: %w", err)
		}
		return key, nil
	}

	// Step 2: auth=oidc で env も auto-generate も無いなら fail-fast
	if authMode == AuthModeOIDC && !autoGenerate {
		return nil, ErrSigningKeyRequired
	}

	// Step 3: container があれば LoadOrCreate
	if container != nil {
		sk, err := container.SigningKey()
		if err != nil {
			return nil, fmt.Errorf("idproxy: signing key store: %w", err)
		}
		key, err := sk.LoadOrCreate(ctx)
		if err != nil {
			return nil, fmt.Errorf("idproxy: load or create: %w", err)
		}
		if autoGenerate {
			output.Logger().Warn(
				"auto-generated SigningKey persisted in plaintext (set KINTONE_MCP_SIGNING_KEY_PEM for production)",
				"backend", backend,
			)
		}
		return key, nil
	}

	// Step 4: container nil + auth=none → ephemeral
	if authMode == AuthModeNone {
		output.Logger().Warn(
			"ephemeral SigningKey (auth=none, no container, key will not survive restart)",
		)
		return generateEphemeral()
	}

	// Step 5: それ以外（authMode=oidc + container nil）→ fail-fast
	return nil, ErrSigningKeyRequired
}

// parsePKCS8PEM は PEM bytes を解析し ECDSA 秘密鍵を返す。
// PKCS#8（PRIVATE KEY ブロック）を一次試行し、失敗したら EC PRIVATE KEY フォールバックを試す。
//
// エラーメッセージには PEM の中身を含めない（鍵情報の leak 防止）。
func parsePKCS8PEM(pemStr string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("invalid PEM (no block)")
	}
	// PKCS#8 を一次試行
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ec, ok := k.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("PKCS#8 key is not ECDSA")
		}
		return ec, nil
	}
	// EC PRIVATE KEY フォールバック
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, errors.New("unsupported PEM key format (expected PKCS#8 or EC PRIVATE KEY)")
}

// generateEphemeral は ES256 (P-256) 鍵を新規生成して返す。
func generateEphemeral() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}
