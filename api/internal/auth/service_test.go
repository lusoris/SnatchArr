// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/auth/lockout"
	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/jellyfin"
)

type stubTokenAuthenticator struct {
	byToken func(ctx context.Context, token string) (jellyfin.Member, error)
}

func (s *stubTokenAuthenticator) SignIn(_ context.Context, _, _ string) (jellyfin.Member, error) {
	return jellyfin.Member{}, domain.ErrNotFound
}

func (s *stubTokenAuthenticator) ByToken(ctx context.Context, token string) (jellyfin.Member, error) {
	return s.byToken(ctx, token)
}

func (s *stubTokenAuthenticator) QuickConnectEnabled(_ context.Context) (bool, error) {
	return false, domain.ErrNotFound
}

func (s *stubTokenAuthenticator) InitiateQuickConnect(_ context.Context) (string, string, error) {
	return "", "", domain.ErrNotFound
}

func (s *stubTokenAuthenticator) QuickConnectAuthorized(_ context.Context, _ string) (bool, error) {
	return false, domain.ErrNotFound
}

func (s *stubTokenAuthenticator) SignInQuickConnect(_ context.Context, _ string) (jellyfin.Member, error) {
	return jellyfin.Member{}, domain.ErrNotFound
}

func (s *stubTokenAuthenticator) User(_ context.Context, _ string) (jellyfin.Member, error) {
	return jellyfin.Member{}, domain.ErrNotFound
}

func newUnitTokenService(t *testing.T) (*Service, *clockwork.FakeClock) {
	t.Helper()
	clk := clock.NewFake()
	lo, err := lockout.New(lockout.NewMemoryStoreWithClock(clk), clk, lockout.Options{
		MaxFails: LockoutMaxFails,
		Window:   LockoutWindow,
		Cooldown: LockoutCooldown,
	})
	if err != nil {
		t.Fatalf("lockout.New: %v", err)
	}
	return &Service{
		clk:     clk,
		logger:  slog.New(slog.DiscardHandler),
		lockout: lo,
	}, clk
}

func TestLoginJellyfinToken_Unit(t *testing.T) {
	t.Parallel()
	t.Run("positive and boundary ip lockout counts and isolates per ip", func(t *testing.T) {
		t.Parallel()
		testUnitTokenLockout(t)
	})

	t.Run("boundary empty or whitespace ip skips lockout without global key", func(t *testing.T) {
		t.Parallel()
		testUnitTokenEmptyIP(t)
	})

	t.Run("negative disabled member returns ErrMemberDisabled and counts toward ip lockout", func(t *testing.T) {
		t.Parallel()
		testUnitTokenDisabled(t)
	})

	t.Run("negative non-auth error passes through without counting toward ip lockout", func(t *testing.T) {
		t.Parallel()
		testUnitTokenForbidden(t)
	})
}

const (
	goodUnitToken = "jf-secret-token-good"
	badUnitToken  = "jf-secret-token-bad"
)

func newUnitTokenAuthenticator() *stubTokenAuthenticator {
	return &stubTokenAuthenticator{byToken: func(_ context.Context, token string) (jellyfin.Member, error) {
		switch token {
		case goodUnitToken:
			return jellyfin.Member{ID: "jf-u1", Name: "alice", Admin: true}, nil
		case "disabled":
			return jellyfin.Member{ID: "jf-dis", Name: "disabled", Disabled: true}, nil
		case "forbidden":
			return jellyfin.Member{}, domain.ErrForbidden
		default:
			return jellyfin.Member{}, domain.ErrUnauthorized
		}
	}}
}

func testUnitTokenLockout(t *testing.T) {
	t.Helper()
	svc, clk := newUnitTokenService(t)
	ctx := t.Context()
	authStub := newUnitTokenAuthenticator()
	for i := 1; i < LockoutMaxFails; i++ {
		_, err := svc.LoginJellyfinToken(ctx, authStub, badUnitToken, "203.0.113.10")
		if !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("fail %d: err = %v, want ErrUnauthorized", i, err)
		}
		if strings.Contains(err.Error(), badUnitToken) {
			t.Fatalf("error leaked raw token: %q", err.Error())
		}
	}
	m, err := svc.LoginJellyfinToken(ctx, authStub, goodUnitToken, "203.0.113.10")
	if err != nil || m.ID != "jf-u1" || !m.Admin {
		t.Fatalf("after %d fails: got (%+v, %v), want jf-u1 admin", LockoutMaxFails-1, m, err)
	}
	if _, err = svc.LoginJellyfinToken(ctx, authStub, badUnitToken, "203.0.113.10"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("5th fail err = %v, want ErrUnauthorized", err)
	}
	testUnitTokenLockoutCooldown(t, svc, clk, authStub)
}

func testUnitTokenLockoutCooldown(t *testing.T, svc *Service, clk *clockwork.FakeClock, authStub *stubTokenAuthenticator) {
	t.Helper()
	ctx := t.Context()
	if _, err := svc.LoginJellyfinToken(ctx, authStub, goodUnitToken, "203.0.113.10"); !errors.Is(err, domain.ErrTooManyRequests) {
		t.Fatalf("locked ip err = %v, want ErrTooManyRequests", err)
	}
	if _, err := svc.LoginJellyfinToken(ctx, authStub, goodUnitToken, "203.0.113.11"); err != nil {
		t.Fatalf("clean ip while other ip locked: err = %v, want nil", err)
	}
	clk.Advance(LockoutCooldown + time.Second)
	if _, err := svc.LoginJellyfinToken(ctx, authStub, goodUnitToken, "203.0.113.10"); err != nil {
		t.Fatalf("after cooldown err = %v, want nil", err)
	}
}

func testUnitTokenEmptyIP(t *testing.T) {
	t.Helper()
	svc, _ := newUnitTokenService(t)
	ctx := t.Context()
	for i := 1; i <= LockoutMaxFails+3; i++ {
		ip := ""
		if i%2 == 0 {
			ip = "  \t "
		}
		if _, err := svc.LoginJellyfinToken(ctx, newUnitTokenAuthenticator(), badUnitToken, ip); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("attempt %d: err = %v, want ErrUnauthorized", i, err)
		}
	}
	if _, err := svc.LoginJellyfinToken(ctx, newUnitTokenAuthenticator(), goodUnitToken, ""); err != nil {
		t.Fatalf("good token with empty ip after bad attempts: err = %v, want nil", err)
	}
}

func testUnitTokenDisabled(t *testing.T) {
	t.Helper()
	svc, _ := newUnitTokenService(t)
	ctx := t.Context()
	authStub := newUnitTokenAuthenticator()
	for i := 1; i <= LockoutMaxFails; i++ {
		_, err := svc.LoginJellyfinToken(ctx, authStub, "disabled", "203.0.113.20")
		if !errors.Is(err, domain.ErrUnauthorized) || !errors.Is(err, ErrMemberDisabled) {
			t.Fatalf("attempt %d: err = %v, want ErrUnauthorized + ErrMemberDisabled", i, err)
		}
	}
	if _, err := svc.LoginJellyfinToken(ctx, authStub, goodUnitToken, "203.0.113.20"); !errors.Is(err, domain.ErrTooManyRequests) {
		t.Fatalf("after disabled failures err = %v, want ErrTooManyRequests", err)
	}
}

func testUnitTokenForbidden(t *testing.T) {
	t.Helper()
	svc, _ := newUnitTokenService(t)
	ctx := t.Context()
	authStub := newUnitTokenAuthenticator()
	for i := 1; i <= LockoutMaxFails+2; i++ {
		if _, err := svc.LoginJellyfinToken(ctx, authStub, "forbidden", "203.0.113.30"); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("attempt %d: err = %v, want ErrForbidden", i, err)
		}
	}
	if _, err := svc.LoginJellyfinToken(ctx, authStub, goodUnitToken, "203.0.113.30"); err != nil {
		t.Fatalf("after ErrForbidden attempts err = %v, want nil", err)
	}
}
