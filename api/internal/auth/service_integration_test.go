// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth_test

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

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
	svc, err := auth.NewService(storetest.New(t), crypto.NewPasswordHasher(2), clock.NewFake(), id.New(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewService() = %v", err)
	}
	return svc
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
			user, err := svc.Login(t.Context(), tc.username, tc.password)
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
