// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/config"
)

type proxyTestCase struct {
	name            string
	enabled         bool
	hasAdmin        bool
	remoteAddr      string
	headerVal       string
	groupsHeaderVal string
	forwardedFor    string
	wantStatus      int
	wantSession     bool
	wantRole        string
}

func buildProxyCases() []proxyTestCase {
	return []proxyTestCase{
		{
			name:            "trusted CIDR and admin group",
			enabled:         true,
			hasAdmin:        true,
			remoteAddr:      "10.0.0.5:12345",
			headerVal:       "admin",
			groupsHeaderVal: "snatcharr-admins",
			wantStatus:      http.StatusOK,
			wantSession:     true,
			wantRole:        "admin",
		},
		{
			name:            "trusted CIDR and viewer group",
			enabled:         true,
			hasAdmin:        true,
			remoteAddr:      "10.0.0.5:12345",
			headerVal:       "viewer-user",
			groupsHeaderVal: "snatcharr-viewers",
			wantStatus:      http.StatusOK,
			wantSession:     true,
			wantRole:        "viewer",
		},
		{
			name:            "trusted CIDR and both admin and viewer groups",
			enabled:         true,
			hasAdmin:        true,
			remoteAddr:      "10.0.0.5:12345",
			headerVal:       "admin",
			groupsHeaderVal: "snatcharr-viewers|snatcharr-admins",
			wantStatus:      http.StatusOK,
			wantSession:     true,
			wantRole:        "admin",
		},
		{
			name:            "boundary pipe separated groups with empty segments",
			enabled:         true,
			hasAdmin:        true,
			remoteAddr:      "10.0.0.5:12345",
			headerVal:       "viewer-pipe",
			groupsHeaderVal: "|snatcharr-viewers||other|",
			wantStatus:      http.StatusOK,
			wantSession:     true,
			wantRole:        "viewer",
		},
		{
			name:            "same header from untrusted peer",
			enabled:         true,
			hasAdmin:        true,
			remoteAddr:      "192.168.1.100:12345",
			headerVal:       "admin",
			groupsHeaderVal: "snatcharr-admins",
			wantStatus:      http.StatusUnauthorized,
			wantSession:     false,
		},
		{
			name:            "trusted peer with group not allowed",
			enabled:         true,
			hasAdmin:        true,
			remoteAddr:      "10.0.0.5:12345",
			headerVal:       "unauthorized-stranger",
			groupsHeaderVal: "other-group",
			wantStatus:      http.StatusForbidden,
			wantSession:     false,
		},
		{
			name:            "trusted peer without groups header",
			enabled:         true,
			hasAdmin:        true,
			remoteAddr:      "10.0.0.5:12345",
			headerVal:       "admin",
			groupsHeaderVal: "",
			wantStatus:      http.StatusForbidden,
			wantSession:     false,
		},
		{
			name:            "trusted peer without user header",
			enabled:         true,
			hasAdmin:        true,
			remoteAddr:      "10.0.0.5:12345",
			headerVal:       "",
			groupsHeaderVal: "snatcharr-admins",
			wantStatus:      http.StatusUnauthorized,
			wantSession:     false,
		},
		{
			name:            "untrusted peer with spoofed X-Forwarded-For",
			enabled:         true,
			hasAdmin:        true,
			remoteAddr:      "192.168.1.100:12345",
			headerVal:       "admin",
			groupsHeaderVal: "snatcharr-admins",
			forwardedFor:    "10.0.0.5",
			wantStatus:      http.StatusUnauthorized,
			wantSession:     false,
		},
		{
			name:            "proxy auth disabled",
			enabled:         false,
			hasAdmin:        true,
			remoteAddr:      "10.0.0.5:12345",
			headerVal:       "admin",
			groupsHeaderVal: "snatcharr-admins",
			wantStatus:      http.StatusNotFound,
			wantSession:     false,
		},
		{
			name:            "no admin yet",
			enabled:         true,
			hasAdmin:        false,
			remoteAddr:      "10.0.0.5:12345",
			headerVal:       "admin",
			groupsHeaderVal: "snatcharr-admins",
			wantStatus:      http.StatusConflict,
			wantSession:     false,
		},
	}
}

func setupProxyTest(t *testing.T, tc proxyTestCase) *ssoFixture {
	t.Helper()
	proxyCfg := config.ProxyAuthOptions{
		Enabled:      tc.enabled,
		Header:       "Remote-User",
		GroupsHeader: "X-authentik-groups",
		TrustedCIDRs: []string{"10.0.0.0/8"},
		AdminGroups:  []string{"snatcharr-admins"},
		ViewerGroups: []string{"snatcharr-viewers"},
	}
	f := newSSOFixture(t, config.OIDCOptions{}, proxyCfg)
	f.h.cfg.Proxy.Trusted = []string{"192.168.1.0/24"}

	if tc.hasAdmin {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		if _, err := f.h.auth.Setup(ctx, "admin", "password-admin-1234"); err != nil {
			t.Fatalf("setupProxyTest: auth.Setup: %v", err)
		}
	}
	return f
}

func executeProxyTest(t *testing.T, tc proxyTestCase) {
	t.Helper()
	f := setupProxyTest(t, tc)

	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/proxy-session", nil)
	req.RemoteAddr = tc.remoteAddr
	if tc.headerVal != "" {
		req.Header.Set("Remote-User", tc.headerVal)
	}
	if tc.groupsHeaderVal != "" {
		req.Header.Set("X-Authentik-Groups", tc.groupsHeaderVal)
	}
	if tc.forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", tc.forwardedFor)
	}

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != tc.wantStatus {
		t.Fatalf("proxy-session status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
	}
	if !tc.wantSession {
		return
	}

	verifyProxySession(t, f, rec, tc.wantRole)
}

func verifyProxySession(t *testing.T, f *ssoFixture, rec *httptest.ResponseRecorder, wantRole string) {
	t.Helper()
	var sess oas.Session
	if err := json.NewDecoder(rec.Body).Decode(&sess); err != nil {
		t.Fatalf("verifyProxySession: decode session: %v", err)
	}
	expectedRole := oas.UserRoleAdmin
	if wantRole != "" {
		expectedRole = oas.UserRole(wantRole)
	}
	if sess.User.Role != expectedRole {
		t.Errorf("verifyProxySession: user role = %q, want %q", sess.User.Role, expectedRole)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("verifyProxySession: no session cookie set")
	}

	sessReq := httptest.NewRequest(http.MethodGet, PathPrefix+"/auth/session", nil)
	sessReq.RemoteAddr = "10.0.0.5:12345"
	for _, c := range cookies {
		sessReq.AddCookie(c)
	}
	sessRec := httptest.NewRecorder()
	f.router.ServeHTTP(sessRec, sessReq)

	if sessRec.Code != http.StatusOK {
		t.Fatalf("GET /auth/session status = %d, want 200", sessRec.Code)
	}
}

func TestProxyAuth_Flow(t *testing.T) {
	t.Parallel()
	for _, tc := range buildProxyCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			executeProxyTest(t, tc)
		})
	}
}

func TestSSO_NoSecretsInLogs(t *testing.T) {
	t.Parallel()
	const (
		secretClient = "sensitive-client-secret-xyz"
		secretCode   = "sensitive-auth-code-123"
		secretUser   = "sensitive-proxy-header-user"
	)

	idp := newFakeIDP(t)
	oidcCfg := config.OIDCOptions{
		Enabled:          true,
		IssuerURL:        idp.srv.URL,
		ClientID:         "test-client-id",
		ClientSecret:     secretClient,
		RedirectURL:      "http://127.0.0.1:8080/api/v1/auth/oidc/callback",
		Scopes:           []string{"openid", "email"},
		AdminGroups:      []string{"snatcharr-admins"},
		DiscoveryTimeout: 5 * time.Second,
	}
	proxyCfg := config.ProxyAuthOptions{
		Enabled:      true,
		Header:       "Remote-User",
		GroupsHeader: "X-authentik-groups",
		TrustedCIDRs: []string{"10.0.0.0/8"},
		AdminGroups:  []string{"snatcharr-admins"},
	}

	f := newSSOFixture(t, oidcCfg, proxyCfg)
	f.ssoSvc.SetHTTPClient(idp.srv.Client())

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := f.h.auth.Setup(ctx, "admin", "password-admin-1234"); err != nil {
		t.Fatalf("auth.Setup: %v", err)
	}

	// 1. Run wrong-code_verifier refusal OIDC flow
	idp.setRejectVerifier(true)
	stateBad, _ := runOIDCLogin(t, f)
	cbURLBad := buildCallbackURL(f.srv.URL, stateBad, secretCode, "")
	cbRespBad := runOIDCCallback(t, f, cbURLBad)
	_ = cbRespBad.Body.Close()
	if cbRespBad.StatusCode != http.StatusFound || cbRespBad.Header.Get("Location") != "/login?sso=failed" {
		t.Fatalf("wrong code verifier callback status = %d loc = %q, want 302 /login?sso=failed",
			cbRespBad.StatusCode, cbRespBad.Header.Get("Location"))
	}
	idp.setRejectVerifier(false)

	// 2. Run successful OIDC flow
	state, nonce := runOIDCLogin(t, f)
	idp.setClaimsModifier(func(c idpClaims) idpClaims {
		c.Nonce = nonce
		return c
	})
	cbURL := buildCallbackURL(f.srv.URL, state, secretCode, "")
	cbResp := runOIDCCallback(t, f, cbURL)
	_ = cbResp.Body.Close()

	// 3. Run Proxy Auth with secret user from untrusted source
	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/proxy-session", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("Remote-User", secretUser)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	// 4. Inspect logs
	logs := f.logBuf.String()
	secrets := []string{secretClient, secretCode, state, stateBad, nonce, secretUser}
	for _, sec := range secrets {
		if strings.Contains(logs, sec) {
			t.Errorf("log output contains sensitive value %q", sec)
		}
	}
	if !strings.Contains(logs, "reason=exchange_failed") || !strings.Contains(logs, "detail=invalid_grant") {
		t.Errorf("expected refusal log with reason=exchange_failed and detail=invalid_grant, got:\n%s", logs)
	}
}
