package sqlite

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
	"time"
)

// signingKeyID は kintone_signing_keys テーブルの primary key 値。
// 単一鍵運用のため固定値とする。鍵ローテーションは Phase 7+ で別 API を追加予定。
const signingKeyID = "current"

// SQLiteSigningKeyStore は [store.SigningKeyStore] の SQLite 実装。
//
// 永続化形式: PKCS#8 PEM (`PRIVATE KEY` ブロック)。読み出し時は PKCS#8 を一次パースし、
// 失敗したら PKCS#1 (EC PRIVATE KEY) フォールバックを試みる。
type SQLiteSigningKeyStore struct {
	db    *sql.DB
	mu    sync.Mutex
	cache *ecdsa.PrivateKey
}

// NewSigningKeyStore は SQLiteSigningKeyStore を構築する。db は呼び出し側 (Container) が所有する。
func NewSigningKeyStore(db *sql.DB) *SQLiteSigningKeyStore {
	return &SQLiteSigningKeyStore{db: db}
}

// LoadOrCreate は永続鍵をロードする。未保存なら ES256 (P-256) 鍵を新規生成して保存する。
//
// 同一 Store 内では同じ *ecdsa.PrivateKey を返す（プロセス内 cache）。
// 並行呼び出しは sync.Mutex で直列化される。
func (s *SQLiteSigningKeyStore) LoadOrCreate(ctx context.Context) (*ecdsa.PrivateKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache != nil {
		return s.cache, nil
	}

	// 既存行を SELECT
	var pemStr string
	row := s.db.QueryRowContext(ctx, `SELECT pem FROM kintone_signing_keys WHERE id=?`, signingKeyID)
	switch err := row.Scan(&pemStr); {
	case err == nil:
		key, perr := parsePEM(pemStr)
		if perr != nil {
			return nil, fmt.Errorf("store/sqlite: signing key parse: %w", perr)
		}
		s.cache = key
		return key, nil
	case errors.Is(err, sql.ErrNoRows):
		// fall through to generate
	default:
		return nil, fmt.Errorf("store/sqlite: signing key select: %w", err)
	}

	// 新規生成
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: signing key generate: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: signing key marshal: %w", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO kintone_signing_keys(id, pem, created_at) VALUES(?,?,?)
		 ON CONFLICT(id) DO NOTHING`,
		signingKeyID, string(pemBytes), time.Now().UnixNano(),
	)
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: signing key insert: %w", err)
	}

	// race: 競合 INSERT で先行行ができていれば再取得して採用する
	row = s.db.QueryRowContext(ctx, `SELECT pem FROM kintone_signing_keys WHERE id=?`, signingKeyID)
	var got string
	if err := row.Scan(&got); err != nil {
		return nil, fmt.Errorf("store/sqlite: signing key reselect: %w", err)
	}
	persisted, err := parsePEM(got)
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: signing key parse persisted: %w", err)
	}
	s.cache = persisted
	return persisted, nil
}

// parsePEM は PEM 文字列から *ecdsa.PrivateKey を取り出す。
// PKCS#8 (PRIVATE KEY) を一次、PKCS#1 (EC PRIVATE KEY) をフォールバックで試す。
func parsePEM(pemStr string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("invalid PEM block")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ec, ok := k.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key is not ECDSA: %T", k)
		}
		return ec, nil
	}
	// fallback: PKCS#1 EC PRIVATE KEY
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, errors.New("PEM is neither PKCS#8 nor SEC1 EC private key")
}

// Close は no-op。実際の *sql.DB.Close は Container が行う。
func (s *SQLiteSigningKeyStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = nil
	return nil
}
