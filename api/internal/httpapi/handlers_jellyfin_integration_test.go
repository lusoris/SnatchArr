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
	byTokenFunc                func(ctx context.Context, token string) (jellyfin.Member, error)
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

func (f *fakeJellyfinAuthenticator) ByToken(ctx context.Context, token string) (jellyfin.Member, error) {
	if f.byTokenFunc != nil {
		return f.byTokenFunc(ctx, token)
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

func hasSessionCookie(cookies []*http.Cookie) bool {
	for _, c := range cookies {
		if c.Name == "snatcharr_session" && c.Value != "" {
			return true
		}
	}
	return false
}

func TestJellyfin_TokenSession_HandlerIntegration(t *testing.T) {
	t.Parallel()
	defaultCfg := tokenSessionJellyfinOptions()
	t.Run("positive valid token starts session with cookie csrf and mapped roles", func(t *testing.T) {
		t.Parallel()
		testTokenSessionRoles(t, defaultCfg)
	})

	t.Run("negative and boundary request forms and auth failures", func(t *testing.T) {
		t.Parallel()
		testTokenSessionForms(t, defaultCfg)
	})

	t.Run("negative prod profile with plain http jellyfin url yields 403 without network call", func(t *testing.T) {
		t.Parallel()
		testTokenSessionProdHTTP(t, defaultCfg)
	})

	t.Run("negative token ip lockout yields 429 with Retry-After", func(t *testing.T) {
		t.Parallel()
		testTokenSessionLockout(t, defaultCfg)
	})
}

func tokenSessionJellyfinOptions() config.JellyfinOptions {
	return config.JellyfinOptions{Enabled: true, URL: "https://jellyfin.example.com", APIKey: "api-key-test", Timeout: 5 * time.Second, DefaultRole: domain.RoleRequester}
}

type tokenSessionRoleCase struct {
	name         string
	defaultRole  domain.Role
	member       jellyfin.Member
	wantRole     oas.UserRole
	wantUsername string
}

func tokenSessionRoleCases() []tokenSessionRoleCase {
	return []tokenSessionRoleCase{
		{
			name: "requester default", defaultRole: domain.RoleRequester, member: jellyfin.Member{ID: "jf-tok-req-1", Name: "erin"}, wantRole: oas.UserRoleRequester,
			wantUsername: "jf-erin",
		},
		{
			name: "viewer default", defaultRole: domain.RoleViewer, member: jellyfin.Member{ID: "jf-tok-view-1", Name: "frank"}, wantRole: oas.UserRoleViewer,
			wantUsername: "jf-frank",
		},
		{
			name: "admin maps to FirstAdmin", defaultRole: domain.RoleRequester, member: jellyfin.Member{ID: "jf-tok-adm-1", Name: "grace", Admin: true}, wantRole: oas.UserRoleAdmin,
			wantUsername: "admin",
		},
	}
}

func testTokenSessionRoles(t *testing.T, defaultCfg config.JellyfinOptions) {
	t.Helper()
	for _, tc := range tokenSessionRoleCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testTokenSessionRole(t, defaultCfg, tc)
		})
	}
}

func testTokenSessionRole(t *testing.T, defaultCfg config.JellyfinOptions, tc tokenSessionRoleCase) {
	t.Helper()
	cfg := defaultCfg
	cfg.DefaultRole = tc.defaultRole
	fakeAuth := &fakeJellyfinAuthenticator{byTokenFunc: func(_ context.Context, token string) (jellyfin.Member, error) {
		if token != "valid-user-token" {
			return jellyfin.Member{}, domain.ErrUnauthorized
		}
		return tc.member, nil
	}, userFunc: func(_ context.Context, id string) (jellyfin.Member, error) {
		if id != tc.member.ID {
			t.Errorf("Jellyfin user lookup id = %q, want %q", id, tc.member.ID)
		}
		return tc.member, nil
	}}
	f := setupJellyfinFixture(t, cfg, fakeAuth)
	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(`{"access_token":"valid-user-token"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/jellyfin status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var sess oas.Session
	if err := json.NewDecoder(rec.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if sess.User.Role != tc.wantRole || sess.User.Username != tc.wantUsername {
		t.Fatalf("session user = (%q, %q), want (%q, %q)", sess.User.Username, sess.User.Role, tc.wantUsername, tc.wantRole)
	}
	if sess.CsrfToken == "" {
		t.Fatal("expected non-empty csrf_token in session response")
	}
	cookies := rec.Result().Cookies()
	if !hasSessionCookie(cookies) {
		t.Fatalf("expected snatcharr_session cookie in %+v", cookies)
	}
	if sessRec := getSessionRequestHelper(f, cookies); sessRec.Code != http.StatusOK {
		t.Fatalf("GET /auth/session with token session cookie = %d, want 200", sessRec.Code)
	}
}

type tokenSessionFormCase struct {
	name       string
	enabled    bool
	body       string
	wantStatus int
	wantSubstr string
	member     jellyfin.Member
	byTokenErr error
	wantSignIn int32
}

func tokenSessionFormCases() []tokenSessionFormCase {
	cases := append(tokenSessionInitialAuthCases(), tokenSessionRequestFormCases()...)
	return append(cases, tokenSessionTailAuthCases()...)
}

func tokenSessionInitialAuthCases() []tokenSessionFormCase {
	return []tokenSessionFormCase{
		{name: "negative invalid or expired token yields 401 without session cookie", enabled: true, body: `{"access_token":"expired-token"}`, wantStatus: http.StatusUnauthorized, byTokenErr: domain.ErrUnauthorized},
		{name: "negative disabled member yields 401 without session cookie", enabled: true, body: `{"access_token":"disabled-user-token"}`, wantStatus: http.StatusUnauthorized, member: jellyfin.Member{ID: "jf-dis-tok", Name: "dis", Disabled: true}},
	}
}

func tokenSessionTailAuthCases() []tokenSessionFormCase {
	return []tokenSessionFormCase{
		{name: "boundary whitespace access_token rejected with 400", enabled: true, body: `{"access_token":"   "}`, wantStatus: http.StatusBadRequest, byTokenErr: fmt.Errorf("%w: empty jellyfin token", domain.ErrBadRequest)},
		{name: "negative jellyfin disabled with access_token yields 404", enabled: false, body: `{"access_token":"any-token"}`, wantStatus: http.StatusNotFound},
	}
}

func tokenSessionRequestFormCases() []tokenSessionFormCase {
	return []tokenSessionFormCase{
		{name: "negative both access_token and username+password yields 400", enabled: true, body: `{"access_token":"tok","username":"alice","password":"pwd"}`, wantStatus: http.StatusBadRequest, wantSubstr: "both an access token and credentials"},
		{name: "negative both access_token and username yields 400", enabled: true, body: `{"access_token":"tok","username":"alice"}`, wantStatus: http.StatusBadRequest, wantSubstr: "both an access token and credentials"},
		{name: "negative both access_token and password yields 400", enabled: true, body: `{"access_token":"tok","password":"pwd"}`, wantStatus: http.StatusBadRequest, wantSubstr: "both an access token and credentials"},
		{name: "boundary empty object yields 400", enabled: true, body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "boundary password without username or token yields 400", enabled: true, body: `{"password":"pwd"}`, wantStatus: http.StatusBadRequest},
		{name: "negative username without password yields 400 without sign in", enabled: true, body: `{"username":"alice"}`, wantStatus: http.StatusBadRequest, wantSubstr: "credentials need both username and password"},
		{name: "boundary empty password is passed to sign in", enabled: true, body: `{"username":"alice","password":""}`, wantStatus: http.StatusUnauthorized, wantSignIn: 1},
		{name: "boundary empty string access_token rejected by ogen minLength", enabled: true, body: `{"access_token":""}`, wantStatus: http.StatusUnprocessableEntity},
	}
}

func testTokenSessionForms(t *testing.T, defaultCfg config.JellyfinOptions) {
	t.Helper()
	for _, tc := range tokenSessionFormCases() {
		t.Run(tc.name, func(t *testing.T) { t.Parallel(); testTokenSessionForm(t, defaultCfg, tc) })
	}
}

func testTokenSessionForm(t *testing.T, defaultCfg config.JellyfinOptions, tc tokenSessionFormCase) {
	t.Helper()
	cfg := defaultCfg
	cfg.Enabled = tc.enabled
	var signInHits atomic.Int32
	fakeAuth := &fakeJellyfinAuthenticator{
		signInFunc: func(_ context.Context, _, _ string) (jellyfin.Member, error) {
			signInHits.Add(1)
			return jellyfin.Member{}, domain.ErrUnauthorized
		},
		byTokenFunc: func(_ context.Context, _ string) (jellyfin.Member, error) {
			if tc.byTokenErr != nil {
				return jellyfin.Member{}, tc.byTokenErr
			}
			return tc.member, nil
		},
	}
	f := setupJellyfinFixture(t, cfg, fakeAuth)
	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(tc.body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != tc.wantStatus {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	if hasSessionCookie(rec.Result().Cookies()) {
		t.Fatal("unexpected snatcharr_session cookie on failed login")
	}
	if got := signInHits.Load(); got != tc.wantSignIn {
		t.Fatalf("SignIn hits = %d, want %d", got, tc.wantSignIn)
	}
	if tc.wantSubstr != "" && !strings.Contains(rec.Body.String(), tc.wantSubstr) {
		t.Fatalf("body = %q, want substring %q", rec.Body.String(), tc.wantSubstr)
	}
}

func testTokenSessionProdHTTP(t *testing.T, defaultCfg config.JellyfinOptions) {
	t.Helper()
	var upstreamHits atomic.Int32
	jfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { upstreamHits.Add(1); w.WriteHeader(http.StatusOK) }))
	t.Cleanup(jfSrv.Close)
	f := setupJellyfinFixture(t, defaultCfg, &fakeJellyfinAuthenticator{})
	prodCfg := config.Default()
	prodCfg.Profile = config.ProfileProd
	prodCfg.Jellyfin = config.JellyfinOptions{Enabled: true, URL: jfSrv.URL, APIKey: "svc-key", Timeout: 2 * time.Second, DefaultRole: domain.RoleRequester}
	realAuth, err := jellyfin.ProvideAuthenticator(prodCfg, nil)
	if err != nil {
		t.Fatalf("ProvideAuthenticator: %v", err)
	}
	f.h.cfg = prodCfg
	f.h.jellyfin = realAuth
	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(`{"access_token":"user-tok-prod-http"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	if hasSessionCookie(rec.Result().Cookies()) {
		t.Fatal("unexpected snatcharr_session cookie on 403")
	}
	if got := upstreamHits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d, want 0", got)
	}
}

func testTokenSessionLockout(t *testing.T, defaultCfg config.JellyfinOptions) {
	t.Helper()
	fakeAuth := &fakeJellyfinAuthenticator{byTokenFunc: func(_ context.Context, _ string) (jellyfin.Member, error) {
		return jellyfin.Member{}, domain.ErrUnauthorized
	}}
	f := setupJellyfinFixture(t, defaultCfg, fakeAuth)
	for i := 1; i <= auth.LockoutMaxFails; i++ {
		req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(`{"access_token":"bad-tok"}`))
		req.RemoteAddr = "198.51.100.77:12345"
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(`{"access_token":"bad-tok"}`))
	req.RemoteAddr = "198.51.100.77:12345"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("locked status = %d, want 429 (body: %s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Fatal("expected Retry-After header on 429")
	}
}

func TestJellyfin_PlantedCheck_TokenIgnoresSpoofedUserHeaders(t *testing.T) {
	t.Parallel()

	defaultCfg := config.JellyfinOptions{
		Enabled:     true,
		URL:         "https://jellyfin.example.com",
		APIKey:      "api-key-test",
		Timeout:     5 * time.Second,
		DefaultRole: domain.RoleRequester,
	}

	fakeAuth := &fakeJellyfinAuthenticator{
		byTokenFunc: func(_ context.Context, token string) (jellyfin.Member, error) {
			if token != "real-token" {
				return jellyfin.Member{}, domain.ErrUnauthorized
			}
			return jellyfin.Member{ID: "jf-verified-me-id", Name: "verifieduser", Admin: false}, nil
		},
	}
	f := setupJellyfinFixture(t, defaultCfg, fakeAuth)

	// Pre-create another Jellyfin user matching the spoofed header ID so any header trust would sign into it.
	if _, err := f.h.auth.ProvisionJellyfinUser(t.Context(), jellyfin.Member{ID: "other-spoofed-id", Name: "victim"}, domain.RoleRequester); err != nil {
		t.Fatalf("ProvisionJellyfinUser victim: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(`{"access_token":"real-token"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Jellyfin-User-Id", "other-spoofed-id")
	req.Header.Set("X-Emby-Userid", "other-spoofed-id")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var sess oas.Session
	if err := json.NewDecoder(rec.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if sess.User.Username != "jf-verifieduser" {
		t.Fatalf("PLANTED DEFECT TRIGGERED: session username = %q, want jf-verifieduser (spoofed header trusted)", sess.User.Username)
	}

	loadReq := httptest.NewRequest(http.MethodGet, PathPrefix+"/auth/session", nil)
	for _, c := range rec.Result().Cookies() {
		loadReq.AddCookie(c)
	}
	rawSess, err := f.sessMgr.Load(loadReq)
	if err != nil {
		t.Fatalf("sessMgr.Load: %v", err)
	}
	origin, ok, oErr := auth.SSOOriginFrom(rawSess)
	if oErr != nil || !ok || origin.Subject != "jf-verified-me-id" {
		t.Fatalf("SSOOriginFrom = (%+v, %v, %v), want Subject=jf-verified-me-id", origin, ok, oErr)
	}
}

func TestJellyfin_TokenSession_RouterContract_Unit(t *testing.T) {
	t.Parallel()
	for _, tc := range routerTokenContractCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testRouterTokenContractCase(t, tc)
		})
	}

	t.Run("negative prod profile with plain http jellyfin url yields 403 without hitting upstream", func(t *testing.T) {
		t.Parallel()
		testRouterTokenProdHTTP(t)
	})
}

type routerTokenContractCase struct {
	name       string
	enabled    bool
	profile    config.Profile
	body       string
	byTokenErr error
	member     jellyfin.Member
	wantStatus int
	wantSubstr string
	wantRetry  bool
}

func routerTokenContractCases() []routerTokenContractCase {
	return []routerTokenContractCase{
		{name: "negative both access_token and username yields 400", enabled: true, profile: config.ProfileDev, body: `{"access_token":"tok","username":"alice"}`, wantStatus: http.StatusBadRequest, wantSubstr: "both an access token and credentials"},
		{name: "negative both access_token and password yields 400", enabled: true, profile: config.ProfileDev, body: `{"access_token":"tok","password":"secret"}`, wantStatus: http.StatusBadRequest, wantSubstr: "both an access token and credentials"},
		{name: "boundary empty json object yields 400", enabled: true, profile: config.ProfileDev, body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "boundary password without username or access_token yields 400", enabled: true, profile: config.ProfileDev, body: `{"password":"secret"}`, wantStatus: http.StatusBadRequest},
		{name: "boundary empty string access_token rejected by ogen minLength", enabled: true, profile: config.ProfileDev, body: `{"access_token":""}`, wantStatus: http.StatusUnprocessableEntity, wantSubstr: "access_token"},
		{name: "boundary whitespace access_token yields 400", enabled: true, profile: config.ProfileDev, body: `{"access_token":"   "}`, byTokenErr: fmt.Errorf("%w: empty jellyfin token", domain.ErrBadRequest), wantStatus: http.StatusBadRequest},
		{name: "negative invalid or expired token yields 401 without cookie", enabled: true, profile: config.ProfileDev, body: `{"access_token":"expired-tok"}`, byTokenErr: domain.ErrUnauthorized, wantStatus: http.StatusUnauthorized},
		{name: "negative disabled member yields 401 without cookie", enabled: true, profile: config.ProfileDev, body: `{"access_token":"dis-tok"}`, member: jellyfin.Member{ID: "jf-dis", Name: "dis", Disabled: true}, wantStatus: http.StatusUnauthorized},
		{name: "negative rate limited yields 429 with Retry-After", enabled: true, profile: config.ProfileDev, body: `{"access_token":"any-tok"}`, byTokenErr: fmt.Errorf("%w: account locked", domain.ErrTooManyRequests), wantStatus: http.StatusTooManyRequests, wantRetry: true},
		{name: "negative jellyfin disabled yields 404", enabled: false, profile: config.ProfileDev, body: `{"access_token":"any-tok"}`, wantStatus: http.StatusNotFound},
	}
}

func newUnitTokenRouter(t *testing.T, cfg config.Options, jfAuth jellyfin.Authenticator) http.Handler {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	authSvc, err := auth.NewServiceWithHasher(nil, testHasher, nil, nil, logger)
	if err != nil {
		t.Fatalf("NewServiceWithHasher: %v", err)
	}
	h := &Handlers{cfg: cfg, auth: authSvc, jellyfin: jfAuth, logger: logger}
	srv, err := oas.NewServer(h, &Security{}, oas.WithPathPrefix(PathPrefix), oas.WithErrorHandler(ErrorHandler(logger)))
	if err != nil {
		t.Fatalf("oas.NewServer: %v", err)
	}
	return withHTTP(srv)
}

func testRouterTokenContractCase(t *testing.T, tc routerTokenContractCase) {
	t.Helper()
	cfg := config.Default()
	cfg.Profile = tc.profile
	cfg.Jellyfin = config.JellyfinOptions{Enabled: tc.enabled, URL: "https://jellyfin.example.com", APIKey: "svc-key", Timeout: 2 * time.Second, DefaultRole: domain.RoleRequester}
	fakeAuth := &fakeJellyfinAuthenticator{byTokenFunc: func(_ context.Context, _ string) (jellyfin.Member, error) {
		if tc.byTokenErr != nil {
			return jellyfin.Member{}, tc.byTokenErr
		}
		return tc.member, nil
	}}
	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(tc.body))
	req.RemoteAddr = ""
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newUnitTokenRouter(t, cfg, fakeAuth).ServeHTTP(rec, req)
	if rec.Code != tc.wantStatus {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	if hasSessionCookie(rec.Result().Cookies()) {
		t.Fatal("unexpected snatcharr_session cookie on failed login")
	}
	if tc.wantSubstr != "" && !strings.Contains(rec.Body.String(), tc.wantSubstr) {
		t.Fatalf("body = %q, want substring %q", rec.Body.String(), tc.wantSubstr)
	}
	if tc.wantRetry && rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429")
	}
}

func testRouterTokenProdHTTP(t *testing.T) {
	t.Helper()
	var hits atomic.Int32
	jfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(http.StatusOK) }))
	t.Cleanup(jfSrv.Close)
	cfg := config.Default()
	cfg.Profile = config.ProfileProd
	cfg.Jellyfin = config.JellyfinOptions{Enabled: true, URL: jfSrv.URL, APIKey: "svc-key", Timeout: 2 * time.Second, DefaultRole: domain.RoleRequester}
	realAuth, err := jellyfin.ProvideAuthenticator(cfg, nil)
	if err != nil {
		t.Fatalf("ProvideAuthenticator: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, PathPrefix+"/auth/jellyfin", bytes.NewBufferString(`{"access_token":"tok-prod"}`))
	req.RemoteAddr = ""
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newUnitTokenRouter(t, cfg, realAuth).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
	if hits.Load() != 0 {
		t.Fatalf("upstream hits = %d, want 0", hits.Load())
	}
}
