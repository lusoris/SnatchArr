// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/jellyfin"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

type tickerMockAuthenticator struct {
	userFunc func(ctx context.Context, id string) (jellyfin.Member, error)
	calls    atomic.Int32
}

func (m *tickerMockAuthenticator) SignIn(context.Context, string, string) (jellyfin.Member, error) {
	return jellyfin.Member{}, domain.ErrNotFound
}

func (m *tickerMockAuthenticator) QuickConnectEnabled(context.Context) (bool, error) {
	return false, domain.ErrNotFound
}

func (m *tickerMockAuthenticator) InitiateQuickConnect(context.Context) (string, string, error) {
	return "", "", domain.ErrNotFound
}

func (m *tickerMockAuthenticator) QuickConnectAuthorized(context.Context, string) (bool, error) {
	return false, domain.ErrNotFound
}

func (m *tickerMockAuthenticator) SignInQuickConnect(context.Context, string) (jellyfin.Member, error) {
	return jellyfin.Member{}, domain.ErrNotFound
}

func (m *tickerMockAuthenticator) User(ctx context.Context, id string) (jellyfin.Member, error) {
	m.calls.Add(1)
	if m.userFunc != nil {
		return m.userFunc(ctx, id)
	}
	return jellyfin.Member{}, domain.ErrNotFound
}

func TestJellyfinRevalidateTicker_Integration(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake()
	st := storetest.New(t)
	logger := slog.New(slog.DiscardHandler)
	m, _ := metricstest.New(t)

	svc, err := auth.NewService(st, crypto.NewPasswordHasher(2), clk, id.New(), logger)
	if err != nil {
		t.Fatalf("auth.NewService() = %v", err)
	}

	mockAuth := &tickerMockAuthenticator{
		userFunc: func(_ context.Context, subject string) (jellyfin.Member, error) {
			switch subject {
			case "subj-missing":
				return jellyfin.Member{}, domain.ErrNotFound
			case "subj-disabled":
				return jellyfin.Member{ID: subject, Name: "DisabledUser", Disabled: true}, nil
			case "subj-changed":
				// Member in Jellyfin is non-admin, but session holds admin
				return jellyfin.Member{ID: subject, Name: "ChangedUser", Admin: false}, nil
			case "subj-ok":
				// Member in Jellyfin matches session role requester
				return jellyfin.Member{ID: subject, Name: "OkUser", Admin: false}, nil
			default:
				return jellyfin.Member{}, domain.ErrNotFound
			}
		},
	}

	cfg := config.Options{}
	cfg.Jellyfin.Enabled = true
	cfg.Jellyfin.DefaultRole = domain.RoleRequester
	cfg.Jellyfin.Timeout = 2 * time.Second

	reval := jellyfin.NewRevalidator(mockAuth, clk, cfg.Jellyfin.DefaultRole, cfg.Jellyfin.Timeout, logger)
	ticker := auth.NewJellyfinRevalidateTicker(svc, reval, cfg, clk, m, logger)
	sessionStore := auth.NewPgSessionStore(st, clk)
	ctx := t.Context()

	// 1. Seed sessions:
	// a) missing Jellyfin member
	if err := sessionStore.Save(ctx, "sess-missing", map[string]any{
		auth.SessionSSOSourceKey:  "jellyfin",
		auth.SessionSSOSubjectKey: "subj-missing",
		auth.SessionSSORoleKey:    "requester",
	}, time.Hour); err != nil {
		t.Fatalf("save sess-missing: %v", err)
	}

	// b) disabled Jellyfin member
	if err := sessionStore.Save(ctx, "sess-disabled", map[string]any{
		auth.SessionSSOSourceKey:  "jellyfin",
		auth.SessionSSOSubjectKey: "subj-disabled",
		auth.SessionSSORoleKey:    "requester",
	}, time.Hour); err != nil {
		t.Fatalf("save sess-disabled: %v", err)
	}

	// c) changed role (session admin, but member requester)
	if err := sessionStore.Save(ctx, "sess-changed", map[string]any{
		auth.SessionSSOSourceKey:  "jellyfin",
		auth.SessionSSOSubjectKey: "subj-changed",
		auth.SessionSSORoleKey:    "admin",
	}, time.Hour); err != nil {
		t.Fatalf("save sess-changed: %v", err)
	}

	// d) valid matching member
	if err := sessionStore.Save(ctx, "sess-ok", map[string]any{
		auth.SessionSSOSourceKey:  "jellyfin",
		auth.SessionSSOSubjectKey: "subj-ok",
		auth.SessionSSORoleKey:    "requester",
	}, time.Hour); err != nil {
		t.Fatalf("save sess-ok: %v", err)
	}

	// e) non-jellyfin session (OIDC)
	if err := sessionStore.Save(ctx, "sess-oidc", map[string]any{
		auth.SessionSSOSourceKey:  "oidc",
		auth.SessionSSOSubjectKey: "subj-oidc",
		auth.SessionSSORoleKey:    "viewer",
	}, time.Hour); err != nil {
		t.Fatalf("save sess-oidc: %v", err)
	}

	// f) non-jellyfin session (plain user session)
	if err := sessionStore.Save(ctx, "sess-plain", map[string]any{
		auth.SessionUserKey: "00000000-0000-0000-0000-000000000001",
	}, time.Hour); err != nil {
		t.Fatalf("save sess-plain: %v", err)
	}

	// g) expired session: save with past timestamp
	if err := sessionStore.Save(ctx, "sess-expired", map[string]any{
		auth.SessionSSOSourceKey:  "jellyfin",
		auth.SessionSSOSubjectKey: "subj-missing",
		auth.SessionSSORoleKey:    "requester",
	}, -time.Minute); err != nil {
		t.Fatalf("save sess-expired: %v", err)
	}

	// 2. Run leader loop tick
	if err := ticker.Tick(ctx); err != nil {
		t.Fatalf("ticker.Tick() = %v", err)
	}

	// 3. Assertions:
	// a) missing revoked
	if _, err := sessionStore.Load(ctx, "sess-missing"); err == nil {
		t.Error("sess-missing should have been revoked")
	}

	// b) disabled revoked
	if _, err := sessionStore.Load(ctx, "sess-disabled"); err == nil {
		t.Error("sess-disabled should have been revoked")
	}

	// c) changed role revoked
	if _, err := sessionStore.Load(ctx, "sess-changed"); err == nil {
		t.Error("sess-changed should have been revoked")
	}

	// d) ok kept
	if _, err := sessionStore.Load(ctx, "sess-ok"); err != nil {
		t.Errorf("sess-ok should have been kept, got err: %v", err)
	}

	// e) non-jellyfin untouched
	if _, err := sessionStore.Load(ctx, "sess-oidc"); err != nil {
		t.Errorf("sess-oidc should be untouched, got err: %v", err)
	}
	if _, err := sessionStore.Load(ctx, "sess-plain"); err != nil {
		t.Errorf("sess-plain should be untouched, got err: %v", err)
	}

	// 4. Test quiescent when Jellyfin is disabled
	cfgDisabled := cfg
	cfgDisabled.Jellyfin.Enabled = false
	tickerDisabled := auth.NewJellyfinRevalidateTicker(svc, reval, cfgDisabled, clk, m, logger)

	callsBefore := mockAuth.calls.Load()
	if err := tickerDisabled.Tick(ctx); err != nil {
		t.Fatalf("tickerDisabled.Tick() = %v", err)
	}
	if callsAfter := mockAuth.calls.Load(); callsAfter != callsBefore {
		t.Fatalf("quiescent tick made calls: before=%d after=%d", callsBefore, callsAfter)
	}
}

func TestJellyfinRevalidateTicker_PlantedCheck_RevokeRoleMismatch(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake()
	st := storetest.New(t)
	logger := slog.New(slog.DiscardHandler)
	m, _ := metricstest.New(t)

	svc, err := auth.NewService(st, crypto.NewPasswordHasher(2), clk, id.New(), logger)
	if err != nil {
		t.Fatalf("auth.NewService() = %v", err)
	}

	mockAuth := &tickerMockAuthenticator{
		userFunc: func(_ context.Context, subject string) (jellyfin.Member, error) {
			// Member is now an administrator
			return jellyfin.Member{ID: subject, Name: "AdminMember", Admin: true}, nil
		},
	}

	cfg := config.Options{}
	cfg.Jellyfin.Enabled = true
	cfg.Jellyfin.DefaultRole = domain.RoleRequester
	cfg.Jellyfin.Timeout = 2 * time.Second

	reval := jellyfin.NewRevalidator(mockAuth, clk, cfg.Jellyfin.DefaultRole, cfg.Jellyfin.Timeout, logger)
	ticker := auth.NewJellyfinRevalidateTicker(svc, reval, cfg, clk, m, logger)
	sessionStore := auth.NewPgSessionStore(st, clk)
	ctx := t.Context()

	// Seed session with role requester, while member is now admin
	if err := sessionStore.Save(ctx, "sess-role-mismatch", map[string]any{
		auth.SessionSSOSourceKey:  "jellyfin",
		auth.SessionSSOSubjectKey: "user-promoted",
		auth.SessionSSORoleKey:    "requester",
	}, time.Hour); err != nil {
		t.Fatalf("save session: %v", err)
	}

	if err := ticker.Tick(ctx); err != nil {
		t.Fatalf("ticker.Tick(): %v", err)
	}

	// Session must be revoked
	if _, err := sessionStore.Load(ctx, "sess-role-mismatch"); err == nil {
		t.Fatal("PLANTED DEFECT TRIGGERED: sess-role-mismatch survived leader loop despite role mismatch")
	}
}
