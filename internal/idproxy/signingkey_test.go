package idproxy_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/store"
	memorystore "github.com/youyo/kintone/internal/store/memory"
	sqlitestore "github.com/youyo/kintone/internal/store/sqlite"
)

// helper: PKCS#8 PEM を生成
func newPKCS8PEM(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	return k, pemStr
}

// helper: SEC1 EC PRIVATE KEY PEM を生成
func newECPrivateKeyPEM(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatalf("marshal ec: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	return k, pemStr
}

// helper: log を bytes.Buffer に capture（プロセスグローバル logger を差し替え）。
// 並列実行不可。restore は defer で呼ぶこと。
func captureLog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	// debug までキャプチャ（warn 検証用）
	lg := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	restore := output.SetForTest(lg)
	return &buf, restore
}

// keyBytes は ecdsa.PrivateKey の生バイトを取り出す（鍵比較用）。
// Go 1.26 で D フィールドが deprecated になったため、ecdsa.PrivateKey.Bytes() を利用する。
func keyBytes(t *testing.T, k *ecdsa.PrivateKey) []byte {
	t.Helper()
	b, err := k.Bytes()
	if err != nil {
		t.Fatalf("PrivateKey.Bytes: %v", err)
	}
	return b
}

// 1. env 優先: pemEnv 設定時は container を呼ばない。返却 key の bytes が PEM 元と一致する。
func TestResolveSigningKey_EnvWins(t *testing.T) {
	want, pemStr := newPKCS8PEM(t)

	// container には別の鍵を入れて、env が優先されることを確認
	c := memorystore.New(0)
	defer func() { _ = c.Close(context.Background()) }()
	sk, err := c.SigningKey()
	if err != nil {
		t.Fatalf("c.SigningKey: %v", err)
	}
	other, err := sk.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}

	// 念のため auth=none + memory 経路（auth=oidc + memory は禁止）
	got, err := idproxy.ResolveSigningKey(
		context.Background(), pemStr, false, idproxy.AuthModeNone, store.BackendMemory, c,
	)
	if err != nil {
		t.Fatalf("ResolveSigningKey: %v", err)
	}
	if !bytes.Equal(keyBytes(t, got), keyBytes(t, want)) {
		t.Errorf("env key not adopted")
	}
	if bytes.Equal(keyBytes(t, got), keyBytes(t, other)) {
		t.Errorf("container key adopted, but env should win")
	}
}

// 2. PEM パース失敗: 不正 PEM で error 返却。エラーメッセージに PEM の中身が含まれない。
func TestResolveSigningKey_InvalidPEM(t *testing.T) {
	// 「BEGIN PRIVATE KEY」を含むが本文が壊れた PEM
	badPEM := "-----BEGIN PRIVATE KEY-----\nbm90LWEtdmFsaWQta2V5\n-----END PRIVATE KEY-----\n"
	_, err := idproxy.ResolveSigningKey(
		context.Background(), badPEM, false, idproxy.AuthModeNone, store.BackendMemory, nil,
	)
	if err == nil {
		t.Fatalf("expected error for invalid PEM, got nil")
	}
	// PEM の中身（base64 ペイロードや BEGIN/END マーカー）がエラーに含まれてはいけない
	if strings.Contains(err.Error(), "BEGIN PRIVATE KEY") {
		t.Errorf("error message leaks PEM marker: %v", err)
	}
	if strings.Contains(err.Error(), "bm90LWEtdmFsaWQta2V5") {
		t.Errorf("error message leaks PEM payload: %v", err)
	}
}

// 3. auth=oidc + memory backend: ErrMemoryOIDCForbidden（env 有無や container 有無に関わらず）
func TestResolveSigningKey_OIDC_MemoryForbidden(t *testing.T) {
	cases := []struct {
		name      string
		pemEnv    string
		container store.Container
	}{
		{"no env, no container", "", nil},
		{"with env, no container", "PEM-DATA", nil},
		{"no env, with container", "", memorystore.New(0)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if tc.container != nil {
					_ = tc.container.Close(context.Background())
				}
			}()
			_, err := idproxy.ResolveSigningKey(
				context.Background(), tc.pemEnv, false, idproxy.AuthModeOIDC, store.BackendMemory, tc.container,
			)
			if err == nil {
				t.Fatalf("expected ErrMemoryOIDCForbidden, got nil")
			}
			if !errors.Is(err, store.ErrMemoryOIDCForbidden) {
				t.Errorf("expected ErrMemoryOIDCForbidden, got %v", err)
			}
		})
	}
}

// 4. auth=oidc + sqlite + env なし + auto-generate=false → ErrSigningKeyRequired
func TestResolveSigningKey_OIDC_NoEnvNoAutoGenerate_Fails(t *testing.T) {
	dir := t.TempDir()
	c, err := sqlitestore.NewContainer(dir)
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()

	_, err = idproxy.ResolveSigningKey(
		context.Background(), "", false, idproxy.AuthModeOIDC, store.BackendSQLite, c,
	)
	if !errors.Is(err, idproxy.ErrSigningKeyRequired) {
		t.Fatalf("expected ErrSigningKeyRequired, got %v", err)
	}
}

// 5. auth=oidc + sqlite + env なし + auto-generate=true + container あり → LoadOrCreate 採用 + Warn
func TestResolveSigningKey_OIDC_AutoGenerate_UsesContainer(t *testing.T) {
	dir := t.TempDir()
	c, err := sqlitestore.NewContainer(dir)
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()

	buf, restore := captureLog(t)
	defer restore()

	got, err := idproxy.ResolveSigningKey(
		context.Background(), "", true, idproxy.AuthModeOIDC, store.BackendSQLite, c,
	)
	if err != nil {
		t.Fatalf("ResolveSigningKey: %v", err)
	}
	if got == nil {
		t.Fatalf("got nil key")
	}
	gotBytes := keyBytes(t, got)

	// 二回目は同じ鍵
	got2, err := idproxy.ResolveSigningKey(
		context.Background(), "", true, idproxy.AuthModeOIDC, store.BackendSQLite, c,
	)
	if err != nil {
		t.Fatalf("ResolveSigningKey 2nd: %v", err)
	}
	if !bytes.Equal(gotBytes, keyBytes(t, got2)) {
		t.Errorf("container did not persist key across calls")
	}

	// Warn ログに "auto-generated" 文字列が含まれる
	if !strings.Contains(buf.String(), "auto-generated SigningKey") {
		t.Errorf("expected auto-generated warn log, got: %s", buf.String())
	}
}

// 6. auth=oidc + sqlite + env なし + auto-generate=false + container あり → ErrSigningKeyRequired
//
// auto-generate が false なら container があっても LoadOrCreate を呼ばないことを確認。
func TestResolveSigningKey_OIDC_NoAutoGenerate_FailsEvenWithContainer(t *testing.T) {
	dir := t.TempDir()
	c, err := sqlitestore.NewContainer(dir)
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()

	_, err = idproxy.ResolveSigningKey(
		context.Background(), "", false, idproxy.AuthModeOIDC, store.BackendSQLite, c,
	)
	if !errors.Is(err, idproxy.ErrSigningKeyRequired) {
		t.Fatalf("expected ErrSigningKeyRequired, got %v", err)
	}
}

// 7. authz は引数に取らない（シグネチャ確認）。コンパイルが通ることが重要。
func TestResolveSigningKey_AuthZIsNotAnArgument(t *testing.T) {
	// シグネチャ: (ctx, pemEnv, autoGenerate, authMode, backend, container) → (key, err)
	// authz 文字列を引数に持たないことをコンパイル時に確認するためのスモークテスト。
	_, err := idproxy.ResolveSigningKey(
		context.Background(), "", false, idproxy.AuthModeNone, store.BackendMemory, nil,
	)
	if err != nil {
		t.Fatalf("smoke: %v", err)
	}
}

// 8. auth=none + env なし + container nil → ephemeral 生成 + Warn
func TestResolveSigningKey_None_NoContainer_Ephemeral(t *testing.T) {
	buf, restore := captureLog(t)
	defer restore()

	got, err := idproxy.ResolveSigningKey(
		context.Background(), "", false, idproxy.AuthModeNone, store.BackendMemory, nil,
	)
	if err != nil {
		t.Fatalf("ResolveSigningKey: %v", err)
	}
	if got == nil {
		t.Fatalf("got nil ephemeral key")
	}
	// 鍵が有効な ECDSA P-256 であることを Bytes() で確認
	if _, err := got.Bytes(); err != nil {
		t.Errorf("ephemeral key invalid: %v", err)
	}
	if !strings.Contains(buf.String(), "ephemeral SigningKey") {
		t.Errorf("expected ephemeral warn log, got: %s", buf.String())
	}
}

// 9. auth=none + env なし + container あり → container.LoadOrCreate
//
// auto-generate=false のときは Warn 出さない。container は memory でも OK（auth=none なので）
func TestResolveSigningKey_None_WithContainer_NoWarnUnlessAutoGenerate(t *testing.T) {
	c := memorystore.New(0)
	defer func() { _ = c.Close(context.Background()) }()

	buf, restore := captureLog(t)
	defer restore()

	got, err := idproxy.ResolveSigningKey(
		context.Background(), "", false, idproxy.AuthModeNone, store.BackendMemory, c,
	)
	if err != nil {
		t.Fatalf("ResolveSigningKey: %v", err)
	}
	if got == nil {
		t.Fatalf("got nil key")
	}
	// auto-generate=false なので "auto-generated" Warn は出ない
	if strings.Contains(buf.String(), "auto-generated SigningKey") {
		t.Errorf("unexpected auto-generated warn (autoGenerate=false): %s", buf.String())
	}
	// "ephemeral" も出ない（container 経由で取得しているため）
	if strings.Contains(buf.String(), "ephemeral SigningKey") {
		t.Errorf("unexpected ephemeral warn: %s", buf.String())
	}
}

// 10. PKCS#1 EC PRIVATE KEY フォールバック
func TestResolveSigningKey_PKCS1ECFallback(t *testing.T) {
	want, pemStr := newECPrivateKeyPEM(t)
	got, err := idproxy.ResolveSigningKey(
		context.Background(), pemStr, false, idproxy.AuthModeNone, store.BackendMemory, nil,
	)
	if err != nil {
		t.Fatalf("ResolveSigningKey: %v", err)
	}
	if !bytes.Equal(keyBytes(t, got), keyBytes(t, want)) {
		t.Errorf("EC PRIVATE KEY fallback did not match")
	}
}

// validateBackendForAuthMode の網羅テスト
func TestValidateBackendForAuthMode(t *testing.T) {
	cases := []struct {
		name    string
		mode    idproxy.AuthMode
		backend string
		wantErr bool
	}{
		{"oidc + memory forbidden", idproxy.AuthModeOIDC, store.BackendMemory, true},
		{"oidc + sqlite ok", idproxy.AuthModeOIDC, store.BackendSQLite, false},
		{"oidc + redis ok", idproxy.AuthModeOIDC, store.BackendRedis, false},
		{"oidc + dynamodb ok", idproxy.AuthModeOIDC, store.BackendDynamoDB, false},
		{"none + memory ok", idproxy.AuthModeNone, store.BackendMemory, false},
		{"none + sqlite ok", idproxy.AuthModeNone, store.BackendSQLite, false},
		{"none + redis ok", idproxy.AuthModeNone, store.BackendRedis, false},
		{"none + dynamodb ok", idproxy.AuthModeNone, store.BackendDynamoDB, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := idproxy.ValidateBackendForAuthMode(tc.mode, tc.backend)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, store.ErrMemoryOIDCForbidden) {
					t.Errorf("expected ErrMemoryOIDCForbidden, got %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("expected nil, got %v", err)
				}
			}
		})
	}
}
