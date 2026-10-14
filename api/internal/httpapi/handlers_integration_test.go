// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/auth/session"
	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

type integrationFixture struct {
	h       *Handlers
	st      *store.Store
	clk     *clockwork.FakeClock
	probe   *stubProber
	sessMgr *session.Manager
	admin   domain.User
	viewer  domain.User
}

func buildHandlers(t *testing.T, st *store.Store, clk clock.Clock, ids id.Generator, authSvc *auth.Service, sessMgr *session.Manager, probe *stubProber, m *metrics.Metrics, logger *slog.Logger) *Handlers {
	t.Helper()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("buildHandlers: encryptor: %v", err)
	}
	instSvc := instances.New(st, enc, clk, ids, probe, logger)
	polSvc := policies.New(st, clk)
	bus := events.New(logger)
	rec := snatch.NewRecorder(st, clk, bus)
	runs := snatch.NewRuns(st, clk, ids)
	workers := snatch.NewWorkers(st, clk)
	budget := snatch.NewBudget(st, clk)
	memory := snatch.NewMemory(st, clk)
	planner := snatch.NewPlanner(st, clk, runs, rec, logger, m, snatch.AllowAll{})
	schedules := snatch.NewSchedules(st, clk, ids)
	settingsSvc := settings.New(st, clk)
	cfg := config.Options{
		Profile: config.ProfileDev,
		Session: config.SessionOptions{TTL: 8 * time.Hour, Secure: false},
	}
	build := buildinfo.Info{Version: "test-ver", Commit: "test-commit", Date: "2026-10-14"}
	return &Handlers{
		build:     build,
		cfg:       cfg,
		clk:       clk,
		auth:      authSvc,
		sessions:  sessMgr,
		instances: instSvc,
		policies:  polSvc,
		runs:      runs,
		workers:   workers,
		rec:       rec,
		budget:    budget,
		memory:    memory,
		planner:   planner,
		schedules: schedules,
		settings:  settingsSvc,
		logger:    logger,
	}
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	ids := id.New()
	hasher := crypto.NewPasswordHasher(1)
	logger := slog.New(slog.DiscardHandler)
	authSvc, err := auth.NewService(st, hasher, clk, ids, logger)
	if err != nil {
		t.Fatalf("newIntegrationFixture: auth.NewService: %v", err)
	}
	sessionStore := auth.NewPgSessionStore(st, clk)
	sessMgr, err := session.NewManager(sessionStore, session.Options{
		CookieName:          "snatcharr_session",
		TTL:                 8 * time.Hour,
		AllowInsecureCookie: true,
	})
	if err != nil {
		t.Fatalf("newIntegrationFixture: session.NewManager: %v", err)
	}
	probe := &stubProber{res: arrclient.Result{AppName: "Sonarr", Version: "4.0.10.2544"}}
	m, _ := metricstest.New(t)
	h := buildHandlers(t, st, clk, ids, authSvc, sessMgr, probe, m, logger)
	return &integrationFixture{
		h:       h,
		st:      st,
		clk:     clk,
		probe:   probe,
		sessMgr: sessMgr,
		admin:   domain.User{ID: uuid.New(), Username: "admin", Role: domain.RoleAdmin},
		viewer:  domain.User{ID: uuid.New(), Username: "viewer", Role: domain.RoleViewer},
	}
}

func withHTTPTest(ctx context.Context, w http.ResponseWriter, r *http.Request) context.Context {
	ctx = context.WithValue(ctx, ctxKeyWriter, w)
	return context.WithValue(ctx, ctxKeyRequest, r)
}

func seedTestInstance(t *testing.T, f *integrationFixture) uuid.UUID {
	t.Helper()
	inst, err := f.h.instances.Create(t.Context(), instances.Input{
		Kind:    domain.KindSonarr,
		Name:    "storetest-" + uuid.NewString(),
		BaseURL: "http://" + uuid.NewString() + ".invalid:8989",
		APIKey:  "secret-test-key",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("seedTestInstance: %v", err)
	}
	return inst.ID
}

func validInstanceInput() oas.InstanceInput {
	return oas.InstanceInput{
		Kind:    oas.AppKindSonarr,
		Name:    "tv-shows",
		BaseURL: "http://sonarr.invalid:8989",
		APIKey:  "secret-test-key",
		Enabled: oas.NewOptBool(true),
	}
}

func validScheduleInput() oas.ScheduleInput {
	return oas.ScheduleInput{
		Name:     "night-pause",
		Days:     []int{0, 1, 2, 3, 4, 5, 6},
		Start:    "22:00",
		End:      "06:00",
		Tz:       "Europe/Berlin",
		Action:   oas.ScheduleInputActionCapOverride,
		CapValue: oas.NewOptInt(15),
		Enabled:  true,
	}
}

func assertSessionUserAndCsrf(t *testing.T, f *integrationFixture, cookies []*http.Cookie, wantUser domain.User) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	ctx := withHTTPTest(t.Context(), httptest.NewRecorder(), req)
	sec := NewSecurity(f.sessMgr, nil, f.h.auth)
	authCtx, err := sec.HandleCookieAuth(ctx, "GetSession", oas.CookieAuth{})
	if err != nil {
		t.Fatalf("HandleCookieAuth() = %v", err)
	}
	sess, err := f.h.GetSession(authCtx)
	if err != nil {
		t.Fatalf("GetSession() = %v", err)
	}
	if sess.Response.User.Username != wantUser.Username || sess.Response.User.ID != wantUser.ID || sess.Response.User.Role != oas.UserRole(wantUser.Role) {
		t.Fatalf("GetSession() user = %+v, want %+v", sess.Response.User, wantUser)
	}
	csrfHeader, ok := sess.XCSRFToken.Get()
	if !ok || csrfHeader == "" || csrfHeader != sess.Response.CsrfToken {
		t.Fatalf("GetSession() XCSRFToken = %q (ok=%v), want non-empty %q", csrfHeader, ok, sess.Response.CsrfToken)
	}
	wantExpiry := f.clk.Now().Add(f.h.cfg.Session.TTL)
	if !sess.Response.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("GetSession() ExpiresAt = %v, want %v", sess.Response.ExpiresAt, wantExpiry)
	}
}

func assertSessionDestroyed(t *testing.T, f *integrationFixture, cookies []*http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	ctx := withHTTPTest(t.Context(), httptest.NewRecorder(), req)
	sec := NewSecurity(f.sessMgr, nil, f.h.auth)
	if _, err := sec.HandleCookieAuth(ctx, "GetSession", oas.CookieAuth{}); err == nil {
		t.Fatal("expected session to be destroyed after logout, but old cookie was valid")
	}
}

func TestGetSystemAndSetupStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		seen    bool
		wantVer string
	}{
		{"positive system status clean", false, "test-ver"},
		{"positive system status worker seen", true, "test-ver"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			ctx := t.Context()
			if tc.seen {
				if err := f.h.workers.Seen(ctx, "w-test"); err != nil {
					t.Fatalf("workers.Seen() = %v", err)
				}
			}
			checkSystemStatusBefore(t, f, tc.wantVer, tc.seen)
			checkSystemStatusAfter(t, f)
		})
	}
}

func checkSystemStatusBefore(t *testing.T, f *integrationFixture, wantVer string, wantSeen bool) {
	t.Helper()
	ctx := t.Context()
	gotSys, err := f.h.GetSystemStatus(ctx)
	if err != nil || gotSys.Version != wantVer {
		t.Fatalf("GetSystemStatus() = %+v, %v; want version %q", gotSys, err, wantVer)
	}
	if gotSys.Profile != oas.SystemStatusProfile(f.h.cfg.Profile) {
		t.Fatalf("GetSystemStatus() profile = %s, want %s", gotSys.Profile, f.h.cfg.Profile)
	}
	if gotSys.SetupComplete {
		t.Fatalf("GetSystemStatus() SetupComplete = true before setup, want false")
	}
	if _, set := gotSys.WorkerLastContactAt.Get(); set != wantSeen {
		t.Fatalf("WorkerLastContactAt.Get() set = %v, want %v", set, wantSeen)
	}
	gotSetup, err := f.h.GetSetupStatus(ctx)
	if err != nil || gotSetup.SetupComplete {
		t.Fatalf("GetSetupStatus() = %+v, %v; want setupComplete=false", gotSetup, err)
	}
}

func checkSystemStatusAfter(t *testing.T, f *integrationFixture) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/setup", nil)
	ctx := withHTTPTest(t.Context(), httptest.NewRecorder(), req)
	setupReq := oas.SetupRequest{Username: "first-admin", Password: "correct-horse-battery-staple"}
	if _, err := f.h.CompleteSetup(ctx, &setupReq); err != nil {
		t.Fatalf("CompleteSetup() = %v", err)
	}
	gotSys, err := f.h.GetSystemStatus(t.Context())
	if err != nil || !gotSys.SetupComplete {
		t.Fatalf("GetSystemStatus() after setup = %+v, %v; want setupComplete=true", gotSys, err)
	}
	gotSetup, err := f.h.GetSetupStatus(t.Context())
	if err != nil || !gotSetup.SetupComplete {
		t.Fatalf("GetSetupStatus() after setup = %+v, %v; want setupComplete=true", gotSetup, err)
	}
}

func TestCompleteSetup(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		req     oas.SetupRequest
		httpCtx bool
		preUser bool
		wantErr error
	}{
		{"positive first admin", oas.SetupRequest{Username: "first-admin", Password: "correct-horse-battery-staple"}, true, false, nil},
		{"negative already setup", oas.SetupRequest{Username: "second-admin", Password: "correct-horse-battery-staple"}, true, true, domain.ErrConflict},
		{"negative no http context", oas.SetupRequest{Username: "first-admin", Password: "correct-horse-battery-staple"}, false, false, nil},
		{"boundary short password", oas.SetupRequest{Username: "first-admin", Password: "short"}, true, false, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			testCompleteSetupCase(t, f, tc.req, tc.httpCtx, tc.preUser, tc.wantErr)
		})
	}
}

func testCompleteSetupCase(t *testing.T, f *integrationFixture, req oas.SetupRequest, httpCtx, preUser bool, wantErr error) {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx := t.Context()
	if httpCtx {
		ctx = withHTTPTest(ctx, rec, httptest.NewRequest(http.MethodPost, "/auth/setup", nil))
	}
	if preUser {
		firstCtx := withHTTPTest(t.Context(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/auth/setup", nil))
		firstReq := oas.SetupRequest{Username: "existing-admin", Password: "correct-horse-battery-staple"}
		if _, err := f.h.CompleteSetup(firstCtx, &firstReq); err != nil {
			t.Fatalf("pre-setup failed: %v", err)
		}
	}
	got, err := f.h.CompleteSetup(ctx, &req)
	if wantErr != nil {
		handleSetupRefused(t, f, err, wantErr, preUser)
		return
	}
	if !httpCtx {
		if err == nil {
			t.Fatal("CompleteSetup() without http context wanted error, got nil")
		}
		return
	}
	if err != nil {
		t.Fatalf("CompleteSetup() err = %v", err)
	}
	assertSetupSessionResult(t, f, got, req.Username, rec.Result().Cookies())
}

func handleSetupRefused(t *testing.T, f *integrationFixture, err, wantErr error, preUser bool) {
	t.Helper()
	if !errors.Is(err, wantErr) {
		t.Fatalf("CompleteSetup() err = %v, want %v", err, wantErr)
	}
	if !preUser {
		status, statErr := f.h.GetSetupStatus(t.Context())
		if statErr != nil || status.SetupComplete {
			t.Fatalf("after refused setup: status = %+v, %v", status, statErr)
		}
	}
}

func assertSetupSessionResult(t *testing.T, f *integrationFixture, got *oas.Session, wantUser string, cookies []*http.Cookie) {
	t.Helper()
	if got.User.Username != wantUser || got.User.ID == uuid.Nil || got.User.Role != oas.UserRoleAdmin {
		t.Fatalf("CompleteSetup() user = %+v, want %s (admin)", got.User, wantUser)
	}
	if got.CsrfToken == "" {
		t.Fatal("CompleteSetup() CsrfToken is empty")
	}
	wantExpiry := f.clk.Now().Add(f.h.cfg.Session.TTL)
	if !got.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("CompleteSetup() ExpiresAt = %v, want %v", got.ExpiresAt, wantExpiry)
	}
	assertSessionUserAndCsrf(t, f, cookies, domain.User{ID: got.User.ID, Username: wantUser, Role: domain.RoleAdmin})
}

func TestLogin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		user    string
		pass    string
		httpCtx bool
		wantErr error
	}{
		{"positive valid credentials", "first-admin", "correct-horse-battery-staple", true, nil},
		{"negative wrong password", "first-admin", "wrong-password-1234", true, domain.ErrUnauthorized},
		{"negative unknown user", "unknown-user", "correct-horse-battery-staple", true, domain.ErrUnauthorized},
		{"negative no http context", "first-admin", "correct-horse-battery-staple", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			testLoginCase(t, f, tc.user, tc.pass, tc.httpCtx, tc.wantErr)
		})
	}
}

func testLoginCase(t *testing.T, f *integrationFixture, user, pass string, httpCtx bool, wantErr error) {
	t.Helper()
	seedCtx := withHTTPTest(t.Context(), httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/auth/setup", nil))
	adminSess, err := f.h.CompleteSetup(seedCtx, &oas.SetupRequest{Username: "first-admin", Password: "correct-horse-battery-staple"})
	if err != nil {
		t.Fatalf("CompleteSetup() = %v", err)
	}
	rec := httptest.NewRecorder()
	ctx := t.Context()
	if httpCtx {
		ctx = withHTTPTest(ctx, rec, httptest.NewRequest(http.MethodPost, "/auth/login", nil))
	}
	got, err := f.h.Login(ctx, &oas.LoginRequest{Username: user, Password: pass})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("Login() error = %v, want %v", err, wantErr)
		}
		return
	}
	if !httpCtx {
		if err == nil {
			t.Fatal("Login() without http context wanted error, got nil")
		}
		return
	}
	if err != nil {
		t.Fatalf("Login() err = %v", err)
	}
	assertLoginSessionResult(t, f, got, user, adminSess.User.ID, rec.Result().Cookies())
}

func assertLoginSessionResult(t *testing.T, f *integrationFixture, got *oas.Session, wantUser string, wantID uuid.UUID, cookies []*http.Cookie) {
	t.Helper()
	if got.User.Username != wantUser || got.User.ID != wantID || got.User.Role != oas.UserRoleAdmin {
		t.Fatalf("Login() user = %+v, want %s %s", got.User, wantID, wantUser)
	}
	if got.CsrfToken == "" {
		t.Fatal("Login() CsrfToken is empty")
	}
	wantExpiry := f.clk.Now().Add(f.h.cfg.Session.TTL)
	if !got.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("Login() ExpiresAt = %v, want %v", got.ExpiresAt, wantExpiry)
	}
	assertSessionUserAndCsrf(t, f, cookies, domain.User{ID: wantID, Username: wantUser, Role: domain.RoleAdmin})
}

func TestLogout(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		noHTTP   bool
		canceled bool
		wantErr  error
	}{
		{"positive logout with session", false, false, nil},
		{"negative logout no http context", true, false, domain.ErrUnauthorized},
		{"negative logout store error", false, true, context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			if tc.noHTTP {
				if err := f.h.Logout(t.Context()); !errors.Is(err, tc.wantErr) {
					t.Fatalf("Logout() err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			setupRec := httptest.NewRecorder()
			setupReq := httptest.NewRequest(http.MethodPost, "/auth/setup", nil)
			setupCtx := withHTTPTest(t.Context(), setupRec, setupReq)
			if _, err := f.h.CompleteSetup(setupCtx, &oas.SetupRequest{Username: "first-admin", Password: "correct-horse-battery-staple"}); err != nil {
				t.Fatalf("CompleteSetup() = %v", err)
			}
			cookies := setupRec.Result().Cookies()
			ctx := t.Context()
			if tc.canceled {
				cancCtx, cancel := context.WithCancel(t.Context())
				cancel()
				ctx = cancCtx
			}
			logoutReq := httptest.NewRequest(http.MethodPost, "/auth/logout", nil).WithContext(ctx)
			for _, c := range cookies {
				logoutReq.AddCookie(c)
			}
			logoutCtx := withHTTPTest(ctx, httptest.NewRecorder(), logoutReq)
			err := f.h.Logout(logoutCtx)
			if tc.wantErr != nil {
				if err == nil || !errors.Is(err, tc.wantErr) {
					t.Fatalf("Logout() err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Logout() = %v", err)
			}
			assertSessionDestroyed(t, f, cookies)
		})
	}
}

func TestGetSession(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		noUser  bool
		wantErr error
	}{
		{"positive get session authenticated", false, nil},
		{"negative get session no user", true, domain.ErrUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			if tc.noUser {
				if _, err := f.h.GetSession(t.Context()); !errors.Is(err, tc.wantErr) {
					t.Fatalf("GetSession() err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			req := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
			ctx := withHTTPTest(withUser(t.Context(), f.admin), httptest.NewRecorder(), req)
			got, err := f.h.GetSession(ctx)
			if err != nil {
				t.Fatalf("GetSession() = %v", err)
			}
			if got.Response.User.Username != f.admin.Username || got.Response.User.ID != f.admin.ID || got.Response.User.Role != oas.UserRole(f.admin.Role) {
				t.Fatalf("GetSession() user = %+v, want %+v", got.Response.User, f.admin)
			}
			csrfHeader, ok := got.XCSRFToken.Get()
			if !ok || csrfHeader == "" || csrfHeader != got.Response.CsrfToken {
				t.Fatalf("GetSession() XCSRFToken = %q (ok=%v), want non-empty %q", csrfHeader, ok, got.Response.CsrfToken)
			}
			wantExpiry := f.clk.Now().Add(f.h.cfg.Session.TTL)
			if !got.Response.ExpiresAt.Equal(wantExpiry) {
				t.Fatalf("GetSession() ExpiresAt = %v, want %v", got.Response.ExpiresAt, wantExpiry)
			}
		})
	}
}

func TestInstanceReadAndProbe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		seed       bool
		probeErr   error
		useUnknown bool
		wantErr    error
	}{
		{"positive list instances", true, nil, false, nil},
		{"boundary list instances empty", false, nil, false, nil},
		{"positive get instance", true, nil, false, nil},
		{"negative get instance unknown", false, nil, true, domain.ErrNotFound},
		{"positive test instance", true, nil, false, nil},
		{"negative test instance unknown", false, nil, true, domain.ErrNotFound},
		{"boundary test instance probe failure", true, domain.ErrUnreachable, false, domain.ErrUnreachable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			instID := uuid.New()
			if tc.seed {
				instID = seedTestInstance(t, f)
			}
			if tc.probeErr != nil {
				f.probe.err = tc.probeErr
			}
			testInstanceReadCase(t, f, tc.name, instID, tc.wantErr)
		})
	}
}

func testInstanceReadCase(t *testing.T, f *integrationFixture, name string, id uuid.UUID, wantErr error) {
	t.Helper()
	ctx := t.Context()
	switch {
	case strings.Contains(name, "list instances"):
		list, err := f.h.ListInstances(ctx)
		if err != nil {
			t.Fatalf("ListInstances() = %v", err)
		}
		wantLen := 0
		if strings.HasPrefix(name, "positive") {
			wantLen = 1
		}
		if len(list) != wantLen {
			t.Fatalf("ListInstances() len = %d, want %d", len(list), wantLen)
		}
	case strings.Contains(name, "get instance"):
		got, err := f.h.GetInstance(ctx, oas.GetInstanceParams{InstanceId: id})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("GetInstance() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil || got.ID != id {
			t.Fatalf("GetInstance() = %+v, %v; want ID %s", got, err, id)
		}
	case strings.Contains(name, "test instance"):
		got, err := f.h.TestInstance(ctx, oas.TestInstanceParams{InstanceId: id})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("TestInstance() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil || got.AppName != "Sonarr" {
			t.Fatalf("TestInstance() = %+v, %v; want Sonarr", got, err)
		}
	}
}

func TestInstanceCreate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		user    *domain.User
		in      oas.InstanceInput
		wantErr error
	}{
		{"positive admin creates instance", &domain.User{Role: domain.RoleAdmin}, validInstanceInput(), nil},
		{"negative no user", nil, validInstanceInput(), domain.ErrUnauthorized},
		{"negative viewer forbidden", &domain.User{Role: domain.RoleViewer}, validInstanceInput(), domain.ErrForbidden},
		{"boundary empty name", &domain.User{Role: domain.RoleAdmin}, oas.InstanceInput{Kind: oas.AppKindSonarr, BaseURL: "http://arr.invalid", APIKey: "key"}, domain.ErrInvalid},
		{"boundary empty base url", &domain.User{Role: domain.RoleAdmin}, oas.InstanceInput{Kind: oas.AppKindSonarr, Name: "tv", APIKey: "key"}, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			got, err := f.h.CreateInstance(ctx, &tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("CreateInstance() err = %v, want %v", err, tc.wantErr)
				}
				list, listErr := f.h.ListInstances(t.Context())
				if listErr != nil || len(list) != 0 {
					t.Fatalf("after refused create: len = %d, %v", len(list), listErr)
				}
				return
			}
			if err != nil || got.Name != tc.in.Name {
				t.Fatalf("CreateInstance() = %+v, %v; want name %s", got, err, tc.in.Name)
			}
			list, listErr := f.h.ListInstances(t.Context())
			if listErr != nil || len(list) != 1 {
				t.Fatalf("ListInstances() after create = %d, want 1", len(list))
			}
		})
	}
}

func TestInstanceUpdateAndDelete(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		isDelete   bool
		user       *domain.User
		useUnknown bool
		in         oas.InstanceUpdate
		wantErr    error
	}{
		{"positive update instance", false, &domain.User{Role: domain.RoleAdmin}, false, oas.InstanceUpdate{Kind: oas.AppKindSonarr, Name: "renamed", BaseURL: "http://sonarr.invalid:8989"}, nil},
		{"negative update no user", false, nil, false, oas.InstanceUpdate{Kind: oas.AppKindSonarr, Name: "renamed", BaseURL: "http://sonarr.invalid:8989"}, domain.ErrUnauthorized},
		{"negative update viewer", false, &domain.User{Role: domain.RoleViewer}, false, oas.InstanceUpdate{Kind: oas.AppKindSonarr, Name: "renamed", BaseURL: "http://sonarr.invalid:8989"}, domain.ErrForbidden},
		{"negative update unknown", false, &domain.User{Role: domain.RoleAdmin}, true, oas.InstanceUpdate{Kind: oas.AppKindSonarr, Name: "renamed", BaseURL: "http://sonarr.invalid:8989"}, domain.ErrNotFound},
		{"boundary update empty name", false, &domain.User{Role: domain.RoleAdmin}, false, oas.InstanceUpdate{Kind: oas.AppKindSonarr, BaseURL: "http://sonarr.invalid:8989"}, domain.ErrInvalid},
		{"positive delete instance", true, &domain.User{Role: domain.RoleAdmin}, false, oas.InstanceUpdate{}, nil},
		{"negative delete no user", true, nil, false, oas.InstanceUpdate{}, domain.ErrUnauthorized},
		{"negative delete viewer", true, &domain.User{Role: domain.RoleViewer}, false, oas.InstanceUpdate{}, domain.ErrForbidden},
		{"negative delete unknown", true, &domain.User{Role: domain.RoleAdmin}, true, oas.InstanceUpdate{}, domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			instID := seedTestInstance(t, f)
			targetID := instID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			testInstanceUpdateDeleteCase(t, f, tc.isDelete, targetID, instID, tc.in, ctx, tc.wantErr)
		})
	}
}

func testInstanceUpdateDeleteCase(t *testing.T, f *integrationFixture, isDel bool, targetID, originalID uuid.UUID, in oas.InstanceUpdate, ctx context.Context, wantErr error) {
	t.Helper()
	if isDel {
		err := f.h.DeleteInstance(ctx, oas.DeleteInstanceParams{InstanceId: targetID})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("DeleteInstance() err = %v, want %v", err, wantErr)
			}
			if _, getErr := f.h.GetInstance(ctx, oas.GetInstanceParams{InstanceId: originalID}); getErr != nil {
				t.Fatalf("original instance lost after refused delete: %v", getErr)
			}
			return
		}
		if err != nil {
			t.Fatalf("DeleteInstance() = %v", err)
		}
		if _, getErr := f.h.GetInstance(ctx, oas.GetInstanceParams{InstanceId: targetID}); !errors.Is(getErr, domain.ErrNotFound) {
			t.Fatalf("GetInstance() after delete err = %v, want ErrNotFound", getErr)
		}
		return
	}
	got, err := f.h.UpdateInstance(ctx, &in, oas.UpdateInstanceParams{InstanceId: targetID})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("UpdateInstance() err = %v, want %v", err, wantErr)
		}
		readBack, getErr := f.h.GetInstance(ctx, oas.GetInstanceParams{InstanceId: originalID})
		if getErr != nil || readBack.Name == in.Name {
			t.Fatalf("instance modified after refused update: %+v, %v", readBack, getErr)
		}
		return
	}
	if err != nil || got.Name != in.Name {
		t.Fatalf("UpdateInstance() = %+v, %v; want name %s", got, err, in.Name)
	}
}

func TestPolicyOperations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		isUpdate   bool
		user       *domain.User
		useUnknown bool
		cap        int
		page       int
		wantErr    error
	}{
		{"positive get policy", false, nil, false, 0, 0, nil},
		{"negative get policy unknown", false, nil, true, 0, 0, domain.ErrNotFound},
		{"positive update policy", true, &domain.User{Role: domain.RoleAdmin}, false, 350, 20, nil},
		{"negative update no user", true, nil, false, 350, 20, domain.ErrUnauthorized},
		{"negative update viewer", true, &domain.User{Role: domain.RoleViewer}, false, 350, 20, domain.ErrForbidden},
		{"negative update unknown", true, &domain.User{Role: domain.RoleAdmin}, true, 350, 20, domain.ErrNotFound},
		{"boundary update cap 501", true, &domain.User{Role: domain.RoleAdmin}, false, 501, 20, domain.ErrInvalid},
		{"boundary update page size 9", true, &domain.User{Role: domain.RoleAdmin}, false, 200, 9, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			instID := seedTestInstance(t, f)
			targetID := instID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			testPolicyCase(t, f, tc.isUpdate, targetID, instID, tc.cap, tc.page, ctx, tc.wantErr)
		})
	}
}

func testPolicyCase(t *testing.T, f *integrationFixture, isUpd bool, targetID, originalID uuid.UUID, capVal, pageVal int, ctx context.Context, wantErr error) {
	t.Helper()
	if !isUpd {
		got, err := f.h.GetPolicy(ctx, oas.GetPolicyParams{InstanceId: targetID})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("GetPolicy() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil || got.HourlyCap != 20 {
			t.Fatalf("GetPolicy() = %+v, %v; want cap 20", got, err)
		}
		return
	}
	policy, err := f.h.GetPolicy(ctx, oas.GetPolicyParams{InstanceId: originalID})
	if err != nil {
		t.Fatalf("GetPolicy() = %v", err)
	}
	policy.HourlyCap = capVal
	policy.PageSize = pageVal
	got, err := f.h.UpdatePolicy(ctx, policy, oas.UpdatePolicyParams{InstanceId: targetID})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("UpdatePolicy() err = %v, want %v", err, wantErr)
		}
		readBack, getErr := f.h.GetPolicy(ctx, oas.GetPolicyParams{InstanceId: originalID})
		if getErr != nil || readBack.HourlyCap == capVal {
			t.Fatalf("policy modified after refused update: %+v, %v", readBack, getErr)
		}
		return
	}
	if err != nil || got.HourlyCap != capVal {
		t.Fatalf("UpdatePolicy() = %+v, %v; want cap %d", got, err, capVal)
	}
}

func TestScheduleReadAndCreate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		seed    bool
		user    *domain.User
		in      oas.ScheduleInput
		wantErr error
	}{
		{"boundary list schedules empty", false, nil, oas.ScheduleInput{}, nil},
		{"positive list schedules", true, nil, oas.ScheduleInput{}, nil},
		{"positive create schedule", false, &domain.User{Role: domain.RoleAdmin}, validScheduleInput(), nil},
		{"negative create no user", false, nil, validScheduleInput(), domain.ErrUnauthorized},
		{"negative create viewer", false, &domain.User{Role: domain.RoleViewer}, validScheduleInput(), domain.ErrForbidden},
		{"boundary create empty name", false, &domain.User{Role: domain.RoleAdmin}, oas.ScheduleInput{Days: []int{0}, Start: "00:00", End: "01:00", Tz: "UTC", Action: oas.ScheduleInputActionPause, Enabled: true}, domain.ErrInvalid},
		{"boundary create invalid day", false, &domain.User{Role: domain.RoleAdmin}, oas.ScheduleInput{Name: "bad", Days: []int{7}, Start: "00:00", End: "01:00", Tz: "UTC", Action: oas.ScheduleInputActionPause, Enabled: true}, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			if strings.Contains(tc.name, "list schedules") {
				testScheduleListCase(t, f, tc.seed, ctx)
				return
			}
			testScheduleCreateCase(t, f, tc.in, ctx, tc.wantErr)
		})
	}
}

func testScheduleListCase(t *testing.T, f *integrationFixture, seed bool, ctx context.Context) {
	t.Helper()
	var createdID uuid.UUID
	if seed {
		adminCtx := withUser(ctx, f.admin)
		in := validScheduleInput()
		created, err := f.h.CreateSchedule(adminCtx, &in)
		if err != nil {
			t.Fatalf("seed schedule: %v", err)
		}
		createdID = created.ID
	}
	list, err := f.h.ListSchedules(ctx)
	if err != nil {
		t.Fatalf("ListSchedules() err = %v", err)
	}
	wantLen := 0
	if seed {
		wantLen = 1
	}
	if len(list) != wantLen {
		t.Fatalf("ListSchedules() len = %d, want %d", len(list), wantLen)
	}
	if seed {
		assertScheduleRoundTrip(t, list[0], createdID)
	}
}

func assertScheduleRoundTrip(t *testing.T, s oas.Schedule, wantID uuid.UUID) {
	t.Helper()
	if s.ID != wantID || s.Name != "night-pause" || len(s.Days) != 7 || s.Start != "22:00" || s.End != "06:00" || s.Tz != "Europe/Berlin" || !s.Enabled {
		t.Fatalf("ListSchedules() round-trip fields mismatch: %+v", s)
	}
	if s.Action != oas.ScheduleActionCapOverride {
		t.Fatalf("ListSchedules() Action = %v, want cap_override", s.Action)
	}
	capVal, ok := s.CapValue.Get()
	if !ok || capVal != 15 {
		t.Fatalf("ListSchedules() CapValue = %v (ok=%v), want 15", capVal, ok)
	}
}

func testScheduleCreateCase(t *testing.T, f *integrationFixture, in oas.ScheduleInput, ctx context.Context, wantErr error) {
	t.Helper()
	got, err := f.h.CreateSchedule(ctx, &in)
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("CreateSchedule() err = %v, want %v", err, wantErr)
		}
		list, listErr := f.h.ListSchedules(ctx)
		if listErr != nil || len(list) != 0 {
			t.Fatalf("after refused create: len = %d, %v", len(list), listErr)
		}
		return
	}
	if err != nil || got.Name != in.Name {
		t.Fatalf("CreateSchedule() = %+v, %v; want name %s", got, err, in.Name)
	}
	if inCap, ok := in.CapValue.Get(); ok {
		gotCap, hasCap := got.CapValue.Get()
		if !hasCap || gotCap != inCap {
			t.Fatalf("CreateSchedule() CapValue = %v, want %d", gotCap, inCap)
		}
	}
}

func TestScheduleUpdateAndDelete(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		isDelete   bool
		user       *domain.User
		useUnknown bool
		in         oas.ScheduleInput
		wantErr    error
	}{
		{"positive update schedule", false, &domain.User{Role: domain.RoleAdmin}, false, oas.ScheduleInput{Name: "day-pause", Days: []int{1}, Start: "08:00", End: "18:00", Tz: "UTC", Action: oas.ScheduleInputActionPause, Enabled: true}, nil},
		{"negative update no user", false, nil, false, oas.ScheduleInput{Name: "day-pause", Days: []int{1}, Start: "08:00", End: "18:00", Tz: "UTC", Action: oas.ScheduleInputActionPause, Enabled: true}, domain.ErrUnauthorized},
		{"negative update viewer", false, &domain.User{Role: domain.RoleViewer}, false, oas.ScheduleInput{Name: "day-pause", Days: []int{1}, Start: "08:00", End: "18:00", Tz: "UTC", Action: oas.ScheduleInputActionPause, Enabled: true}, domain.ErrForbidden},
		{"negative update unknown", false, &domain.User{Role: domain.RoleAdmin}, true, oas.ScheduleInput{Name: "day-pause", Days: []int{1}, Start: "08:00", End: "18:00", Tz: "UTC", Action: oas.ScheduleInputActionPause, Enabled: true}, domain.ErrNotFound},
		{"boundary update bad time", false, &domain.User{Role: domain.RoleAdmin}, false, oas.ScheduleInput{Name: "bad", Days: []int{1}, Start: "25:00", End: "18:00", Tz: "UTC", Action: oas.ScheduleInputActionPause, Enabled: true}, domain.ErrInvalid},
		{"positive delete schedule", true, &domain.User{Role: domain.RoleAdmin}, false, oas.ScheduleInput{}, nil},
		{"negative delete no user", true, nil, false, oas.ScheduleInput{}, domain.ErrUnauthorized},
		{"negative delete viewer", true, &domain.User{Role: domain.RoleViewer}, false, oas.ScheduleInput{}, domain.ErrForbidden},
		{"negative delete unknown", true, &domain.User{Role: domain.RoleAdmin}, true, oas.ScheduleInput{}, domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			adminCtx := withUser(t.Context(), f.admin)
			seedIn := validScheduleInput()
			created, err := f.h.CreateSchedule(adminCtx, &seedIn)
			if err != nil {
				t.Fatalf("CreateSchedule seed = %v", err)
			}
			targetID := created.ID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			testScheduleUpdateDeleteCase(t, f, tc.isDelete, targetID, tc.in, ctx, tc.wantErr)
		})
	}
}

func testScheduleUpdateDeleteCase(t *testing.T, f *integrationFixture, isDel bool, targetID uuid.UUID, in oas.ScheduleInput, ctx context.Context, wantErr error) {
	t.Helper()
	if isDel {
		err := f.h.DeleteSchedule(ctx, oas.DeleteScheduleParams{ScheduleId: targetID})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("DeleteSchedule() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil {
			t.Fatalf("DeleteSchedule() = %v", err)
		}
		list, listErr := f.h.ListSchedules(ctx)
		if listErr != nil || len(list) != 0 {
			t.Fatalf("ListSchedules() after delete len = %d, %v", len(list), listErr)
		}
		return
	}
	got, err := f.h.UpdateSchedule(ctx, &in, oas.UpdateScheduleParams{ScheduleId: targetID})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("UpdateSchedule() err = %v, want %v", err, wantErr)
		}
		list, _ := f.h.ListSchedules(ctx)
		if len(list) > 0 && list[0].Name == in.Name {
			t.Fatalf("schedule modified after refused update: %+v", list[0])
		}
		return
	}
	if err != nil || got.Name != in.Name {
		t.Fatalf("UpdateSchedule() = %+v, %v; want %s", got, err, in.Name)
	}
}

func TestRunOperations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		op         string
		user       *domain.User
		useUnknown bool
		kind       string
		wantErr    error
	}{
		{"positive trigger run created", "trigger", &domain.User{Role: domain.RoleAdmin}, false, "missing", nil},
		{"boundary trigger run ok active", "trigger-again", &domain.User{Role: domain.RoleAdmin}, false, "missing", nil},
		{"negative trigger run no user", "trigger", nil, false, "missing", domain.ErrUnauthorized},
		{"negative trigger run viewer", "trigger", &domain.User{Role: domain.RoleViewer}, false, "missing", domain.ErrForbidden},
		{"negative trigger run unknown instance", "trigger", &domain.User{Role: domain.RoleAdmin}, true, "missing", domain.ErrNotFound},
		{"boundary trigger run invalid kind", "trigger", &domain.User{Role: domain.RoleAdmin}, false, "invalid-kind", domain.ErrInvalid},
		{"positive list runs", "list", nil, false, "", nil},
		{"boundary list runs empty", "list-empty", nil, false, "", nil},
		{"positive cancel run", "cancel", &domain.User{Role: domain.RoleAdmin}, false, "", nil},
		{"negative cancel run no user", "cancel", nil, false, "", domain.ErrUnauthorized},
		{"negative cancel run viewer", "cancel", &domain.User{Role: domain.RoleViewer}, false, "", domain.ErrForbidden},
		{"negative cancel run unknown", "cancel", &domain.User{Role: domain.RoleAdmin}, true, "", domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			instID := seedTestInstance(t, f)
			targetID := instID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			testRunCase(t, f, tc.op, targetID, instID, tc.kind, ctx, tc.wantErr)
		})
	}
}

func testRunCase(t *testing.T, f *integrationFixture, op string, targetID, instID uuid.UUID, kind string, ctx context.Context, wantErr error) {
	t.Helper()
	switch op {
	case "trigger", "trigger-again":
		req := &oas.RunTrigger{Kind: oas.SnatchKind(kind)}
		params := oas.TriggerRunParams{InstanceId: targetID}
		if op == "trigger-again" {
			adminCtx := withUser(ctx, f.admin)
			if _, err := f.h.TriggerRun(adminCtx, req, params); err != nil {
				t.Fatalf("first trigger: %v", err)
			}
		}
		got, err := f.h.TriggerRun(ctx, req, params)
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("TriggerRun() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil {
			t.Fatalf("TriggerRun() err = %v", err)
		}
		if op == "trigger" {
			created, ok := got.(*oas.TriggerRunCreated)
			if !ok {
				t.Fatalf("TriggerRun() got %T, want *TriggerRunCreated", got)
			}
			assertTriggerQuickie(t, f, ctx, targetID, created.ID, kind)
		} else {
			if _, ok := got.(*oas.TriggerRunOK); !ok {
				t.Fatalf("TriggerRun() got %T, want *TriggerRunOK", got)
			}
		}
	case "list", "list-empty":
		if op == "list" {
			if _, _, err := f.h.runs.Enqueue(ctx, instID, domain.SnatchMissing); err != nil {
				t.Fatalf("seed run: %v", err)
			}
		}
		list, err := f.h.ListRuns(ctx, oas.ListRunsParams{InstanceID: oas.NewOptUUID(instID)})
		wantLen := 0
		if op == "list" {
			wantLen = 1
		}
		if err != nil || len(list) != wantLen {
			t.Fatalf("ListRuns() len = %d, %v; want %d", len(list), err, wantLen)
		}
	case "cancel":
		runID := targetID
		if targetID == instID {
			run, _, err := f.h.runs.Enqueue(ctx, instID, domain.SnatchMissing)
			if err != nil {
				t.Fatalf("seed run: %v", err)
			}
			runID = run.ID
		}
		err := f.h.CancelRun(ctx, oas.CancelRunParams{RunId: runID})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("CancelRun() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil {
			t.Fatalf("CancelRun() err = %v", err)
		}
	}
}

func assertTriggerQuickie(t *testing.T, f *integrationFixture, ctx context.Context, instID, runID uuid.UUID, kind string) {
	t.Helper()
	eventsList, err := f.h.ListEvents(ctx, oas.ListEventsParams{InstanceID: oas.NewOptUUID(instID)})
	if err != nil {
		t.Fatalf("ListEvents() = %v", err)
	}
	if len(eventsList) != 1 {
		t.Fatalf("ListEvents() len = %d, want 1 quickie event", len(eventsList))
	}
	if eventsList[0].Type != "run_queued" {
		t.Fatalf("event Type = %s, want run_queued", eventsList[0].Type)
	}
	gotRunID, hasRunID := eventsList[0].RunID.Get()
	if !hasRunID || gotRunID != runID {
		t.Fatalf("event RunID = %v, want %v", gotRunID, runID)
	}
	if eventsList[0].InstanceID != instID {
		t.Fatalf("event InstanceID = %v, want %v", eventsList[0].InstanceID, instID)
	}
	title, ok := eventsList[0].Title.Get()
	wantTitle := fmt.Sprintf("Quickie: %s snatch queued by hand", kind)
	if !ok || title != wantTitle {
		t.Fatalf("event Title = %q, want %q", title, wantTitle)
	}
}

func TestListRunsPaginationAndFilter(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := t.Context()
	inst1 := seedTestInstance(t, f)
	inst2 := seedTestInstance(t, f)
	if _, _, err := f.h.runs.Enqueue(ctx, inst1, domain.SnatchMissing); err != nil {
		t.Fatalf("enqueue inst1 missing: %v", err)
	}
	if _, _, err := f.h.runs.Enqueue(ctx, inst1, domain.SnatchUpgrade); err != nil {
		t.Fatalf("enqueue inst1 cutoff: %v", err)
	}
	if _, _, err := f.h.runs.Enqueue(ctx, inst2, domain.SnatchMissing); err != nil {
		t.Fatalf("enqueue inst2 missing: %v", err)
	}
	allRuns, err := f.h.ListRuns(ctx, oas.ListRunsParams{})
	if err != nil {
		t.Fatalf("ListRuns() = %v", err)
	}
	if len(allRuns) != 3 {
		t.Fatalf("ListRuns() default limit len = %d, want 3", len(allRuns))
	}
	inst1Runs, err := f.h.ListRuns(ctx, oas.ListRunsParams{InstanceID: oas.NewOptUUID(inst1)})
	if err != nil {
		t.Fatalf("ListRuns() inst1 = %v", err)
	}
	if len(inst1Runs) != 2 {
		t.Fatalf("ListRuns() inst1 len = %d, want 2", len(inst1Runs))
	}
	for _, r := range inst1Runs {
		if r.InstanceID != inst1 {
			t.Fatalf("ListRuns() run instance = %v, want %v", r.InstanceID, inst1)
		}
	}
}

func TestEventOperations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		isDelete   bool
		seed       bool
		user       *domain.User
		useUnknown bool
		wantErr    error
	}{
		{"positive list events", false, true, nil, false, nil},
		{"boundary list events empty", false, false, nil, false, nil},
		{"positive delete events", true, true, &domain.User{Role: domain.RoleAdmin}, false, nil},
		{"negative delete events no user", true, true, nil, false, domain.ErrUnauthorized},
		{"negative delete events viewer", true, true, &domain.User{Role: domain.RoleViewer}, false, domain.ErrForbidden},
		{"negative delete events unknown", true, true, &domain.User{Role: domain.RoleAdmin}, true, domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			instID := seedTestInstance(t, f)
			targetID := instID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			if tc.seed {
				if err := f.h.rec.Record(t.Context(), domain.Event{InstanceID: instID, Level: "info", Type: "test_event", Title: "seeded event"}); err != nil {
					t.Fatalf("Record() = %v", err)
				}
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			testEventCase(t, f, tc.isDelete, targetID, instID, tc.seed, ctx, tc.wantErr)
		})
	}
}

func testEventCase(t *testing.T, f *integrationFixture, isDel bool, targetID, instID uuid.UUID, seeded bool, ctx context.Context, wantErr error) {
	t.Helper()
	if !isDel {
		list, err := f.h.ListEvents(ctx, oas.ListEventsParams{InstanceID: oas.NewOptUUID(instID)})
		wantLen := 0
		if seeded {
			wantLen = 1
		}
		if err != nil || len(list) != wantLen {
			t.Fatalf("ListEvents() len = %d, %v; want %d", len(list), err, wantLen)
		}
		return
	}
	err := f.h.DeleteInstanceEvents(ctx, oas.DeleteInstanceEventsParams{InstanceId: targetID})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("DeleteInstanceEvents() err = %v, want %v", err, wantErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("DeleteInstanceEvents() err = %v", err)
	}
	list, listErr := f.h.ListEvents(ctx, oas.ListEventsParams{InstanceID: oas.NewOptUUID(instID)})
	if listErr != nil || len(list) != 0 {
		t.Fatalf("ListEvents() after delete len = %d, %v; want 0", len(list), listErr)
	}
}

func TestListEventsFiltering(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := t.Context()
	inst1 := seedTestInstance(t, f)
	inst2 := seedTestInstance(t, f)
	seedEventRecord(t, f, inst1, "search_dispatched", "ev1")
	seedEventRecord(t, f, inst1, "snatch_completed", "ev2")
	seedEventRecord(t, f, inst2, "search_dispatched", "ev3")
	seedEventRecord(t, f, inst2, "snatch_completed", "ev4")

	allEvents, err := f.h.ListEvents(ctx, oas.ListEventsParams{})
	if err != nil || len(allEvents) != 4 {
		t.Fatalf("ListEvents() default page len = %d, %v; want 4", len(allEvents), err)
	}
	typeEvents, err := f.h.ListEvents(ctx, oas.ListEventsParams{Type: oas.NewOptString("search_dispatched")})
	if err != nil || len(typeEvents) != 2 {
		t.Fatalf("ListEvents() type filter len = %d, %v; want 2", len(typeEvents), err)
	}
	for _, e := range typeEvents {
		if e.Type != "search_dispatched" {
			t.Fatalf("event Type = %s, want search_dispatched", e.Type)
		}
	}
	instEvents, err := f.h.ListEvents(ctx, oas.ListEventsParams{InstanceID: oas.NewOptUUID(inst1)})
	if err != nil || len(instEvents) != 2 {
		t.Fatalf("ListEvents() inst filter len = %d, %v; want 2", len(instEvents), err)
	}
	for _, e := range instEvents {
		if e.InstanceID != inst1 {
			t.Fatalf("event InstanceID = %v, want %v", e.InstanceID, inst1)
		}
	}
	beforeID := allEvents[1].ID
	beforeEvents, err := f.h.ListEvents(ctx, oas.ListEventsParams{BeforeID: oas.NewOptInt64(beforeID)})
	if err != nil || len(beforeEvents) != 2 {
		t.Fatalf("ListEvents() before filter len = %d, %v; want 2", len(beforeEvents), err)
	}
	for _, e := range beforeEvents {
		if e.ID >= beforeID {
			t.Fatalf("event ID = %d, want < %d", e.ID, beforeID)
		}
	}
}

func seedEventRecord(t *testing.T, f *integrationFixture, instID uuid.UUID, eventType, title string) {
	t.Helper()
	if err := f.h.rec.Record(t.Context(), domain.Event{InstanceID: instID, Level: "info", Type: eventType, Title: title}); err != nil {
		t.Fatalf("Record() = %v", err)
	}
}

func TestSettingsOperations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		isUpdate bool
		user     *domain.User
		days     int
		ua       string
		cap      int
		wantErr  error
	}{
		{"positive get settings", false, nil, 90, "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)", 0, nil},
		{"positive update settings", true, &domain.User{Role: domain.RoleAdmin}, 45, "CustomUA", 150, nil},
		{"negative update settings no user", true, nil, 45, "CustomUA", 150, domain.ErrUnauthorized},
		{"negative update settings viewer", true, &domain.User{Role: domain.RoleViewer}, 45, "CustomUA", 150, domain.ErrForbidden},
		{"boundary update settings cap 5001", true, &domain.User{Role: domain.RoleAdmin}, 45, "CustomUA", 5001, domain.ErrInvalid},
		{"boundary update settings days 0", true, &domain.User{Role: domain.RoleAdmin}, 0, "CustomUA", 150, domain.ErrInvalid},
		{"boundary update settings empty ua", true, &domain.User{Role: domain.RoleAdmin}, 45, "", 150, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			if !tc.isUpdate {
				got, err := f.h.GetSettings(ctx)
				if err != nil || got.HistoryRetentionDays != 90 {
					t.Fatalf("GetSettings() = %+v, %v; want 90 days", got, err)
				}
				return
			}
			req := &oas.Settings{HistoryRetentionDays: tc.days, UserAgent: tc.ua, GlobalHourlyCap: tc.cap}
			got, err := f.h.UpdateSettings(ctx, req)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("UpdateSettings() err = %v, want %v", err, tc.wantErr)
				}
				cur, getErr := f.h.GetSettings(t.Context())
				if getErr != nil || cur.HistoryRetentionDays == tc.days {
					t.Fatalf("settings modified after refused update: %+v", cur)
				}
				return
			}
			if err != nil || got.HistoryRetentionDays != tc.days || got.GlobalHourlyCap != tc.cap {
				t.Fatalf("UpdateSettings() = %+v, %v", got, err)
			}
		})
	}
}

func TestStaminaAndStateOperations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		isReset   bool
		seed      bool
		globalCap int
		user      *domain.User
		wantErr   error
	}{
		{"boundary hourly caps empty", false, false, 0, nil, nil},
		{"positive hourly caps with instance", false, true, 0, nil, nil},
		{"positive hourly caps with global", false, true, 120, nil, nil},
		{"positive reset state all", true, false, 0, &domain.User{Role: domain.RoleAdmin}, nil},
		{"positive reset state instance", true, true, 0, &domain.User{Role: domain.RoleAdmin}, nil},
		{"negative reset state no user", true, false, 0, nil, domain.ErrUnauthorized},
		{"negative reset state viewer", true, false, 0, &domain.User{Role: domain.RoleViewer}, domain.ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			instID := uuid.Nil
			if tc.seed {
				instID = seedTestInstance(t, f)
			}
			if tc.globalCap > 0 {
				adminCtx := withUser(t.Context(), f.admin)
				if _, err := f.h.UpdateSettings(adminCtx, &oas.Settings{HistoryRetentionDays: 30, UserAgent: "SnatchArr", GlobalHourlyCap: tc.globalCap}); err != nil {
					t.Fatalf("UpdateSettings() = %v", err)
				}
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			testStaminaStateCase(t, f, tc.isReset, instID, tc.seed, tc.globalCap, ctx, tc.wantErr)
		})
	}
}

func testStaminaStateCase(t *testing.T, f *integrationFixture, isReset bool, instID uuid.UUID, seeded bool, globalCap int, ctx context.Context, wantErr error) {
	t.Helper()
	if !isReset {
		caps, err := f.h.GetHourlyCaps(ctx)
		if err != nil {
			t.Fatalf("GetHourlyCaps() err = %v", err)
		}
		wantLen := 0
		if seeded {
			wantLen++
		}
		if globalCap > 0 {
			wantLen++
		}
		if len(caps) != wantLen {
			t.Fatalf("GetHourlyCaps() len = %d, want %d", len(caps), wantLen)
		}
		return
	}
	var resetReq oas.OptStateReset
	if instID != uuid.Nil {
		resetReq = oas.NewOptStateReset(oas.StateReset{InstanceID: oas.NewOptUUID(instID)})
	}
	got, err := f.h.ResetState(ctx, resetReq)
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("ResetState() err = %v, want %v", err, wantErr)
		}
		return
	}
	if err != nil || got.Forgotten < 0 {
		t.Fatalf("ResetState() = %+v, %v", got, err)
	}
}

func TestHourlyCapsDetails(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := t.Context()
	instID := seedTestInstance(t, f)
	pol, err := f.h.policies.Get(ctx, instID)
	if err != nil {
		t.Fatalf("policies.Get() = %v", err)
	}
	pol.HourlyCap = 40
	pol.PageSize = 20
	if _, updateErr := f.h.policies.Update(ctx, pol); updateErr != nil {
		t.Fatalf("policies.Update() = %v", updateErr)
	}
	grant, err := f.h.budget.Acquire(ctx, instID, 40, 0, 15)
	if err != nil || grant.Granted != 15 {
		t.Fatalf("budget.Acquire() = %+v, %v; want 15 granted", grant, err)
	}
	caps, err := f.h.GetHourlyCaps(ctx)
	if err != nil {
		t.Fatalf("GetHourlyCaps() = %v", err)
	}
	if len(caps) != 1 {
		t.Fatalf("GetHourlyCaps() len = %d, want 1", len(caps))
	}
	instUUID, ok := caps[0].InstanceID.Get()
	if !ok || instUUID != instID {
		t.Fatalf("GetHourlyCaps() instanceID = %v, want %v", instUUID, instID)
	}
	if caps[0].Cap != 40 {
		t.Fatalf("GetHourlyCaps() Cap = %d, want 40", caps[0].Cap)
	}
	if caps[0].Used != 15 {
		t.Fatalf("GetHourlyCaps() Used = %d, want 15", caps[0].Used)
	}
}

func TestResetStateDetails(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := t.Context()
	inst1 := seedTestInstance(t, f)
	inst2 := seedTestInstance(t, f)
	if err := f.h.memory.Mark(ctx, inst1, domain.SnatchMissing, "episode", []int64{101, 102}, time.Hour, 2*time.Hour); err != nil {
		t.Fatalf("Mark() inst1 = %v", err)
	}
	if err := f.h.memory.Mark(ctx, inst2, domain.SnatchMissing, "episode", []int64{201, 202, 203}, time.Hour, 2*time.Hour); err != nil {
		t.Fatalf("Mark() inst2 = %v", err)
	}
	adminCtx := withUser(ctx, f.admin)
	resetReq := oas.NewOptStateReset(oas.StateReset{InstanceID: oas.NewOptUUID(inst1)})
	got, err := f.h.ResetState(adminCtx, resetReq)
	if err != nil {
		t.Fatalf("ResetState() = %v", err)
	}
	if got.Forgotten != 2 {
		t.Fatalf("ResetState() Forgotten = %d, want 2", got.Forgotten)
	}
	unprocessed1, err := f.h.memory.FilterUnprocessed(ctx, inst1, domain.SnatchMissing, "episode", []int64{101, 102})
	if err != nil || len(unprocessed1) != 2 {
		t.Fatalf("FilterUnprocessed() inst1 len = %d, %v; want 2 forgotten", len(unprocessed1), err)
	}
	unprocessed2, err := f.h.memory.FilterUnprocessed(ctx, inst2, domain.SnatchMissing, "episode", []int64{201, 202, 203})
	if err != nil || len(unprocessed2) != 0 {
		t.Fatalf("FilterUnprocessed() inst2 len = %d, %v; want 0 forgotten (retained)", len(unprocessed2), err)
	}
}
