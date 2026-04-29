package cache

import "time"

// 標準 TTL 定数（仕様書: apps / fields / resolver = 1 年）。
const (
	// TTLApps は GetApp レスポンスの TTL。
	TTLApps = 365 * 24 * time.Hour
	// TTLFields は GetFormFields レスポンスの TTL。
	TTLFields = 365 * 24 * time.Hour
	// TTLListApps は ListApps レスポンスの TTL。
	TTLListApps = 365 * 24 * time.Hour
)
