-- M07 tokenstore schema
-- expires_at / updated_at は Unix epoch seconds (INTEGER)
-- 0 は "期限なし" or "未設定" を意味する
CREATE TABLE IF NOT EXISTS tokens (
    domain        TEXT NOT NULL,
    principal_id  TEXT NOT NULL,
    auth_type     TEXT NOT NULL,    -- "api-token" | "oauth"
    api_token     TEXT NOT NULL DEFAULT '',
    access_token  TEXT NOT NULL DEFAULT '',
    refresh_token TEXT NOT NULL DEFAULT '',
    expires_at    INTEGER NOT NULL DEFAULT 0,
    updated_at    INTEGER NOT NULL,
    PRIMARY KEY (domain, principal_id, auth_type)
);
