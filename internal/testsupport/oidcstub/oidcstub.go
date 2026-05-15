// Package oidcstub は in-process な最小 OIDC プロバイダを提供する。
//
// E2E テスト専用。本番コードから import してはならない。
//
// httptest.NewServer で起動し、以下の endpoint を提供する:
//   - /.well-known/openid-configuration (discovery)
//   - /jwks (JWKS)
//   - /authorize (Authorization Code flow)
//   - /token (Token endpoint)
//   - /userinfo (UserInfo endpoint)
//
// RSA 鍵は起動時に生成され、Server.URL の Issuer として discovery と JWT 署名に使われる。
package oidcstub

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Config は OIDC stub の起動オプション。
type Config struct {
	// ClientID は /token endpoint で受け付ける client_id（空のとき "test-client"）。
	ClientID string
	// SubjectFor は authorize 時に sub クレームを決める。
	// nil の場合は "user-1" を返す。
	SubjectFor func(r *http.Request) string
}

// codeEntry は /authorize で発行した code に紐づくデータ。
type codeEntry struct {
	sub   string
	nonce string
}

// Server は httptest ベースの OIDC stub。
type Server struct {
	httpServer *httptest.Server
	cfg        Config
	priv       *rsa.PrivateKey
	kid        string

	mu    sync.Mutex
	codes map[string]codeEntry // code -> entry
}

// New は新しい OIDC stub を起動する。失敗時は error を返す。
func New(cfg Config) (*Server, error) {
	if cfg.ClientID == "" {
		cfg.ClientID = "test-client"
	}
	if cfg.SubjectFor == nil {
		cfg.SubjectFor = func(r *http.Request) string { return "user-1" }
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("oidcstub: generate key: %w", err)
	}
	s := &Server{
		cfg:   cfg,
		priv:  priv,
		kid:   "oidcstub-key-1",
		codes: map[string]codeEntry{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", s.handleDiscovery)
	mux.HandleFunc("/jwks", s.handleJWKS)
	mux.HandleFunc("/authorize", s.handleAuthorize)
	mux.HandleFunc("/token", s.handleToken)
	mux.HandleFunc("/userinfo", s.handleUserInfo)
	s.httpServer = httptest.NewServer(mux)
	return s, nil
}

// URL は stub の base URL（例: http://127.0.0.1:NNNNN）を返す。
func (s *Server) URL() string {
	return s.httpServer.URL
}

// Issuer は OIDC issuer 文字列を返す（=URL と同じ）。
func (s *Server) Issuer() string {
	return s.httpServer.URL
}

// ClientID は受け付ける client_id を返す。
func (s *Server) ClientID() string {
	return s.cfg.ClientID
}

// PublicKeyPEM は ID Token 検証用の RSA 公開鍵を PEM 形式で返す。
func (s *Server) PublicKeyPEM() string {
	der, _ := x509.MarshalPKIXPublicKey(&s.priv.PublicKey)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return string(pemBytes)
}

// Stop は stub サーバを停止する。
func (s *Server) Stop() {
	s.httpServer.Close()
}

// --- handlers ---

func (s *Server) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	doc := map[string]any{
		"issuer":                                s.Issuer(),
		"authorization_endpoint":                s.Issuer() + "/authorize",
		"token_endpoint":                        s.Issuer() + "/token",
		"jwks_uri":                              s.Issuer() + "/jwks",
		"userinfo_endpoint":                     s.Issuer() + "/userinfo",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	pub := s.priv.PublicKey
	jwks := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"kid": s.kid,
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(intToBigEndian(pub.E)),
			},
		},
	}
	writeJSON(w, http.StatusOK, jwks)
}

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	state := q.Get("state")
	nonce := q.Get("nonce")
	if redirect == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}
	code, err := randomString(16)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sub := s.cfg.SubjectFor(r)
	s.mu.Lock()
	s.codes[code] = codeEntry{sub: sub, nonce: nonce}
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

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	grant := r.PostForm.Get("grant_type")
	switch grant {
	case "authorization_code":
		code := r.PostForm.Get("code")
		s.mu.Lock()
		entry, ok := s.codes[code]
		if ok {
			delete(s.codes, code)
		}
		s.mu.Unlock()
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid_grant", "unknown code")
			return
		}
		s.respondToken(w, entry.sub, entry.nonce)
	case "refresh_token":
		// refresh_token はテスト簡略化のため任意の値を受け付け、新規発行のみ行う。
		s.respondToken(w, "user-1", "")
	default:
		writeJSONError(w, http.StatusBadRequest, "unsupported_grant_type", grant)
	}
}

func (s *Server) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "missing bearer", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sub":   "user-1",
		"email": "user-1@example.com",
	})
}

// respondToken は ID Token / access_token / refresh_token を返す。
func (s *Server) respondToken(w http.ResponseWriter, sub, nonce string) {
	now := time.Now()
	idToken, err := s.signIDToken(sub, nonce, now)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	access, _ := randomString(24)
	refresh, _ := randomString(24)
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"id_token":      idToken,
		"token_type":    "Bearer",
		"expires_in":    3600,
	})
}

// signIDToken は RS256 で OIDC 標準クレームを含む JWT を発行する。
// nonce が空でない場合は nonce クレームも含める（idproxy の nonce 検証に対応）。
// email は sub@test.example.com 形式のダミーアドレスを付与する（idproxy の email 必須要件に対応）。
func (s *Server) signIDToken(sub, nonce string, now time.Time) (string, error) {
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": s.kid}
	payload := map[string]any{
		"iss":   s.Issuer(),
		"sub":   sub,
		"aud":   s.cfg.ClientID,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
		"email": sub + "@test.example.com",
	}
	if nonce != "" {
		payload["nonce"] = nonce
	}
	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	pb, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(pb)
	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.priv, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func intToBigEndian(e int) []byte {
	b := big.NewInt(int64(e)).Bytes()
	if len(b) == 0 {
		return []byte{0}
	}
	return b
}

func randomString(n int) (string, error) {
	if n <= 0 {
		return "", errors.New("oidcstub: non-positive length")
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
