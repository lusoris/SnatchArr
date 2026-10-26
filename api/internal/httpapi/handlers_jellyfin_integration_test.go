// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	sec := NewSecurity(f.sessMgr, f.h.cfg, f.keys, f.h.auth, f.apiKeys, f.clk, logger)
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
