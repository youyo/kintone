-- M12 統合 Storage SQLite backend スキーマ
-- expires_at / updated_at / created_at は Unix epoch nano (INTEGER)
-- ただし kintone_oauth_tokens の expires_at / updated_at は Unix epoch sec (INTEGER) 互換

CREATE TABLE IF NOT EXISTS kintone_oauth_tokens (
    domain        TEXT NOT NULL,
    principal_id  TEXT NOT NULL,
    auth_type     TEXT NOT NULL,
    api_token     TEXT NOT NULL DEFAULT '',
    access_token  TEXT NOT NULL DEFAULT '',
    refresh_token TEXT NOT NULL DEFAULT '',
    expires_at    INTEGER NOT NULL DEFAULT 0,
    updated_at    INTEGER NOT NULL,
    PRIMARY KEY (domain, principal_id, auth_type)
);

CREATE TABLE IF NOT EXISTS kintone_kv_cache (
    key        TEXT PRIMARY KEY,
    value      BLOB NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kintone_kv_cache_expires ON kintone_kv_cache(expires_at);

CREATE TABLE IF NOT EXISTS kintone_signing_keys (
    id         TEXT PRIMARY KEY,
    pem        TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

-- M14: OAuth Authorization Code フロー用 state ↔ session map
-- state は authorize 開始時に Put され、callback 受信時に Take で取り出される（one-shot）。
-- TTL は DefaultStateTTL（10 分）。expires_at は Unix epoch nanoseconds。
CREATE TABLE IF NOT EXISTS kintone_oauth_state (
    state        TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL,
    verifier     TEXT NOT NULL,
    method       TEXT NOT NULL DEFAULT 'S256',
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kintone_oauth_state_expires ON kintone_oauth_state(expires_at);
