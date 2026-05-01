// Package kintonefake は in-process な kintone OAuth + REST API mock を提供する。
//
// E2E テスト専用。本番コードから import してはならない。
//
// 提供 endpoint:
//   - POST /oauth2/token: refresh_token grant に応答（rotation: 毎回新しい refresh_token）
//   - GET /k/v1/records.json: Bearer access_token 検証 → 401(expired) または 200(records)
//
// 「初回 access_token は expired を返し、refresh 後は OK」シナリオを再現する。
package kintonefake

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Server は httptest ベースの kintone mock。
type Server struct {
	httpServer *httptest.Server

	mu             sync.Mutex
	refreshTokens  map[string]string // refresh_token -> principalID
	validAccess    map[string]bool   // access_token -> true
	expiredOnceFor map[string]bool   // principalID -> 「最初の records 呼び出しは 401 を返す」フラグ
}

// New は新しい kintone fake を起動する。
func New() *Server {
	s := &Server{
		refreshTokens:  map[string]string{},
		validAccess:    map[string]bool{},
		expiredOnceFor: map[string]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", s.handleToken)
	mux.HandleFunc("/k/v1/records.json", s.handleRecords)
	s.httpServer = httptest.NewServer(mux)
	return s
}

// URL は base URL を返す。
func (s *Server) URL() string { return s.httpServer.URL }

// Stop はサーバを停止する。
func (s *Server) Stop() { s.httpServer.Close() }

// SeedTokenFor は principalID 用の初期 refresh_token を発行し返す。
//
// 「最初の records 呼び出しでは access_token が expired として 401 を返す」シナリオを併せて準備する。
func (s *Server) SeedTokenFor(principalID string) (refreshToken string) {
	rt := mustRandom(24)
	s.mu.Lock()
	s.refreshTokens[rt] = principalID
	s.expiredOnceFor[principalID] = true
	s.mu.Unlock()
	return rt
}

// --- handlers ---

// handleToken は kintone OAuth /oauth2/token mock。
//
// refresh_token grant の場合、新しい access_token / refresh_token を発行し、
// 旧 refresh_token を無効化する（rotation）。
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	grant := r.PostForm.Get("grant_type")
	if grant != "refresh_token" {
		writeJSONError(w, http.StatusBadRequest, "unsupported_grant_type", grant)
		return
	}
	old := r.PostForm.Get("refresh_token")
	s.mu.Lock()
	principalID, ok := s.refreshTokens[old]
	if ok {
		delete(s.refreshTokens, old)
	}
	s.mu.Unlock()
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid_grant", "unknown refresh_token")
		return
	}

	access := mustRandom(24)
	newRefresh := mustRandom(24)
	s.mu.Lock()
	s.validAccess[access] = true
	s.refreshTokens[newRefresh] = principalID
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"refresh_token": newRefresh,
		"token_type":    "Bearer",
		"expires_in":    3600,
	})
}

// handleRecords は GET /k/v1/records.json の mock。
//
// Bearer access_token を検証する。「expiredOnceFor フラグ立ち」の principal の最初の
// 1 回は無条件で 401 を返し、以降は 200 + 空 records を返す。
func (s *Server) handleRecords(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "missing bearer", http.StatusUnauthorized)
		return
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	s.mu.Lock()
	valid := s.validAccess[token]
	s.mu.Unlock()
	if !valid {
		w.Header().Set("WWW-Authenticate", "Bearer error=\"invalid_token\"")
		http.Error(w, "expired", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": []any{}, "totalCount": "0"})
}

// --- helpers ---

func mustRandom(n int) string {
	if n <= 0 {
		panic(errors.New("kintonefake: non-positive length"))
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}
