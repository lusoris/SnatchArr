// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/httpx/csrf"
	"github.com/golusoris/golusoris/httpx/middleware"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/jellyfin"
	"github.com/lusoris/SnatchArr/api/internal/sso"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

type identitiesFixture struct {
	*integrationFixture
	fakeAuth *fakeJellyfinAuthenticator
	router   chi.Router
}

func setupIdentitiesFixture(t *testing.T, jfDefaultRole domain.Role) *identitiesFixture {
	t.Helper()
	f := newIntegrationFixture(t)
	logBuf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	fakeAuth := &fakeJellyfinAuthenticator{}

	proxyCfg := config.ProxyAuthOptions{
		Enabled:         true,
		Header:          "Remote-User",
		GroupsHeader:    "X-authentik-groups",
		SecretHeader:    "X-SnatchArr-Proxy-Secret",
		Secret:          testProxySecret,
		TrustedCIDRs:    []string{"10.0.0.0/8"},
		AdminGroups:     []string{"snatcharr-admins"},
		RequesterGroups: []string{"snatcharr-requesters"},
		ViewerGroups:    []string{"snatcharr-viewers"},
	}

	f.h.cfg.ProxyAuth = proxyCfg
	f.h.cfg.Proxy.Trusted = []string{"192.168.1.0/24"}
	f.h.cfg.Jellyfin = config.JellyfinOptions{
		Enabled:     true,
		URL:         "http://jellyfin.local:8096",
		APIKey:      "api-key-test",
		Timeout:     5 * time.Second,
		DefaultRole: jfDefaultRole,
	}
	f.h.jellyfin = fakeAuth
	f.h.logger = logger

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := f.h.auth.Setup(ctx, "admin", "password-admin-1234")
	if err != nil && !strings.Contains(err.Error(), "first admin already exists") {
		t.Fatalf("setupIdentitiesFixture: auth.Setup: %v", err)
	}

	csrfMW, cErr := csrf.New(csrf.Options{})
	if cErr != nil {
		t.Fatalf("setupIdentitiesFixture: csrf.New: %v", cErr)
	}

	r := chi.NewRouter()
	r.Use(
		middleware.RequestID,
		sso.ProxyHeaderMiddleware(proxyCfg, logger, ErrorHandler(logger)),
		middleware.TrustProxy(middleware.TrustProxyOptions{TrustedCIDRs: f.h.cfg.Proxy.Trusted}),
		middleware.Recover(logger),
		middleware.Logger(logger, f.clk),
	)
	sec := NewSecurity(f.sessMgr, f.h.cfg, f.keys, f.h.auth, f.apiKeys, f.clk, logger)
	if mErr := Mount(r, MountParams{Logger: logger, Handlers: f.h, Security: sec, CSRF: csrfMW, Modules: f.h.modules}); mErr != nil {
		t.Fatalf("setupIdentitiesFixture: Mount: %v", mErr)
	}

	return &identitiesFixture{
		integrationFixture: f,
		fakeAuth:           fakeAuth,
		router:             r,
	}
}

type authenticatedSession struct {
	user      domain.User
	cookies   []*http.Cookie
	csrfToken string
}

func createAndLoginUser(t *testing.T, f *identitiesFixture, username, password string) authenticatedSession {
	t.Helper()
	hasher := crypto.NewPasswordHasher(1)
	hash, err := hasher.Hash(t.Context(), password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	userID := uuid.New()
	u, err := f.st.Q().CreateUser(t.Context(), sqlcgen.CreateUserParams{
		ID:           userID,
		Username:     username,
		PasswordHash: hash,
		Role:         string(domain.RoleRequester),
		CreatedAt:    f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}

	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login %s status = %d, want 200 (body: %s)", username, rec.Code, rec.Body.String())
	}
	var sess oas.Session
	if err := json.NewDecoder(rec.Body).Decode(&sess); err != nil {
		t.Fatalf("decode login session: %v", err)
	}
	return authenticatedSession{
		user:      domain.User{ID: u.ID, Username: u.Username, Role: domain.Role(u.Role)},
		cookies:   rec.Result().Cookies(),
		csrfToken: sess.CsrfToken,
	}
}

func TestIdentityLink_PositiveJellyfin(t *testing.T) {
	t.Parallel()
	f := setupIdentitiesFixture(t, domain.RoleRequester)
	bob := createAndLoginUser(t, f, "bob", "password-bob-1234")

	f.fakeAuth.signInFunc = func(ctx context.Context, user, pw string) (jellyfin.Member, error) {
		if user == "jelly-bob" && pw == "jf-secret" {
			return jellyfin.Member{ID: "jf-bob-guid", Name: "jelly-bob", Admin: false}, nil
		}
		return jellyfin.Member{}, domain.ErrUnauthorized
	}

	// Link Jellyfin identity
	linkBody := `{"username":"jelly-bob","password":"jf-secret"}`
	linkReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/jellyfin", bytes.NewBufferString(linkBody))
	linkReq.Header.Set("Content-Type", "application/json")
	linkReq.Header.Set("X-Csrf-Token", bob.csrfToken)
	for _, c := range bob.cookies {
		linkReq.AddCookie(c)
	}
	linkRec := httptest.NewRecorder()
	f.router.ServeHTTP(linkRec, linkReq)
	if linkRec.Code != http.StatusNoContent {
		t.Fatalf("POST /auth/identities/jellyfin status = %d, want 204 (body: %s)", linkRec.Code, linkRec.Body.String())
	}

	// Verify listed identities
	listReq := httptest.NewRequest(http.MethodGet, PathPrefix+"/auth/identities", nil)
	for _, c := range bob.cookies {
		listReq.AddCookie(c)
	}
	listRec := httptest.NewRecorder()
	f.router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /auth/identities status = %d, want 200", listRec.Code)
	}
	var idList oas.UserIdentities
	if err := json.NewDecoder(listRec.Body).Decode(&idList); err != nil {
		t.Fatalf("decode UserIdentities: %v", err)
	}
	if len(idList.Items) != 1 || idList.Items[0].Provider != oas.UserIdentityProviderJellyfin {
		t.Fatalf("listed identities = %+v, want 1 jellyfin item", idList.Items)
	}

	// Sign in via Jellyfin -> lands on local account (same user id)
	jfLoginBody := `{"username":"jelly-bob","password":"jf-secret"}`
	jfReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(jfLoginBody))
	jfReq.Header.Set("Content-Type", "application/json")
	jfRec := httptest.NewRecorder()
	f.router.ServeHTTP(jfRec, jfReq)
	if jfRec.Code != http.StatusOK {
		t.Fatalf("POST /auth/jellyfin status = %d, want 200 (body: %s)", jfRec.Code, jfRec.Body.String())
	}
	var jfSess oas.Session
	if err := json.NewDecoder(jfRec.Body).Decode(&jfSess); err != nil {
		t.Fatalf("decode jf session: %v", err)
	}
	if jfSess.User.ID != bob.user.ID {
		t.Fatalf("jf session user ID = %s, want bob ID %s", jfSess.User.ID, bob.user.ID)
	}
	if jfSess.User.Role != oas.UserRoleRequester {
		t.Fatalf("jf session role = %q, want requester", jfSess.User.Role)
	}
}

func TestIdentityLink_PositiveProxy(t *testing.T) {
	t.Parallel()
	f := setupIdentitiesFixture(t, domain.RoleRequester)
	carol := createAndLoginUser(t, f, "carol", "password-carol-1234")

	// Link proxy identity on current request
	linkReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/proxy", nil)
	linkReq.RemoteAddr = "10.0.0.5:12345"
	linkReq.Header.Set("Remote-User", "proxy-carol")
	linkReq.Header.Set("X-Authentik-Groups", "snatcharr-viewers")
	linkReq.Header.Set("X-Snatcharr-Proxy-Secret", testProxySecret)
	linkReq.Header.Set("X-Csrf-Token", carol.csrfToken)
	for _, c := range carol.cookies {
		linkReq.AddCookie(c)
	}
	linkRec := httptest.NewRecorder()
	f.router.ServeHTTP(linkRec, linkReq)
	if linkRec.Code != http.StatusNoContent {
		t.Fatalf("POST /auth/identities/proxy status = %d, want 204 (body: %s)", linkRec.Code, linkRec.Body.String())
	}

	// Verify listed identities
	listReq := httptest.NewRequest(http.MethodGet, PathPrefix+"/auth/identities", nil)
	for _, c := range carol.cookies {
		listReq.AddCookie(c)
	}
	listRec := httptest.NewRecorder()
	f.router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /auth/identities status = %d, want 200", listRec.Code)
	}
	var idList oas.UserIdentities
	if err := json.NewDecoder(listRec.Body).Decode(&idList); err != nil {
		t.Fatalf("decode UserIdentities: %v", err)
	}
	if len(idList.Items) != 1 || idList.Items[0].Provider != oas.UserIdentityProviderProxy {
		t.Fatalf("listed identities = %+v, want 1 proxy item", idList.Items)
	}

	// Proxy sign-in -> lands on linked carol account
	proxyReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/proxy-session", nil)
	proxyReq.RemoteAddr = "10.0.0.5:12345"
	proxyReq.Header.Set("Remote-User", "proxy-carol")
	proxyReq.Header.Set("X-Authentik-Groups", "snatcharr-viewers")
	proxyReq.Header.Set("X-Snatcharr-Proxy-Secret", testProxySecret)
	proxyRec := httptest.NewRecorder()
	f.router.ServeHTTP(proxyRec, proxyReq)
	if proxyRec.Code != http.StatusOK {
		t.Fatalf("POST /auth/proxy-session status = %d, want 200 (body: %s)", proxyRec.Code, proxyRec.Body.String())
	}
	var proxySess oas.Session
	if err := json.NewDecoder(proxyRec.Body).Decode(&proxySess); err != nil {
		t.Fatalf("decode proxy session: %v", err)
	}
	if proxySess.User.ID != carol.user.ID {
		t.Fatalf("proxy session user ID = %s, want carol ID %s", proxySess.User.ID, carol.user.ID)
	}
	if proxySess.User.Role != oas.UserRoleViewer {
		t.Fatalf("proxy session role = %q, want viewer", proxySess.User.Role)
	}
}

func TestIdentityLink_NegativeUnlinkedUsernameCollision(t *testing.T) {
	t.Parallel()
	assertUnlinkedUsernameCollision(t)
}

func assertUnlinkedUsernameCollision(t *testing.T) {
	t.Helper()
	f := setupIdentitiesFixture(t, domain.RoleRequester)
	alice := createAndLoginUser(t, f, "alice", "password-alice-1234")

	// Jellyfin member named "alice" signs in without being linked
	f.fakeAuth.signInFunc = func(ctx context.Context, user, pw string) (jellyfin.Member, error) {
		if user == "alice" && pw == "jf-alice-pass" {
			return jellyfin.Member{ID: "jf-alice-guid", Name: "alice", Admin: false}, nil
		}
		return jellyfin.Member{}, domain.ErrUnauthorized
	}

	jfReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(`{"username":"alice","password":"jf-alice-pass"}`))
	jfReq.Header.Set("Content-Type", "application/json")
	jfRec := httptest.NewRecorder()
	f.router.ServeHTTP(jfRec, jfReq)
	if jfRec.Code != http.StatusOK {
		t.Fatalf("POST /auth/jellyfin status = %d, want 200 (body: %s)", jfRec.Code, jfRec.Body.String())
	}
	var jfSess oas.Session
	if err := json.NewDecoder(jfRec.Body).Decode(&jfSess); err != nil {
		t.Fatalf("decode jf session: %v", err)
	}
	if jfSess.User.ID == alice.user.ID {
		t.Fatalf("Jellyfin member named 'alice' logged into local alice account %s!", alice.user.ID)
	}
	if !strings.HasPrefix(jfSess.User.Username, "jf-") {
		t.Fatalf("Jellyfin provisioned username = %q, want jf- prefix", jfSess.User.Username)
	}

	// Proxy user named "alice" signs in without being linked
	proxyReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/proxy-session", nil)
	proxyReq.RemoteAddr = "10.0.0.5:12345"
	proxyReq.Header.Set("Remote-User", "alice")
	proxyReq.Header.Set("X-Authentik-Groups", "snatcharr-viewers")
	proxyReq.Header.Set("X-Snatcharr-Proxy-Secret", testProxySecret)
	proxyRec := httptest.NewRecorder()
	f.router.ServeHTTP(proxyRec, proxyReq)
	if proxyRec.Code != http.StatusOK {
		t.Fatalf("POST /auth/proxy-session status = %d, want 200", proxyRec.Code)
	}
	var proxySess oas.Session
	if err := json.NewDecoder(proxyRec.Body).Decode(&proxySess); err != nil {
		t.Fatalf("decode proxy session: %v", err)
	}
	if proxySess.User.ID == alice.user.ID {
		t.Fatalf("Proxy user named 'alice' logged into local alice account %s!", alice.user.ID)
	}
	if !strings.HasPrefix(proxySess.User.Username, "proxy-") {
		t.Fatalf("Proxy provisioned username = %q, want proxy- prefix", proxySess.User.Username)
	}
}

//nolint:paralleltest // sequential subtests share fixture and database state
func TestIdentityLink_NegativeConflictsAndAuth(t *testing.T) {
	f := setupIdentitiesFixture(t, domain.RoleRequester)
	userA := createAndLoginUser(t, f, "user-a", "password-user-a-123")
	userB := createAndLoginUser(t, f, "user-b", "password-user-b-123")

	f.fakeAuth.signInFunc = func(ctx context.Context, user, pw string) (jellyfin.Member, error) {
		switch user {
		case "dan":
			if pw == "secret" {
				return jellyfin.Member{ID: "jf-dan-guid", Name: "dan", Admin: false}, nil
			}
		case "frank":
			if pw == "secret" {
				return jellyfin.Member{ID: "jf-frank-guid", Name: "frank", Admin: false}, nil
			}
		case "george":
			if pw == "secret" {
				return jellyfin.Member{ID: "jf-george-guid", Name: "george", Admin: false}, nil
			}
		case "jf-admin":
			if pw == "secret" {
				return jellyfin.Member{ID: "jf-admin-guid", Name: "jf-admin", Admin: true}, nil
			}
		case "disabled-user":
			return jellyfin.Member{ID: "jf-disabled-guid", Name: "disabled-user", Disabled: true}, nil
		}
		return jellyfin.Member{}, domain.ErrUnauthorized
	}

	t.Run("identity already owns its own account -> 409", func(t *testing.T) {
		// dan signs in via Jellyfin first, creating a provisioned account with external_id
		danReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(`{"username":"dan","password":"secret"}`))
		danReq.Header.Set("Content-Type", "application/json")
		danRec := httptest.NewRecorder()
		f.router.ServeHTTP(danRec, danReq)
		if danRec.Code != http.StatusOK {
			t.Fatalf("dan sign-in status = %d, want 200", danRec.Code)
		}

		// userA tries to link dan's identity -> 409
		linkReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/jellyfin", bytes.NewBufferString(`{"username":"dan","password":"secret"}`))
		linkReq.Header.Set("Content-Type", "application/json")
		linkReq.Header.Set("X-Csrf-Token", userA.csrfToken)
		for _, c := range userA.cookies {
			linkReq.AddCookie(c)
		}
		linkRec := httptest.NewRecorder()
		f.router.ServeHTTP(linkRec, linkReq)
		if linkRec.Code != http.StatusConflict {
			t.Fatalf("link owned identity status = %d, want 409 (body: %s)", linkRec.Code, linkRec.Body.String())
		}
	})

	t.Run("linked to another user -> 409", func(t *testing.T) {
		// userA links frank
		linkReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/jellyfin", bytes.NewBufferString(`{"username":"frank","password":"secret"}`))
		linkReq.Header.Set("Content-Type", "application/json")
		linkReq.Header.Set("X-Csrf-Token", userA.csrfToken)
		for _, c := range userA.cookies {
			linkReq.AddCookie(c)
		}
		linkRec := httptest.NewRecorder()
		f.router.ServeHTTP(linkRec, linkReq)
		if linkRec.Code != http.StatusNoContent {
			t.Fatalf("userA link frank status = %d, want 204", linkRec.Code)
		}

		// userB tries to link frank -> 409
		linkReqB := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/jellyfin", bytes.NewBufferString(`{"username":"frank","password":"secret"}`))
		linkReqB.Header.Set("Content-Type", "application/json")
		linkReqB.Header.Set("X-Csrf-Token", userB.csrfToken)
		for _, c := range userB.cookies {
			linkReqB.AddCookie(c)
		}
		linkRecB := httptest.NewRecorder()
		f.router.ServeHTTP(linkRecB, linkReqB)
		if linkRecB.Code != http.StatusConflict {
			t.Fatalf("userB link frank status = %d, want 409 (body: %s)", linkRecB.Code, linkRecB.Body.String())
		}
	})

	t.Run("account already linked to another provider identity -> 409", func(t *testing.T) {
		// userA is already linked to frank (jellyfin). userA tries to link george (jellyfin)
		linkReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/jellyfin", bytes.NewBufferString(`{"username":"george","password":"secret"}`))
		linkReq.Header.Set("Content-Type", "application/json")
		linkReq.Header.Set("X-Csrf-Token", userA.csrfToken)
		for _, c := range userA.cookies {
			linkReq.AddCookie(c)
		}
		linkRec := httptest.NewRecorder()
		f.router.ServeHTTP(linkRec, linkReq)
		if linkRec.Code != http.StatusConflict {
			t.Fatalf("link second jellyfin identity status = %d, want 409 (body: %s)", linkRec.Code, linkRec.Body.String())
		}
		if !strings.Contains(linkRec.Body.String(), "unlink first") {
			t.Fatalf("link second jellyfin identity body = %s, want problem detail mentioning unlink first", linkRec.Body.String())
		}
	})

	t.Run("jellyfin admin -> 409", func(t *testing.T) {
		linkReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/jellyfin", bytes.NewBufferString(`{"username":"jf-admin","password":"secret"}`))
		linkReq.Header.Set("Content-Type", "application/json")
		linkReq.Header.Set("X-Csrf-Token", userA.csrfToken)
		for _, c := range userA.cookies {
			linkReq.AddCookie(c)
		}
		linkRec := httptest.NewRecorder()
		f.router.ServeHTTP(linkRec, linkReq)
		if linkRec.Code != http.StatusConflict {
			t.Fatalf("link admin identity status = %d, want 409 (body: %s)", linkRec.Code, linkRec.Body.String())
		}
	})

	t.Run("bad password -> 401", func(t *testing.T) {
		linkReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/jellyfin", bytes.NewBufferString(`{"username":"frank","password":"wrong-password"}`))
		linkReq.Header.Set("Content-Type", "application/json")
		linkReq.Header.Set("X-Csrf-Token", userB.csrfToken)
		for _, c := range userB.cookies {
			linkReq.AddCookie(c)
		}
		linkRec := httptest.NewRecorder()
		f.router.ServeHTTP(linkRec, linkReq)
		if linkRec.Code != http.StatusUnauthorized {
			t.Fatalf("link bad password status = %d, want 401 (body: %s)", linkRec.Code, linkRec.Body.String())
		}
	})

	t.Run("disabled member -> 403", func(t *testing.T) {
		linkReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/jellyfin", bytes.NewBufferString(`{"username":"disabled-user","password":"any"}`))
		linkReq.Header.Set("Content-Type", "application/json")
		linkReq.Header.Set("X-Csrf-Token", userB.csrfToken)
		for _, c := range userB.cookies {
			linkReq.AddCookie(c)
		}
		linkRec := httptest.NewRecorder()
		f.router.ServeHTTP(linkRec, linkReq)
		if linkRec.Code != http.StatusForbidden {
			t.Fatalf("link disabled member status = %d, want 403 (body: %s)", linkRec.Code, linkRec.Body.String())
		}
	})

	t.Run("bearer API key -> 401", func(t *testing.T) {
		rawKey, _, err := f.keys.Issue(t.Context(), userA.user.ID.String(), nil)
		if err != nil {
			t.Fatalf("Issue api key: %v", err)
		}
		getReq := httptest.NewRequest(http.MethodGet, PathPrefix+"/auth/identities", nil)
		getReq.Header.Set("Authorization", "Bearer "+rawKey)
		getRec := httptest.NewRecorder()
		f.router.ServeHTTP(getRec, getReq)
		if getRec.Code != http.StatusUnauthorized {
			t.Fatalf("GET /auth/identities with bearer status = %d, want 401", getRec.Code)
		}

		postReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/jellyfin", bytes.NewBufferString(`{"username":"dan","password":"secret"}`))
		postReq.Header.Set("Content-Type", "application/json")
		postReq.Header.Set("Authorization", "Bearer "+rawKey)
		postRec := httptest.NewRecorder()
		f.router.ServeHTTP(postRec, postReq)
		if postRec.Code != http.StatusUnauthorized {
			t.Fatalf("POST /auth/identities/jellyfin with bearer status = %d, want 401", postRec.Code)
		}

		delJFReq := httptest.NewRequest(http.MethodDelete, PathPrefix+"/auth/identities/jellyfin", nil)
		delJFReq.Header.Set("Authorization", "Bearer "+rawKey)
		delJFRec := httptest.NewRecorder()
		f.router.ServeHTTP(delJFRec, delJFReq)
		if delJFRec.Code != http.StatusUnauthorized {
			t.Fatalf("DELETE /auth/identities/jellyfin with bearer status = %d, want 401", delJFRec.Code)
		}

		delProxyReq := httptest.NewRequest(http.MethodDelete, PathPrefix+"/auth/identities/proxy", nil)
		delProxyReq.Header.Set("Authorization", "Bearer "+rawKey)
		delProxyRec := httptest.NewRecorder()
		f.router.ServeHTTP(delProxyRec, delProxyReq)
		if delProxyRec.Code != http.StatusUnauthorized {
			t.Fatalf("DELETE /auth/identities/proxy with bearer status = %d, want 401", delProxyRec.Code)
		}
	})
}

//nolint:paralleltest // sequential subtests share fixture and database state
func TestIdentityLink_Boundary(t *testing.T) {
	f := setupIdentitiesFixture(t, domain.RoleRequester)
	grace := createAndLoginUser(t, f, "grace", "password-grace-1234")

	f.fakeAuth.signInFunc = func(ctx context.Context, user, pw string) (jellyfin.Member, error) {
		if user == "jf-grace" && pw == "secret" {
			return jellyfin.Member{ID: "jf-grace-guid", Name: "jf-grace", Admin: false}, nil
		}
		return jellyfin.Member{}, domain.ErrUnauthorized
	}

	t.Run("link twice same user -> 204 both", func(t *testing.T) {
		for i := 1; i <= 2; i++ {
			linkReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/jellyfin", bytes.NewBufferString(`{"username":"jf-grace","password":"secret"}`))
			linkReq.Header.Set("Content-Type", "application/json")
			linkReq.Header.Set("X-Csrf-Token", grace.csrfToken)
			for _, c := range grace.cookies {
				linkReq.AddCookie(c)
			}
			linkRec := httptest.NewRecorder()
			f.router.ServeHTTP(linkRec, linkReq)
			if linkRec.Code != http.StatusNoContent {
				t.Fatalf("link iteration %d status = %d, want 204", i, linkRec.Code)
			}
		}

		// Verify only 1 row exists
		listReq := httptest.NewRequest(http.MethodGet, PathPrefix+"/auth/identities", nil)
		for _, c := range grace.cookies {
			listReq.AddCookie(c)
		}
		listRec := httptest.NewRecorder()
		f.router.ServeHTTP(listRec, listReq)
		var idList oas.UserIdentities
		if err := json.NewDecoder(listRec.Body).Decode(&idList); err != nil {
			t.Fatalf("decode UserIdentities: %v", err)
		}
		if len(idList.Items) != 1 {
			t.Fatalf("listed identities count = %d, want 1", len(idList.Items))
		}
	})

	t.Run("unlink then sign-in -> provisioned account path again", func(t *testing.T) {
		delReq := httptest.NewRequest(http.MethodDelete, PathPrefix+"/auth/identities/jellyfin", nil)
		delReq.Header.Set("X-Csrf-Token", grace.csrfToken)
		for _, c := range grace.cookies {
			delReq.AddCookie(c)
		}
		delRec := httptest.NewRecorder()
		f.router.ServeHTTP(delRec, delReq)
		if delRec.Code != http.StatusNoContent {
			t.Fatalf("DELETE /auth/identities/jellyfin status = %d, want 204", delRec.Code)
		}

		// Now sign in via Jellyfin -> provisions new external account, does NOT link to grace
		jfReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(`{"username":"jf-grace","password":"secret"}`))
		jfReq.Header.Set("Content-Type", "application/json")
		jfRec := httptest.NewRecorder()
		f.router.ServeHTTP(jfRec, jfReq)
		if jfRec.Code != http.StatusOK {
			t.Fatalf("POST /auth/jellyfin status = %d, want 200", jfRec.Code)
		}
		var jfSess oas.Session
		if err := json.NewDecoder(jfRec.Body).Decode(&jfSess); err != nil {
			t.Fatalf("decode jf session: %v", err)
		}
		if jfSess.User.ID == grace.user.ID {
			t.Fatalf("Unlinked Jellyfin sign-in landed on grace ID %s!", grace.user.ID)
		}
	})

	t.Run("DELETE unlinked provider -> 404", func(t *testing.T) {
		delReq := httptest.NewRequest(http.MethodDelete, PathPrefix+"/auth/identities/proxy", nil)
		delReq.Header.Set("X-Csrf-Token", grace.csrfToken)
		for _, c := range grace.cookies {
			delReq.AddCookie(c)
		}
		delRec := httptest.NewRecorder()
		f.router.ServeHTTP(delRec, delReq)
		if delRec.Code != http.StatusNotFound {
			t.Fatalf("DELETE unlinked proxy status = %d, want 404", delRec.Code)
		}
	})
}

// TestIdentityLink_PlantedUsernameAutoLink documents and verifies against the account takeover defect.
// Planted mutation: If startJellyfinSession or CreateProxySession were mutated to look up users by
// username/handle instead of LinkedUser(ctx, provider, subject), then an unlinked Jellyfin or proxy
// user named "alice" would hijack the local "alice" account. The negative test
// TestIdentityLink_NegativeUnlinkedUsernameCollision prevents and catches this mutation by asserting
// that colliding usernames produce distinct accounts when not explicitly linked.
func TestIdentityLink_PlantedUsernameAutoLink(t *testing.T) {
	t.Parallel()
	// Replay the unlinked collision check to guarantee that automatic username-based linking is refused.
	assertUnlinkedUsernameCollision(t)
}

func TestIdentityLink_EffectiveRole(t *testing.T) {
	t.Parallel()
	// Default Jellyfin role is viewer
	f := setupIdentitiesFixture(t, domain.RoleViewer)
	henry := createAndLoginUser(t, f, "henry", "password-henry-1234")

	f.fakeAuth.signInFunc = func(ctx context.Context, user, pw string) (jellyfin.Member, error) {
		if user == "jf-henry" && pw == "secret" {
			return jellyfin.Member{ID: "jf-henry-guid", Name: "jf-henry", Admin: false}, nil
		}
		return jellyfin.Member{}, domain.ErrUnauthorized
	}

	// Henry links Jellyfin account
	linkReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/identities/jellyfin", bytes.NewBufferString(`{"username":"jf-henry","password":"secret"}`))
	linkReq.Header.Set("Content-Type", "application/json")
	linkReq.Header.Set("X-Csrf-Token", henry.csrfToken)
	for _, c := range henry.cookies {
		linkReq.AddCookie(c)
	}
	linkRec := httptest.NewRecorder()
	f.router.ServeHTTP(linkRec, linkReq)
	if linkRec.Code != http.StatusNoContent {
		t.Fatalf("link status = %d, want 204", linkRec.Code)
	}

	// Sign in via Jellyfin -> gets viewer session role (default_role viewer)
	jfReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(`{"username":"jf-henry","password":"secret"}`))
	jfReq.Header.Set("Content-Type", "application/json")
	jfRec := httptest.NewRecorder()
	f.router.ServeHTTP(jfRec, jfReq)
	if jfRec.Code != http.StatusOK {
		t.Fatalf("jf login status = %d, want 200", jfRec.Code)
	}
	var jfSess oas.Session
	if err := json.NewDecoder(jfRec.Body).Decode(&jfSess); err != nil {
		t.Fatalf("decode jf session: %v", err)
	}
	if jfSess.User.ID != henry.user.ID {
		t.Fatalf("jf session user ID = %s, want henry ID %s", jfSess.User.ID, henry.user.ID)
	}
	if jfSess.User.Role != oas.UserRoleViewer {
		t.Fatalf("jf session role = %q, want viewer", jfSess.User.Role)
	}
	jfCookies := jfRec.Result().Cookies()

	// Calling POST /auth/api-keys (requester-only) on the Jellyfin session MUST BE REFUSED with 403 Forbidden!
	keyBody := `{"name":"test-key","scopes":["core:read"]}`
	apiKeyReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/api-keys", bytes.NewBufferString(keyBody))
	apiKeyReq.Header.Set("Content-Type", "application/json")
	apiKeyReq.Header.Set("X-Csrf-Token", jfSess.CsrfToken)
	for _, c := range jfCookies {
		apiKeyReq.AddCookie(c)
	}
	apiKeyRec := httptest.NewRecorder()
	f.router.ServeHTTP(apiKeyRec, apiKeyReq)
	if apiKeyRec.Code != http.StatusForbidden {
		t.Fatalf("POST /auth/api-keys on Jellyfin viewer session status = %d, want 403 (body: %s)", apiKeyRec.Code, apiKeyRec.Body.String())
	}

	// Calling POST /auth/api-keys on the password session (requester role) MUST SUCCEED with 201 Created!
	pwdKeyReq := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/api-keys", bytes.NewBufferString(keyBody))
	pwdKeyReq.Header.Set("Content-Type", "application/json")
	pwdKeyReq.Header.Set("X-Csrf-Token", henry.csrfToken)
	for _, c := range henry.cookies {
		pwdKeyReq.AddCookie(c)
	}
	pwdKeyRec := httptest.NewRecorder()
	f.router.ServeHTTP(pwdKeyRec, pwdKeyReq)
	if pwdKeyRec.Code != http.StatusCreated {
		t.Fatalf("POST /auth/api-keys on password session status = %d, want 201 (body: %s)", pwdKeyRec.Code, pwdKeyRec.Body.String())
	}
}
