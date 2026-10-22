// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/golusoris/golusoris/httpx/csrf"
	"github.com/golusoris/golusoris/httpx/middleware"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/sso"
	"github.com/lusoris/SnatchArr/api/internal/testdiag"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type ssoFixture struct {
	*integrationFixture
	srv    *httptest.Server
	client *http.Client
	jar    http.CookieJar
	ssoSvc *sso.Service
	logBuf *syncBuffer
	router chi.Router
	sec    *Security
}

func newSSOFixture(t *testing.T, oidcCfg config.OIDCOptions, proxyCfg config.ProxyAuthOptions) *ssoFixture {
	t.Helper()
	f := newIntegrationFixture(t)
	logBuf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	f.h.cfg.OIDC = oidcCfg
	f.h.cfg.ProxyAuth = proxyCfg
	f.h.logger = logger

	ssoSvc := sso.NewService(f.h.cfg, f.h.auth, f.sessMgr, f.clk, logger)
	csrfMW, cErr := csrf.New(csrf.Options{})
	if cErr != nil {
		t.Fatalf("newSSOFixture: csrf.New: %v", cErr)
	}

	r := chi.NewRouter()
	r.Use(
		middleware.RequestID,
		sso.ProxyHeaderMiddleware(proxyCfg, logger),
		middleware.TrustProxy(middleware.TrustProxyOptions{TrustedCIDRs: f.h.cfg.Proxy.Trusted}),
		middleware.Recover(logger),
		middleware.Logger(logger, f.clk),
		middleware.SecureHeaders(middleware.SecureHeadersDefaults()),
	)
	sso.Mount(r, PathPrefix, ssoSvc, ErrorHandler(logger))
	sec := NewSecurity(f.sessMgr, f.h.cfg, f.keys, f.h.auth, f.apiKeys, f.clk, logger)
	if mErr := Mount(r, MountParams{Logger: logger, Handlers: f.h, Security: sec, CSRF: csrfMW}); mErr != nil {
		t.Fatalf("newSSOFixture: Mount: %v", mErr)
	}

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	jar, jErr := cookiejar.New(nil)
	if jErr != nil {
		t.Fatalf("newSSOFixture: cookiejar.New: %v", jErr)
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Jar:     jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &ssoFixture{
		integrationFixture: f,
		srv:                srv,
		client:             client,
		jar:                jar,
		ssoSvc:             ssoSvc,
		logBuf:             logBuf,
		router:             r,
		sec:                sec,
	}
}

func extractAuthParams(t *testing.T, resp *http.Response) (state, nonce string) {
	t.Helper()
	loc := resp.Header.Get("Location")
	if loc == "" {
		t.Fatalf("extractAuthParams: missing Location header in login response")
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("extractAuthParams: parse Location URL: %v", err)
	}
	q := u.Query()
	return q.Get("state"), q.Get("nonce")
}

func checkSessionUser(t *testing.T, f *ssoFixture, wantOK bool, wantRole string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.srv.URL+PathPrefix+"/auth/session", nil)
	if err != nil {
		t.Fatalf("checkSessionUser: NewRequest: %v", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("checkSessionUser: Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !wantOK {
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("checkSessionUser status = %d, want 401", resp.StatusCode)
		}
		return
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("checkSessionUser status = %d, want 200", resp.StatusCode)
	}
	var sess oas.Session
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("checkSessionUser: decode session: %v", err)
	}
	expectedRole := oas.UserRoleAdmin
	if wantRole != "" {
		expectedRole = oas.UserRole(wantRole)
	}
	if sess.User.Role != expectedRole {
		t.Errorf("checkSessionUser role = %q, want %q", sess.User.Role, expectedRole)
	}
}

type oidcTestCase struct {
	name         string
	claimMod     func(idpClaims) idpClaims
	clockAdvance time.Duration
	rejectCode   bool
	tamperState  string
	errorParam   string
	replay       bool
	wantRedirect string
	wantSession  bool
	wantRole     string
	wantReason   string
}

func buildOIDCCases() []oidcTestCase {
	return []oidcTestCase{
		{
			name:         "positive full flow",
			wantRedirect: "/",
			wantSession:  true,
			wantRole:     "admin",
		},
		{
			name: "positive viewer group flow",
			claimMod: func(c idpClaims) idpClaims {
				c.Groups = []string{"snatcharr-viewers"}
				return c
			},
			wantRedirect: "/",
			wantSession:  true,
			wantRole:     "viewer",
		},
		{
			name: "positive both admin and viewer groups resolves to admin",
			claimMod: func(c idpClaims) idpClaims {
				c.Groups = []string{"snatcharr-viewers", "snatcharr-admins"}
				return c
			},
			wantRedirect: "/",
			wantSession:  true,
			wantRole:     "admin",
		},
		{
			name:         "boundary flow 10m-1s",
			clockAdvance: 10*time.Minute - time.Second,
			wantRedirect: "/",
			wantSession:  true,
			wantRole:     "admin",
		},
		{
			name:         "negative missing state",
			tamperState:  "missing",
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "invalid_state",
		},
		{
			name:         "negative wrong state",
			tamperState:  "bogus-state-value",
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "invalid_state",
		},
		{
			name:         "negative replayed callback",
			replay:       true,
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "invalid_state",
		},
		{
			name:         "negative expired flow 10m+1s",
			clockAdvance: 10*time.Minute + time.Second,
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "flow_expired",
		},
		{
			name: "negative wrong nonce",
			claimMod: func(c idpClaims) idpClaims {
				c.Nonce = "wrong-nonce"
				return c
			},
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "invalid_nonce",
		},
		{
			name: "negative missing nonce",
			claimMod: func(c idpClaims) idpClaims {
				c.Nonce = ""
				return c
			},
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "invalid_nonce",
		},
		{
			name:         "negative wrong code verifier",
			rejectCode:   true,
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "exchange_failed",
		},
		{
			name: "negative id_token signed by other key",
			claimMod: func(c idpClaims) idpClaims {
				c.UseOtherKey = true
				return c
			},
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "invalid_id_token",
		},
		{
			name: "negative wrong audience",
			claimMod: func(c idpClaims) idpClaims {
				c.Audience = "different-client-id"
				return c
			},
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "invalid_id_token",
		},
		{
			name: "negative wrong issuer",
			claimMod: func(c idpClaims) idpClaims {
				c.Issuer = "https://untrusted.issuer.example.com"
				return c
			},
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "invalid_id_token",
		},
		{
			name: "negative expired id_token",
			claimMod: func(c idpClaims) idpClaims {
				c.Expiry = time.Now().Add(-1 * time.Hour)
				return c
			},
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "invalid_id_token",
		},
		{
			name: "negative unmapped group",
			claimMod: func(c idpClaims) idpClaims {
				c.Groups = []string{"unmapped-group"}
				return c
			},
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "not_authorized",
		},
		{
			name: "negative missing groups claim",
			claimMod: func(c idpClaims) idpClaims {
				c.Groups = nil
				return c
			},
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "not_authorized",
		},
		{
			name: "negative wrong type in groups claim",
			claimMod: func(c idpClaims) idpClaims {
				c.RawGroups = "not-an-array"
				return c
			},
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "not_authorized",
		},
		{
			name:         "negative idp error param",
			errorParam:   "access_denied",
			wantRedirect: "/login?sso=failed",
			wantSession:  false,
			wantReason:   "idp_error",
		},
	}
}

func runOIDCLogin(t *testing.T, f *ssoFixture) (state, nonce string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	loginReq, err := http.NewRequestWithContext(ctx, http.MethodGet, f.srv.URL+PathPrefix+"/auth/oidc/login", nil)
	if err != nil {
		t.Fatalf("runOIDCLogin: NewRequest: %v", err)
	}
	loginResp, err := f.client.Do(loginReq)
	if err != nil {
		t.Fatalf("runOIDCLogin: Do: %v", err)
	}
	defer func() { _ = loginResp.Body.Close() }()

	if loginResp.StatusCode != http.StatusFound {
		t.Fatalf("runOIDCLogin: status = %d, want 302", loginResp.StatusCode)
	}
	return extractAuthParams(t, loginResp)
}

func runOIDCCallback(t *testing.T, f *ssoFixture, cbURL string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	cbReq, err := http.NewRequestWithContext(ctx, http.MethodGet, cbURL, nil)
	if err != nil {
		t.Fatalf("runOIDCCallback: NewRequest: %v", err)
	}
	cbResp, err := f.client.Do(cbReq)
	if err != nil {
		t.Fatalf("runOIDCCallback: Do: %v", err)
	}
	return cbResp
}

func buildCallbackURL(srvURL, state, code, errParam string) string {
	u := srvURL + PathPrefix + "/auth/oidc/callback?"
	params := url.Values{}
	if errParam != "" {
		params.Set("error", errParam)
	}
	if state != "missing" {
		params.Set("state", state)
	}
	if code != "" {
		params.Set("code", code)
	}
	return u + params.Encode()
}

// hasReasonToken reports whether the slog text line carries reason=wantReason as a whole field.
func hasReasonToken(line, wantReason string) bool {
	return slices.Contains(strings.Fields(line), "reason="+wantReason)
}

func assertRefusalLog(t *testing.T, logs, wantReason string) {
	t.Helper()
	const refusalMsg = `msg="sso: oidc login refused"`
	if wantReason == "" {
		if strings.Contains(logs, refusalMsg) {
			t.Errorf("assertRefusalLog: unexpected refusal log in:\n%s", logs)
		}
		return
	}

	lines := strings.Split(logs, "\n")
	const maxLogLines = 500
	limit := min(len(lines), maxLogLines)
	for i := range limit {
		line := lines[i]
		if strings.Contains(line, "level=WARN") &&
			strings.Contains(line, refusalMsg) &&
			hasReasonToken(line, wantReason) {
			return
		}
	}
	t.Errorf("assertRefusalLog: missing WARN log for reason %q in:\n%s", wantReason, logs)
}

func executeOIDCReplay(t *testing.T, f *ssoFixture, cbURL, wantReason string, firstResp *http.Response) {
	t.Helper()
	if firstResp.Header.Get("Location") != "/" {
		t.Fatalf("first callback location = %q, want /", firstResp.Header.Get("Location"))
	}
	// Replayed callback (second use of same state) must be refused.
	resp2 := runOIDCCallback(t, f, cbURL)
	defer func() { _ = resp2.Body.Close() }()
	if loc := resp2.Header.Get("Location"); loc != "/login?sso=failed" {
		t.Errorf("replayed callback Location = %q, want /login?sso=failed", loc)
	}
	assertRefusalLog(t, f.logBuf.String(), wantReason)
}

func executeOIDCTest(t *testing.T, tc oidcTestCase) {
	t.Helper()
	idp := newFakeIDP(t)
	oidcCfg := config.OIDCOptions{
		Enabled:          true,
		IssuerURL:        idp.srv.URL,
		ClientID:         "test-client-id",
		ClientSecret:     "test-client-secret",
		RedirectURL:      "http://127.0.0.1:8080/api/v1/auth/oidc/callback",
		Scopes:           []string{"openid", "email", "profile"},
		AdminGroups:      []string{"snatcharr-admins"},
		ViewerGroups:     []string{"snatcharr-viewers"},
		GroupsClaim:      "groups",
		DiscoveryTimeout: 5 * time.Second,
	}
	f := newSSOFixture(t, oidcCfg, config.ProxyAuthOptions{})
	f.ssoSvc.SetHTTPClient(idp.srv.Client())

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	w := testdiag.Watch(5*time.Second, f.st.Pool())
	_, err := f.h.auth.Setup(ctx, "admin", "password-admin-1234")
	w.Stop()
	w.Report(t, err)
	if err != nil {
		t.Fatalf("auth.Setup: %v", err)
	}

	state, nonce := runOIDCLogin(t, f)
	idp.setClaimsModifier(func(c idpClaims) idpClaims {
		c.Nonce = nonce
		if tc.claimMod != nil {
			c = tc.claimMod(c)
		}
		return c
	})
	idp.setRejectVerifier(tc.rejectCode)

	if tc.clockAdvance > 0 {
		f.clk.Advance(tc.clockAdvance)
	}

	cbState := state
	if tc.tamperState != "" {
		cbState = tc.tamperState
	}

	cbURL := buildCallbackURL(f.srv.URL, cbState, "mock-auth-code", tc.errorParam)
	resp := runOIDCCallback(t, f, cbURL)
	defer func() { _ = resp.Body.Close() }()

	if tc.replay {
		executeOIDCReplay(t, f, cbURL, tc.wantReason, resp)
		return
	}

	loc := resp.Header.Get("Location")
	if loc != tc.wantRedirect {
		t.Errorf("Location = %q, want %q", loc, tc.wantRedirect)
	}
	checkSessionUser(t, f, tc.wantSession, tc.wantRole)
	assertRefusalLog(t, f.logBuf.String(), tc.wantReason)
}

func TestOIDC_Flow(t *testing.T) {
	t.Parallel()
	for _, tc := range buildOIDCCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			executeOIDCTest(t, tc)
		})
	}
}

func TestOIDC_DiscoveryFailure(t *testing.T) {
	t.Parallel()
	oidcCfg := config.OIDCOptions{
		Enabled:          true,
		IssuerURL:        "http://127.0.0.1:59999/unreachable",
		ClientID:         "test-client-id",
		ClientSecret:     "test-client-secret",
		RedirectURL:      "http://127.0.0.1:8080/api/v1/auth/oidc/callback",
		Scopes:           []string{"openid", "email"},
		AdminGroups:      []string{"snatcharr-admins"},
		DiscoveryTimeout: 500 * time.Millisecond,
	}
	f := newSSOFixture(t, oidcCfg, config.ProxyAuthOptions{})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	loginReq, err := http.NewRequestWithContext(ctx, http.MethodGet, f.srv.URL+PathPrefix+"/auth/oidc/login", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	loginResp, err := f.client.Do(loginReq)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = loginResp.Body.Close() }()

	if loginResp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302", loginResp.StatusCode)
	}
	if loc := loginResp.Header.Get("Location"); loc != "/login?sso=failed" {
		t.Errorf("Location = %q, want /login?sso=failed", loc)
	}

	// Verify server is alive and other routes respond.
	statusReq, err := http.NewRequestWithContext(ctx, http.MethodGet, f.srv.URL+PathPrefix+"/auth/sso", nil)
	if err != nil {
		t.Fatalf("NewRequest /auth/sso: %v", err)
	}
	statusResp, err := f.client.Do(statusReq)
	if err != nil {
		t.Fatalf("Do /auth/sso: %v", err)
	}
	defer func() { _ = statusResp.Body.Close() }()
	if statusResp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", statusResp.StatusCode)
	}
}

func TestOIDC_Revocation(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	oidcCfg := config.OIDCOptions{
		Enabled:          true,
		IssuerURL:        idp.srv.URL,
		ClientID:         "test-client-id",
		ClientSecret:     "test-client-secret",
		RedirectURL:      "http://127.0.0.1:8080/api/v1/auth/oidc/callback",
		Scopes:           []string{"openid", "email", "profile"},
		AdminGroups:      []string{"snatcharr-admins"},
		ViewerGroups:     []string{"snatcharr-viewers"},
		GroupsClaim:      "groups",
		DiscoveryTimeout: 5 * time.Second,
	}
	f := newSSOFixture(t, oidcCfg, config.ProxyAuthOptions{})
	f.ssoSvc.SetHTTPClient(idp.srv.Client())

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := f.h.auth.Setup(ctx, "admin", "password-admin-1234"); err != nil {
		t.Fatalf("auth.Setup: %v", err)
	}

	state, nonce := runOIDCLogin(t, f)
	idp.setClaimsModifier(func(c idpClaims) idpClaims {
		c.Nonce = nonce
		c.Groups = []string{"snatcharr-viewers"}
		return c
	})

	cbURL := buildCallbackURL(f.srv.URL, state, "mock-auth-code", "")
	resp := runOIDCCallback(t, f, cbURL)
	_ = resp.Body.Close()

	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Fatalf("Location = %q, want /", loc)
	}

	// Positive: unchanged cfg -> 200
	checkSessionUser(t, f, true, "viewer")

	// Set f.sec cfg ViewerGroups to a different list
	f.sec.ssoCfg.OIDC.ViewerGroups = []string{"other-groups"}

	// Next GET /auth/session -> 401
	checkSessionUser(t, f, false, "")
}
