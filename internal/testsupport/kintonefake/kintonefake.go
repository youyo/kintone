// Package kintonefake は in-process な kintone OAuth + REST API mock を提供する。
//
// E2E テスト専用。本番コードから import してはならない。
//
// 提供 endpoint:
//   - GET  /oauth2/authorization: authorize endpoint（M13）— redirect_uri に code/state を付与し 302
//   - POST /oauth2/token: authorization_code / refresh_token grant に応答（rotation）
//   - GET  /k/v1/records.json: Bearer access_token 検証 → 401(expired) または 200(records)
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
	"net/url"
	"strings"
	"sync"
)

// Server は httptest ベースの kintone mock。
type Server struct {
	httpServer *httptest.Server

	mu             sync.Mutex
	authzCodes     map[string]string // code -> principalID（M13）
	refreshTokens  map[string]string // refresh_token -> principalID
	validAccess    map[string]bool   // access_token -> true
	expiredOnceFor map[string]bool   // principalID -> 「最初の records 呼び出しは 401 を返す」フラグ

	// authorizePrincipalID は /oauth2/authorization で発行する code に紐付ける principalID。
	authorizePrincipalID string
}

// New は新しい kintone fake を起動する。
func New() *Server {
	s := &Server{
		authzCodes:           map[string]string{},
		refreshTokens:        map[string]string{},
		validAccess:          map[string]bool{},
		expiredOnceFor:       map[string]bool{},
		authorizePrincipalID: "user-1",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/authorization", s.handleAuthorize)
	mux.HandleFunc("/oauth2/token", s.handleToken)
	mux.HandleFunc("/k/v1/records.json", s.handleRecords)
	s.httpServer = httptest.NewServer(mux)
	return s
}

// SetAuthorizePrincipalID は /oauth2/authorization で発行する code に紐付ける principalID を設定する（M13）。
func (s *Server) SetAuthorizePrincipalID(pid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorizePrincipalID = pid
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

// handleAuthorize は kintone OAuth /oauth2/authorization mock（M13）。
//
// UI を介さず即座に code / state を redirect_uri に付与して 302 を返す。
// テスト簡略化のため client_id / scope 等の追加検証は行わない（redirect_uri と
// state の往復のみを担保する）。
func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	state := q.Get("state")
	if redirect == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}
	code := mustRandom(16)
	s.mu.Lock()
	s.authzCodes[code] = s.authorizePrincipalID
	s.mu.Unlock()

	dest, err := url.Parse(redirect)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	qq := dest.Query()
	qq.Set("code", code)
	if state != "" {
		qq.Set("state", state)
	}
	dest.RawQuery = qq.Encode()
	http.Redirect(w, r, dest.String(), http.StatusFound)
}

// handleToken は kintone OAuth /oauth2/token mock。
//
// authorization_code grant: code を消費し新規 access_token + refresh_token を返す（M13）。
// refresh_token grant: 旧 refresh_token を無効化し新規発行（rotation）。
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
	switch grant {
	case "authorization_code":
		code := r.PostForm.Get("code")
		s.mu.Lock()
		principalID, ok := s.authzCodes[code]
		if ok {
			delete(s.authzCodes, code)
		}
		s.mu.Unlock()
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid_grant", "unknown code")
			return
		}
		s.issueTokens(w, principalID)
		return
	case "refresh_token":
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
		s.issueTokens(w, principalID)
		return
	default:
		writeJSONError(w, http.StatusBadRequest, "unsupported_grant_type", grant)
		return
	}
}

// issueTokens は新規 access_token / refresh_token を発行し principalID に紐付ける。
func (s *Server) issueTokens(w http.ResponseWriter, principalID string) {
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
