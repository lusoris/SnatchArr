// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type idpClaims struct {
	Subject       string
	Email         string
	EmailVerified *bool
	Audience      string
	Issuer        string
	Expiry        time.Time
	Nonce         string
	UseOtherKey   bool
}

type fakeIDP struct {
	srv            *httptest.Server
	mainKey        *rsa.PrivateKey
	otherKey       *rsa.PrivateKey
	mu             sync.Mutex
	claimsModifier func(idpClaims) idpClaims
	rejectVerifier bool
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	mainKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("newFakeIDP: generate main key: %v", err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("newFakeIDP: generate other key: %v", err)
	}
	f := &fakeIDP{
		mainKey:  mainKey,
		otherKey: otherKey,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", f.handleDiscovery)
	mux.HandleFunc("/jwks", f.handleJWKS)
	mux.HandleFunc("/token", f.handleToken)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIDP) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                f.srv.URL,
		"authorization_endpoint":                f.srv.URL + "/authorize",
		"token_endpoint":                        f.srv.URL + "/token",
		"jwks_uri":                              f.srv.URL + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (f *fakeIDP) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	jwk := jose.JSONWebKey{
		Key:       &f.mainKey.PublicKey,
		KeyID:     "main-key-id",
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{jwk},
	})
}

func (f *fakeIDP) setClaimsModifier(fn func(idpClaims) idpClaims) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimsModifier = fn
}

func (f *fakeIDP) setRejectVerifier(reject bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejectVerifier = reject
}

func (f *fakeIDP) handleToken(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	reject := f.rejectVerifier
	mod := f.claimsModifier
	f.mu.Unlock()

	if reject {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
		return
	}
	tokenStr, err := f.buildIDToken(mod)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server_error"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "mock-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     tokenStr,
	})
}

func (f *fakeIDP) buildIDToken(mod func(idpClaims) idpClaims) (string, error) {
	verified := true
	c := idpClaims{
		Subject:       "admin-sub",
		Email:         "admin@example.com",
		EmailVerified: &verified,
		Audience:      "test-client-id",
		Issuer:        f.srv.URL,
		Expiry:        time.Now().Add(time.Hour),
		Nonce:         "expected-nonce",
	}
	if mod != nil {
		c = mod(c)
	}
	key := f.mainKey
	keyID := "main-key-id"
	if c.UseOtherKey {
		key = f.otherKey
		keyID = "other-key-id"
	}
	signerOpts := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, signerOpts)
	if err != nil {
		return "", err
	}
	stdClaims := jwt.Claims{
		Issuer:   c.Issuer,
		Subject:  c.Subject,
		Audience: jwt.Audience{c.Audience},
		Expiry:   jwt.NewNumericDate(c.Expiry),
		IssuedAt: jwt.NewNumericDate(time.Now()),
	}
	customClaims := map[string]any{}
	if c.Nonce != "" {
		customClaims["nonce"] = c.Nonce
	}
	if c.Email != "" {
		customClaims["email"] = c.Email
	}
	if c.EmailVerified != nil {
		customClaims["email_verified"] = *c.EmailVerified
	}
	return jwt.Signed(signer).Claims(stdClaims).Claims(customClaims).Serialize()
}
