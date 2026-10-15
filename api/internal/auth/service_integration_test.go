// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth_test

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

const (
	adminName = "admin"
	adminPass = "correct-horse-battery"
)

func newService(t *testing.T) *auth.Service {
	t.Helper()
	svc, _ := newServiceWithClock(t)
	return svc
}

func newServiceWithClock(t *testing.T) (*auth.Service, *clockwork.FakeClock) {
	t.Helper()
	clk := clock.NewFake()
	svc, err := auth.NewService(storetest.New(t), crypto.NewPasswordHasher(2), clk, id.New(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewService() = %v", err)
	}
	return svc, clk
}

func TestSetupCreatesTheFirstAdminOnce(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	if done, err := svc.SetupComplete(t.Context()); err != nil || done {
		t.Fatalf("SetupComplete() before setup = %v, %v; want false", done, err)
	}
	user, err := svc.Setup(t.Context(), "  "+adminName+"  ", adminPass)
	if err != nil {
		t.Fatalf("Setup() = %v", err)
	}
	if user.Username != adminName || user.Role != domain.RoleAdmin {
		t.Errorf("Setup() = %q role %q, want %q role %q", user.Username, user.Role, adminName, domain.RoleAdmin)
	}
	done, err := svc.SetupComplete(t.Context())
	if err != nil || !done {
		t.Fatalf("SetupComplete() after setup = %v, %v; want true", done, err)
	}
	if _, err = svc.Setup(t.Context(), "second", adminPass); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("second Setup() = %v, want %v", err, domain.ErrConflict)
	}
	loaded, err := svc.User(t.Context(), user.ID)
	if err != nil || loaded.Username != adminName {
		t.Errorf("User() = %+v, %v; want %q", loaded, err, adminName)
	}
}

func TestSetupValidatesTheCredentials(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, username, password string
		want                     error
	}{
		{"boundary shortest username", strings.Repeat("a", domain.MinUsernameLen), adminPass, nil},
		{"boundary longest username", strings.Repeat("a", domain.MaxUsernameLen), adminPass, nil},
		{"boundary shortest password", adminName, strings.Repeat("p", domain.MinPasswordLen), nil},
		{"boundary longest password", adminName, strings.Repeat("p", domain.MaxPasswordLen), nil},
		{"negative username too short", strings.Repeat("a", domain.MinUsernameLen-1), adminPass, domain.ErrInvalid},
		{"negative username too long", strings.Repeat("a", domain.MaxUsernameLen+1), adminPass, domain.ErrInvalid},
		{"negative username with a space inside", "ad min", adminPass, domain.ErrInvalid},
		{"negative password too short", adminName, strings.Repeat("p", domain.MinPasswordLen-1), domain.ErrInvalid},
		{"negative password too long", adminName, strings.Repeat("p", domain.MaxPasswordLen+1), domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := newService(t)
			_, err := svc.Setup(t.Context(), tc.username, tc.password)
			if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
				t.Fatalf("Setup() = %v, want %v", err, tc.want)
			}
			if done, _ := svc.SetupComplete(t.Context()); done != (tc.want == nil) {
				t.Errorf("SetupComplete() = %v after Setup() = %v", done, err)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	admin, err := svc.Setup(t.Context(), adminName, adminPass)
	if err != nil {
		t.Fatalf("Setup() = %v", err)
	}
	cases := []struct {
		name, username, password string
		want                     error
	}{
		{"positive right credentials", adminName, adminPass, nil},
		{"positive username with surrounding spaces", " " + adminName + " ", adminPass, nil},
		{"negative wrong password", adminName, adminPass + "x", domain.ErrUnauthorized},
		{"negative unknown username", "nobody", adminPass, domain.ErrUnauthorized},
		{"negative username differs in case", strings.ToUpper(adminName), adminPass, domain.ErrUnauthorized},
		{"boundary empty password", adminName, "", domain.ErrUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			user, err := svc.Login(t.Context(), tc.username, tc.password, "192.0.2.1")
			if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
				t.Fatalf("Login() = %v, want %v", err, tc.want)
			}
			if tc.want == nil && user.ID != admin.ID {
				t.Errorf("Login() user %s, want %s", user.ID, admin.ID)
			}
		})
	}
}

func TestUnknownUserIsNotFound(t *testing.T) {
	t.Parallel()
	if _, err := newService(t).User(t.Context(), uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("User() = %v, want %v", err, domain.ErrNotFound)
	}
}

func TestLoginThrottlingBoundary(t *testing.T) {
	t.Parallel()
	svc, clk := newServiceWithClock(t)
	ctx := t.Context()
	_, err := svc.Setup(ctx, adminName, adminPass)
	if err != nil {
		t.Fatalf("Setup() = %v", err)
	}

	clientIP := "192.0.2.1"

	// Boundary 1: MaxFails-1 (4) failures -> next login with right password succeeds
	for i := 1; i < auth.LockoutMaxFails; i++ {
		if _, err := svc.Login(ctx, adminName, "wrong-pass", clientIP); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("fail attempt %d: err = %v, want ErrUnauthorized", i, err)
		}
	}
	if _, err := svc.Login(ctx, adminName, adminPass, clientIP); err != nil {
		t.Fatalf("login after %d fails = %v, want success", auth.LockoutMaxFails-1, err)
	}

	// Boundary 2: exactly MaxFails (5) failures from a new IP -> locks both user and IP
	clientIP2 := "192.0.2.2"
	for i := 1; i <= auth.LockoutMaxFails; i++ {
		if _, err := svc.Login(ctx, adminName, "wrong-pass", clientIP2); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("fail attempt %d: err = %v, want ErrUnauthorized", i, err)
		}
	}
	// Right password refused with domain.ErrTooManyRequests from same IP and from clean IP (user key locked)
	if _, err := svc.Login(ctx, adminName, adminPass, clientIP2); !errors.Is(err, domain.ErrTooManyRequests) {
		t.Fatalf("Login() with right password when locked = %v, want ErrTooManyRequests", err)
	}
	if _, err := svc.Login(ctx, adminName, adminPass, "192.0.2.99"); !errors.Is(err, domain.ErrTooManyRequests) {
		t.Fatalf("Login() from clean IP when user locked = %v, want ErrTooManyRequests", err)
	}

	// Boundary 3: Cooldown expires -> right password succeeds
	clk.Advance(auth.LockoutCooldown - time.Second)
	if _, err := svc.Login(ctx, adminName, adminPass, clientIP2); !errors.Is(err, domain.ErrTooManyRequests) {
		t.Fatalf("Login() just before cooldown expiry = %v, want ErrTooManyRequests", err)
	}
	clk.Advance(2 * time.Second)
	if _, err := svc.Login(ctx, adminName, adminPass, clientIP2); err != nil {
		t.Fatalf("Login() after cooldown = %v, want success", err)
	}

	// Boundary 4: IP key locks a second username from the same IP
	testIPLockoutAcrossUsers(t, svc, clientIP)
}

func testIPLockoutAcrossUsers(t *testing.T, svc *auth.Service, clientIP string) {
	t.Helper()
	ctx := t.Context()
	otherIP := "192.0.2.3"

	// 5 failed logins for non-existent user from otherIP locks otherIP
	for i := 1; i <= auth.LockoutMaxFails; i++ {
		if _, err := svc.Login(ctx, "attacker-random-user", "bad-pass", otherIP); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("attacker attempt %d: err = %v, want ErrUnauthorized", i, err)
		}
	}

	// admin login from locked otherIP is refused even with correct password
	if _, err := svc.Login(ctx, adminName, adminPass, otherIP); !errors.Is(err, domain.ErrTooManyRequests) {
		t.Fatalf("admin login from locked IP = %v, want ErrTooManyRequests", err)
	}

	// admin login from clean clientIP succeeds
	if _, err := svc.Login(ctx, adminName, adminPass, clientIP); err != nil {
		t.Fatalf("admin login from clean IP = %v, want success", err)
	}
}
