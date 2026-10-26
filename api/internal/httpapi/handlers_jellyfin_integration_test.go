// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/golusoris/golusoris/httpx/csrf"
	"github.com/golusoris/golusoris/httpx/middleware"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/jellyfin"
)

type fakeJellyfinAuthenticator struct {
	signInFunc                 func(ctx context.Context, user, pw string) (jellyfin.Member, error)
	quickConnectEnabledFunc    func(ctx context.Context) (bool, error)
	initiateQuickConnectFunc   func(ctx context.Context) (code, secret string, err error)
	quickConnectAuthorizedFunc func(ctx context.Context, secret string) (bool, error)
	signInQuickConnectFunc     func(ctx context.Context, secret string) (jellyfin.Member, error)
	userFunc                   func(ctx context.Context, id string) (jellyfin.Member, error)
}

func (f *fakeJellyfinAuthenticator) SignIn(ctx context.Context, user, pw string) (jellyfin.Member, error) {
	if f.signInFunc != nil {
		return f.signInFunc(ctx, user, pw)
	}
	return jellyfin.Member{}, domain.ErrUnauthorized
}

func (f *fakeJellyfinAuthenticator) QuickConnectEnabled(ctx context.Context) (bool, error) {
	if f.quickConnectEnabledFunc != nil {
		return f.quickConnectEnabledFunc(ctx)
	}
	return true, nil
}

func (f *fakeJellyfinAuthenticator) InitiateQuickConnect(ctx context.Context) (string, string, error) {
	if f.initiateQuickConnectFunc != nil {
		return f.initiateQuickConnectFunc(ctx)
	}
	return "code-123456", "secret-abcdef", nil
}

func (f *fakeJellyfinAuthenticator) QuickConnectAuthorized(ctx context.Context, secret string) (bool, error) {
	if f.quickConnectAuthorizedFunc != nil {
		return f.quickConnectAuthorizedFunc(ctx, secret)
	}
	return true, nil
}

func (f *fakeJellyfinAuthenticator) SignInQuickConnect(ctx context.Context, secret string) (jellyfin.Member, error) {
	if f.signInQuickConnectFunc != nil {
		return f.signInQuickConnectFunc(ctx, secret)
	}
	return jellyfin.Member{ID: "jf-qc-user", Name: "qcuser", Admin: false}, nil
}

func (f *fakeJellyfinAuthenticator) User(ctx context.Context, id string) (jellyfin.Member, error) {
	if f.userFunc != nil {
		return f.userFunc(ctx, id)
	}
	return jellyfin.Member{ID: id, Name: "user"}, nil
}

type jellyfinFixture struct {
	*integrationFixture
	fakeAuth *fakeJellyfinAuthenticator
	router   chi.Router
}

func setupJellyfinFixture(t *testing.T, jfCfg config.JellyfinOptions, fakeAuth *fakeJellyfinAuthenticator) *jellyfinFixture {
	t.Helper()
	f := newIntegrationFixture(t)
	logBuf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	f.h.cfg.Jellyfin = jfCfg
	f.h.jellyfin = fakeAuth
	f.h.logger = logger

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := f.h.auth.Setup(ctx, "admin", "password-admin-1234")
	if err != nil && !strings.Contains(err.Error(), "first admin already exists") {
		t.Fatalf("setupJellyfinFixture: auth.Setup: %v", err)
	}

	csrfMW, cErr := csrf.New(csrf.Options{})
	if cErr != nil {
		t.Fatalf("setupJellyfinFixture: csrf.New: %v", cErr)
	}

	r := chi.NewRouter()
	r.Use(
		middleware.RequestID,
		middleware.Recover(logger),
		middleware.Logger(logger, f.clk),
	)
	reval := jellyfin.NewRevalidator(fakeAuth, f.clk, f.h.cfg.Jellyfin.DefaultRole, 2*time.Second, logger)
	sec := NewSecurity(f.sessMgr, f.h.cfg, f.keys, f.h.auth, f.apiKeys, f.clk, logger, reval)
	if mErr := Mount(r, MountParams{Logger: logger, Handlers: f.h, Security: sec, CSRF: csrfMW, Modules: f.h.modules}); mErr != nil {
		t.Fatalf("setupJellyfinFixture: Mount: %v", mErr)
	}

	return &jellyfinFixture{
		integrationFixture: f,
		fakeAuth:           fakeAuth,
		router:             r,
	}
}

func TestJellyfin_HandlerIntegration(t *testing.T) {
	t.Parallel()

	defaultCfg := config.JellyfinOptions{
		Enabled:     true,
		URL:         "http://jellyfin.local:8096",
		APIKey:      "api-key-test",
		Timeout:     5 * time.Second,
		DefaultRole: domain.RoleRequester,
	}

	t.Run("admin member maps to session role admin FirstAdmin", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(ctx context.Context, user, pw string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: "jf-admin-id", Name: "jelly-admin", Admin: true}, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)

		body := `{"username":"jelly-admin","password":"password"}`
		req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("POST /auth/jellyfin status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var sess oas.Session
		if err := json.NewDecoder(rec.Body).Decode(&sess); err != nil {
			t.Fatalf("decode session: %v", err)
		}
		if sess.User.Role != oas.UserRoleAdmin {
			t.Errorf("role = %q, want %q", sess.User.Role, oas.UserRoleAdmin)
		}
		if sess.User.Username != "admin" {
			t.Errorf("username = %q, want first admin handle 'admin'", sess.User.Username)
		}
		cookies := rec.Result().Cookies()
		if len(cookies) == 0 {
			t.Fatal("expected session cookie set")
		}
	})

	t.Run("non-admin maps to default_role requester", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(ctx context.Context, user, pw string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: "jf-user-1", Name: "alice", Admin: false}, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)

		body := `{"username":"alice","password":"password"}`
		req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("POST /auth/jellyfin status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var sess oas.Session
		if err := json.NewDecoder(rec.Body).Decode(&sess); err != nil {
			t.Fatalf("decode session: %v", err)
		}
		if sess.User.Role != oas.UserRoleRequester {
			t.Errorf("role = %q, want %q", sess.User.Role, oas.UserRoleRequester)
		}
		if sess.User.Username != "jf-alice" {
			t.Errorf("username = %q, want jf-alice", sess.User.Username)
		}
	})

	t.Run("default_role viewer yields viewer", func(t *testing.T) {
		t.Parallel()
		viewerCfg := defaultCfg
		viewerCfg.DefaultRole = domain.RoleViewer
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(ctx context.Context, user, pw string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: "jf-user-2", Name: "bob", Admin: false}, nil
			},
		}
		f := setupJellyfinFixture(t, viewerCfg, fakeAuth)

		body := `{"username":"bob","password":"password"}`
		req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("POST /auth/jellyfin status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var sess oas.Session
		if err := json.NewDecoder(rec.Body).Decode(&sess); err != nil {
			t.Fatalf("decode session: %v", err)
		}
		if sess.User.Role != oas.UserRoleViewer {
			t.Errorf("role = %q, want %q", sess.User.Role, oas.UserRoleViewer)
		}
		if sess.User.Username != "jf-bob" {
			t.Errorf("username = %q, want jf-bob", sess.User.Username)
		}
	})

	t.Run("wrong password yields 401 problem+json", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(ctx context.Context, user, pw string) (jellyfin.Member, error) {
				return jellyfin.Member{}, domain.ErrUnauthorized
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)

		body := `{"username":"alice","password":"wrong-password"}`
		req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("POST /auth/jellyfin status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "application/problem+json") {
			t.Errorf("Content-Type = %q, want application/problem+json", ct)
		}
	})

	t.Run("upstream error yields 500 problem+json", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(context.Context, string, string) (jellyfin.Member, error) {
				return jellyfin.Member{}, errors.New("upstream jellyfin connection refused")
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)

		body := `{"username":"alice","password":"password"}`
		req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("POST /auth/jellyfin status = %d, want %d", rec.Code, http.StatusInternalServerError)
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "application/problem+json") {
			t.Errorf("Content-Type = %q, want application/problem+json", ct)
		}
	})

	t.Run("disabled provider yields 404 problem+json", func(t *testing.T) {
		t.Parallel()
		disabledCfg := defaultCfg
		disabledCfg.Enabled = false
		fakeAuth := &fakeJellyfinAuthenticator{}
		f := setupJellyfinFixture(t, disabledCfg, fakeAuth)

		paths := []string{
			PathPrefix + "/auth/jellyfin",
			PathPrefix + "/auth/jellyfin/quickconnect/initiate",
			PathPrefix + "/auth/jellyfin/quickconnect/authenticate",
		}
		for _, p := range paths {
			req := httptest.NewRequest(http.MethodPost, p, bytes.NewBufferString(`{"username":"u","password":"p","secret":"s"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			f.router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("POST %s status = %d, want 404", p, rec.Code)
			}
			ct := rec.Header().Get("Content-Type")
			if !strings.Contains(ct, "application/problem+json") {
				t.Errorf("POST %s Content-Type = %q, want application/problem+json", p, ct)
			}
		}
	})

	t.Run("quick connect pending yields 409", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			quickConnectAuthorizedFunc: func(ctx context.Context, secret string) (bool, error) {
				return false, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)

		body := `{"secret":"pending-secret"}`
		req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin/quickconnect/authenticate", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("POST /auth/jellyfin/quickconnect/authenticate status = %d, want %d", rec.Code, http.StatusConflict)
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "application/problem+json") {
			t.Errorf("Content-Type = %q, want application/problem+json", ct)
		}
	})

	t.Run("quick connect authorized yields session", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			quickConnectAuthorizedFunc: func(ctx context.Context, secret string) (bool, error) {
				return true, nil
			},
			signInQuickConnectFunc: func(ctx context.Context, secret string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: "jf-qc-1", Name: "carol", Admin: false}, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)

		body := `{"secret":"authorized-secret"}`
		req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin/quickconnect/authenticate", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("POST /auth/jellyfin/quickconnect/authenticate status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		var sess oas.Session
		if err := json.NewDecoder(rec.Body).Decode(&sess); err != nil {
			t.Fatalf("decode session: %v", err)
		}
		if sess.User.Role != oas.UserRoleRequester {
			t.Errorf("role = %q, want requester", sess.User.Role)
		}
		if sess.User.Username != "jf-carol" {
			t.Errorf("username = %q, want jf-carol", sess.User.Username)
		}
		cookies := rec.Result().Cookies()
		if len(cookies) == 0 {
			t.Fatal("expected session cookie set")
		}

		// Re-read session with cookie
		sessReq := httptest.NewRequest(http.MethodGet, PathPrefix+"/auth/session", nil)
		for _, c := range cookies {
			sessReq.AddCookie(c)
		}
		sessRec := httptest.NewRecorder()
		f.router.ServeHTTP(sessRec, sessReq)
		if sessRec.Code != http.StatusOK {
			t.Fatalf("GET /auth/session status = %d, want 200", sessRec.Code)
		}
	})

	t.Run("quick connect disabled on server yields 404", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			quickConnectEnabledFunc: func(ctx context.Context) (bool, error) {
				return false, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)

		req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin/quickconnect/initiate", nil)
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("POST /auth/jellyfin/quickconnect/initiate status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "application/problem+json") {
			t.Errorf("Content-Type = %q, want application/problem+json", ct)
		}
	})
}

func TestJellyfin_PlantedCheck_NonAdminNeverAdmin(t *testing.T) {
	t.Parallel()

	defaultCfg := config.JellyfinOptions{
		Enabled:     true,
		URL:         "http://jellyfin.local:8096",
		APIKey:      "api-key-test",
		Timeout:     5 * time.Second,
		DefaultRole: domain.RoleRequester,
	}

	fakeAuth := &fakeJellyfinAuthenticator{
		signInFunc: func(ctx context.Context, user, pw string) (jellyfin.Member, error) {
			// Explicitly non-admin member
			return jellyfin.Member{ID: "jf-non-admin-id", Name: "dave", Admin: false}, nil
		},
	}
	f := setupJellyfinFixture(t, defaultCfg, fakeAuth)

	body := `{"username":"dave","password":"password"}`
	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var sess oas.Session
	if err := json.NewDecoder(rec.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	// Planted check: mapping every member to admin must fail
	if sess.User.Role == oas.UserRoleAdmin {
		t.Fatalf("PLANTED DEFECT TRIGGERED: non-admin member received admin role!")
	}
	if sess.User.Role != oas.UserRoleRequester {
		t.Fatalf("role = %q, want requester", sess.User.Role)
	}
}

func TestGetSSO_QuickConnectCache(t *testing.T) {
	t.Parallel()
	var callCount atomic.Int32
	fakeAuth := &fakeJellyfinAuthenticator{
		quickConnectEnabledFunc: func(context.Context) (bool, error) {
			callCount.Add(1)
			return true, nil
		},
	}
	f := setupJellyfinFixture(t, config.JellyfinOptions{Enabled: true}, fakeAuth)
	ctx := t.Context()

	assertSSO(t, f, ctx, true, 1, &callCount)
	f.clk.Advance(30 * time.Second)
	assertSSO(t, f, ctx, true, 1, &callCount)
	f.clk.Advance(31 * time.Second)
	assertSSO(t, f, ctx, true, 2, &callCount)

	f.clk.Advance(61 * time.Second)
	fakeAuth.quickConnectEnabledFunc = func(context.Context) (bool, error) {
		callCount.Add(1)
		return false, errors.New("upstream jellyfin error")
	}
	assertSSO(t, f, ctx, false, 3, &callCount)
	f.clk.Advance(30 * time.Second)
	assertSSO(t, f, ctx, false, 3, &callCount)
}

func assertSSO(t *testing.T, f *jellyfinFixture, ctx context.Context, wantQC bool, wantCalls int32, calls *atomic.Int32) {
	t.Helper()
	sso, err := f.h.GetSSO(ctx)
	if err != nil {
		t.Fatalf("GetSSO: %v", err)
	}
	if sso.JellyfinQuickConnect.Value != wantQC {
		t.Fatalf("JellyfinQuickConnect = %v, want %v", sso.JellyfinQuickConnect.Value, wantQC)
	}
	if got := calls.Load(); got != wantCalls {
		t.Fatalf("callCount = %d, want %d", got, wantCalls)
	}
}

func loginJellyfinHelper(t *testing.T, f *jellyfinFixture, username string) []*http.Cookie {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":"pwd"}`, username)
	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/jellyfin failed: %d, body: %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie from login")
	}
	return cookies
}

func getSessionRequestHelper(f *jellyfinFixture, cookies []*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, PathPrefix+"/auth/session", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func TestJellyfin_CookieAuthRevalidation(t *testing.T) {
	t.Parallel()

	defaultCfg := config.JellyfinOptions{
		Enabled:     true,
		URL:         "http://jellyfin.local:8096",
		APIKey:      "api-key-test",
		Timeout:     5 * time.Second,
		DefaultRole: domain.RoleRequester,
	}

	t.Run("fresh cache skips call and 15m boundary", func(t *testing.T) {
		t.Parallel()
		var userCalls atomic.Int32
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(_ context.Context, u, pw string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: "jf-reval-1", Name: u, Admin: false}, nil
			},
			userFunc: func(_ context.Context, id string) (jellyfin.Member, error) {
				userCalls.Add(1)
				return jellyfin.Member{ID: id, Name: "reval1", Admin: false}, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)
		cookies := loginJellyfinHelper(t, f, "reval1")

		// First request: cache empty, User called once
		rec1 := getSessionRequestHelper(f, cookies)
		if rec1.Code != http.StatusOK {
			t.Fatalf("first request status = %d, want 200", rec1.Code)
		}
		if calls := userCalls.Load(); calls != 1 {
			t.Fatalf("first request calls = %d, want 1", calls)
		}

		// Second request immediately: fresh cache, no User call
		rec2 := getSessionRequestHelper(f, cookies)
		if rec2.Code != http.StatusOK {
			t.Fatalf("second request status = %d, want 200", rec2.Code)
		}
		if calls := userCalls.Load(); calls != 1 {
			t.Fatalf("second request calls = %d, want 1 (cache hit)", calls)
		}

		// Advance 14m59s: boundary check, still fresh cache
		f.clk.Advance(14*time.Minute + 59*time.Second)
		rec3 := getSessionRequestHelper(f, cookies)
		if rec3.Code != http.StatusOK {
			t.Fatalf("at 14m59s status = %d, want 200", rec3.Code)
		}
		if calls := userCalls.Load(); calls != 1 {
			t.Fatalf("at 14m59s calls = %d, want 1 (cache hit)", calls)
		}

		// Advance 1s (total 15m): cache expired, User called
		f.clk.Advance(1 * time.Second)
		rec4 := getSessionRequestHelper(f, cookies)
		if rec4.Code != http.StatusOK {
			t.Fatalf("at 15m status = %d, want 200", rec4.Code)
		}
		if calls := userCalls.Load(); calls != 2 {
			t.Fatalf("at 15m calls = %d, want 2 (cache expired)", calls)
		}
	})

	t.Run("missing member revokes session with 401", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(_ context.Context, u, pw string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: "jf-missing-id", Name: u, Admin: false}, nil
			},
			userFunc: func(_ context.Context, id string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: id, Name: "missing", Admin: false}, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)
		cookies := loginJellyfinHelper(t, f, "missinguser")

		// First request to populate cache
		rec1 := getSessionRequestHelper(f, cookies)
		if rec1.Code != http.StatusOK {
			t.Fatalf("init request = %d, want 200", rec1.Code)
		}

		// User deleted from Jellyfin: User returns ErrNotFound
		fakeAuth.userFunc = func(context.Context, string) (jellyfin.Member, error) {
			return jellyfin.Member{}, domain.ErrNotFound
		}

		// Advance past 15m
		f.clk.Advance(15 * time.Minute)
		rec2 := getSessionRequestHelper(f, cookies)
		if rec2.Code != http.StatusUnauthorized {
			t.Fatalf("missing user status = %d, want 401", rec2.Code)
		}

		// Subsequent request must also fail with 401 because session is revoked from DB
		rec3 := getSessionRequestHelper(f, cookies)
		if rec3.Code != http.StatusUnauthorized {
			t.Fatalf("subsequent status = %d, want 401", rec3.Code)
		}
	})

	t.Run("disabled member revokes session with 401", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(_ context.Context, u, pw string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: "jf-dis-id", Name: u, Admin: false}, nil
			},
			userFunc: func(_ context.Context, id string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: id, Name: "dis", Admin: false}, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)
		cookies := loginJellyfinHelper(t, f, "disuser")

		rec1 := getSessionRequestHelper(f, cookies)
		if rec1.Code != http.StatusOK {
			t.Fatalf("init request = %d, want 200", rec1.Code)
		}

		// Member disabled in Jellyfin
		fakeAuth.userFunc = func(context.Context, string) (jellyfin.Member, error) {
			return jellyfin.Member{ID: "jf-dis-id", Name: "dis", Disabled: true}, nil
		}

		f.clk.Advance(15 * time.Minute)
		rec2 := getSessionRequestHelper(f, cookies)
		if rec2.Code != http.StatusUnauthorized {
			t.Fatalf("disabled user status = %d, want 401", rec2.Code)
		}
	})

	t.Run("role changed admin to requester revokes session", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(_ context.Context, u, pw string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: "jf-admin-demo", Name: u, Admin: true}, nil
			},
			userFunc: func(_ context.Context, id string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: id, Name: "admin-demo", Admin: true}, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)
		cookies := loginJellyfinHelper(t, f, "admin-demo")

		rec1 := getSessionRequestHelper(f, cookies)
		if rec1.Code != http.StatusOK {
			t.Fatalf("init request = %d, want 200", rec1.Code)
		}

		// Admin revoked in Jellyfin (now Admin: false)
		fakeAuth.userFunc = func(context.Context, string) (jellyfin.Member, error) {
			return jellyfin.Member{ID: "jf-admin-demo", Name: "admin-demo", Admin: false}, nil
		}

		f.clk.Advance(15 * time.Minute)
		rec2 := getSessionRequestHelper(f, cookies)
		if rec2.Code != http.StatusUnauthorized {
			t.Fatalf("role changed status = %d, want 401", rec2.Code)
		}
	})

	t.Run("role changed requester to admin revokes session", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(_ context.Context, u, pw string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: "jf-req-demo", Name: u, Admin: false}, nil
			},
			userFunc: func(_ context.Context, id string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: id, Name: "req-demo", Admin: false}, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)
		cookies := loginJellyfinHelper(t, f, "req-demo")

		rec1 := getSessionRequestHelper(f, cookies)
		if rec1.Code != http.StatusOK {
			t.Fatalf("init request = %d, want 200", rec1.Code)
		}

		// User promoted to admin in Jellyfin
		fakeAuth.userFunc = func(context.Context, string) (jellyfin.Member, error) {
			return jellyfin.Member{ID: "jf-req-demo", Name: "req-demo", Admin: true}, nil
		}

		f.clk.Advance(15 * time.Minute)
		rec2 := getSessionRequestHelper(f, cookies)
		if rec2.Code != http.StatusUnauthorized {
			t.Fatalf("role changed status = %d, want 401", rec2.Code)
		}
	})

	t.Run("transient error keeps session and retries next request", func(t *testing.T) {
		t.Parallel()
		var userCalls atomic.Int32
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(_ context.Context, u, pw string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: "jf-trans-id", Name: u, Admin: false}, nil
			},
			userFunc: func(_ context.Context, id string) (jellyfin.Member, error) {
				userCalls.Add(1)
				return jellyfin.Member{ID: id, Name: "trans", Admin: false}, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)
		cookies := loginJellyfinHelper(t, f, "transuser")

		rec1 := getSessionRequestHelper(f, cookies)
		if rec1.Code != http.StatusOK {
			t.Fatalf("init request = %d, want 200", rec1.Code)
		}
		if calls := userCalls.Load(); calls != 1 {
			t.Fatalf("init calls = %d, want 1", calls)
		}

		// Set transient error
		fakeAuth.userFunc = func(context.Context, string) (jellyfin.Member, error) {
			userCalls.Add(1)
			return jellyfin.Member{}, io.ErrUnexpectedEOF
		}

		f.clk.Advance(15 * time.Minute)
		rec2 := getSessionRequestHelper(f, cookies)
		if rec2.Code != http.StatusOK {
			t.Fatalf("transient error status = %d, want 200 (session kept)", rec2.Code)
		}
		if calls := userCalls.Load(); calls != 2 {
			t.Fatalf("calls after transient error = %d, want 2", calls)
		}

		// Next request must call userFunc again (retry!)
		rec3 := getSessionRequestHelper(f, cookies)
		if rec3.Code != http.StatusOK {
			t.Fatalf("retry status = %d, want 200", rec3.Code)
		}
		if calls := userCalls.Load(); calls != 3 {
			t.Fatalf("calls on immediate retry = %d, want 3", calls)
		}
	})

	t.Run("jellyfin disabled revokes with 401", func(t *testing.T) {
		t.Parallel()
		fakeAuth := &fakeJellyfinAuthenticator{
			signInFunc: func(_ context.Context, u, pw string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: "jf-dis-jf-id", Name: u, Admin: false}, nil
			},
			userFunc: func(_ context.Context, id string) (jellyfin.Member, error) {
				return jellyfin.Member{ID: id, Name: "dis-jf", Admin: false}, nil
			},
		}
		f := setupJellyfinFixture(t, defaultCfg, fakeAuth)
		cookies := loginJellyfinHelper(t, f, "disjfuser")

		rec1 := getSessionRequestHelper(f, cookies)
		if rec1.Code != http.StatusOK {
			t.Fatalf("init request = %d, want 200", rec1.Code)
		}

		// Disable Jellyfin in security config
		cfgDisabled := f.h.cfg
		cfgDisabled.Jellyfin.Enabled = false
		secDisabled := NewSecurity(f.sessMgr, cfgDisabled, f.keys, f.h.auth, f.apiKeys, f.clk, f.h.logger, nil)

		// Make request through secDisabled
		req := httptest.NewRequest(http.MethodGet, PathPrefix+"/auth/session", nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		ctx := withHTTPTest(t.Context(), httptest.NewRecorder(), req)
		_, err := secDisabled.HandleCookieAuth(ctx, "GetSession", oas.CookieAuth{})
		if !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("expected ErrUnauthorized when Jellyfin disabled, got %v", err)
		}

		// Session should now be revoked in the store (Load returns empty session without user)
		sess, err := f.sessMgr.Load(req)
		if err != nil || sess.Get(auth.SessionUserKey) != nil {
			t.Fatal("expected session to be revoked when Jellyfin disabled")
		}
	})
}

func TestJellyfin_QuickConnect_WrongSecretRefused(t *testing.T) {
	t.Parallel()
	defaultCfg := config.JellyfinOptions{
		Enabled:     true,
		URL:         "http://jellyfin.local:8096",
		APIKey:      "api-key-test",
		Timeout:     5 * time.Second,
		DefaultRole: domain.RoleRequester,
	}
	fakeAuth := &fakeJellyfinAuthenticator{
		quickConnectAuthorizedFunc: func(context.Context, string) (bool, error) {
			return true, nil
		},
		signInQuickConnectFunc: func(context.Context, string) (jellyfin.Member, error) {
			return jellyfin.Member{}, domain.ErrUnauthorized
		},
	}
	f := setupJellyfinFixture(t, defaultCfg, fakeAuth)

	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin/quickconnect/authenticate", bytes.NewBufferString(`{"secret":"invalid-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /auth/jellyfin/quickconnect/authenticate status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
}
