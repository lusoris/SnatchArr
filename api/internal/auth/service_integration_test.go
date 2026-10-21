// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
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
	svc, clk, _ := newServiceWithStore(t)
	return svc, clk
}

func newServiceWithStore(t *testing.T) (*auth.Service, *clockwork.FakeClock, *store.Store) {
	t.Helper()
	clk := clock.NewFake()
	st := storetest.New(t)
	svc, err := auth.NewService(st, crypto.NewPasswordHasher(2), clk, id.New(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewService() = %v", err)
	}
	return svc, clk, st
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

func TestChangePassword(t *testing.T) {
	t.Parallel()
	svc, clk, st := newServiceWithStore(t)
	ctx := t.Context()

	// Seed first admin
	user, err := svc.Setup(ctx, adminName, adminPass)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Seed 3 sessions for admin
	sessionTTL := 8 * time.Hour
	s1 := "sess-1"
	s2 := "sess-2"
	s3 := "sess-3"
	adminData := []byte(`{"user_id": "` + user.ID.String() + `"}`)
	for _, sid := range []string{s1, s2, s3} {
		if saveErr := st.Q().SaveSession(ctx, sqlcgen.SaveSessionParams{ID: sid, Data: adminData, ExpiresAt: clk.Now().Add(sessionTTL)}); saveErr != nil {
			t.Fatalf("SaveSession(%s): %v", sid, saveErr)
		}
	}

	// Seed another user session
	otherID := uuid.New()
	sOther := "sess-other"
	otherData := []byte(`{"user_id": "` + otherID.String() + `"}`)
	if saveErr := st.Q().SaveSession(ctx, sqlcgen.SaveSessionParams{ID: sOther, Data: otherData, ExpiresAt: clk.Now().Add(sessionTTL)}); saveErr != nil {
		t.Fatalf("SaveSession(%s): %v", sOther, saveErr)
	}

	// Negative 1: wrong current password -> refused, nothing changes
	err = svc.ChangePassword(ctx, user.ID, "wrong-password", "new-valid-password-123")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("ChangePassword wrong pass = %v, want ErrUnauthorized", err)
	}
	for _, sid := range []string{s1, s2, s3, sOther} {
		if _, loadErr := st.Q().LoadSession(ctx, sqlcgen.LoadSessionParams{ID: sid, ExpiresAt: clk.Now()}); loadErr != nil {
			t.Fatalf("session %s unexpectedly missing after failed password change: %v", sid, loadErr)
		}
	}

	// Negative 2: invalid new password -> refused, nothing changes
	err = svc.ChangePassword(ctx, user.ID, adminPass, "short")
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("ChangePassword short pass = %v, want ErrInvalid", err)
	}
	for _, sid := range []string{s1, s2, s3, sOther} {
		if _, loadErr := st.Q().LoadSession(ctx, sqlcgen.LoadSessionParams{ID: sid, ExpiresAt: clk.Now()}); loadErr != nil {
			t.Fatalf("session %s unexpectedly missing after failed password change: %v", sid, loadErr)
		}
	}

	// Positive: correct current password + valid new password while on s1
	newPass := "brand-new-secret-password-123"
	if err := svc.ChangePassword(ctx, user.ID, adminPass, newPass); err != nil {
		t.Fatalf("ChangePassword success err = %v", err)
	}

	// Every session of the user is gone, the calling s1 included (#268); the handler issues a new id.
	for _, sid := range []string{s1, s2, s3} {
		if _, err := st.Q().LoadSession(ctx, sqlcgen.LoadSessionParams{ID: sid, ExpiresAt: clk.Now()}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("session %s should be deleted, got err = %v", sid, err)
		}
	}

	// Other user session sOther is untouched
	if _, err := st.Q().LoadSession(ctx, sqlcgen.LoadSessionParams{ID: sOther, ExpiresAt: clk.Now()}); err != nil {
		t.Fatalf("other user's session was deleted: %v", err)
	}

	// User can log in with new password
	if _, err := svc.Login(ctx, adminName, newPass, "127.0.0.1"); err != nil {
		t.Fatalf("Login with new password failed: %v", err)
	}

	// User cannot log in with old password
	if _, err := svc.Login(ctx, adminName, adminPass, "127.0.0.1"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Login with old password = %v, want ErrUnauthorized", err)
	}
}

func TestChangePasswordRollbackOnDeleteFailure(t *testing.T) {
	t.Parallel()
	svc, clk, st := newServiceWithStore(t)
	ctx := t.Context()

	user, err := svc.Setup(ctx, adminName, adminPass)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	sessionTTL := 8 * time.Hour
	s1 := "sess-1"
	s2 := "sess-2"
	adminData := []byte(`{"user_id": "` + user.ID.String() + `"}`)
	for _, sid := range []string{s1, s2} {
		if saveErr := st.Q().SaveSession(ctx, sqlcgen.SaveSessionParams{ID: sid, Data: adminData, ExpiresAt: clk.Now().Add(sessionTTL)}); saveErr != nil {
			t.Fatalf("SaveSession(%s): %v", sid, saveErr)
		}
	}

	cancCtx, cancel := context.WithCancel(t.Context())
	svc.SetPostPasswordUpdateHook(func(_ context.Context) {
		cancel()
	})

	newPass := "brand-new-secret-password-123"
	err = svc.ChangePassword(cancCtx, user.ID, adminPass, newPass)
	if err == nil {
		t.Fatal("ChangePassword with cancelled context after update should have failed, got nil")
	}

	// Verify password was rolled back: old password still works, new password does not
	if _, err := svc.Login(ctx, adminName, adminPass, "127.0.0.1"); err != nil {
		t.Fatalf("Login with old password failed after rollback: %v", err)
	}
	if _, err := svc.Login(ctx, adminName, newPass, "127.0.0.1"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Login with new password succeeded after rollback, got %v; want ErrUnauthorized", err)
	}

	// Both sessions still exist
	for _, sid := range []string{s1, s2} {
		if _, loadErr := st.Q().LoadSession(ctx, sqlcgen.LoadSessionParams{ID: sid, ExpiresAt: clk.Now()}); loadErr != nil {
			t.Fatalf("session %s missing after rollback: %v", sid, loadErr)
		}
	}
}

func TestProvisionExternalViewer_BeforeSetupRefused(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	ctx := t.Context()

	_, err := svc.ProvisionExternalViewer(ctx, "oidc-", "alice@example.com")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("ProvisionExternalViewer before setup = %v, want ErrForbidden", err)
	}

	admin, err := svc.Setup(ctx, adminName, adminPass)
	if err != nil {
		t.Fatalf("Setup after failed provisioning: %v", err)
	}
	if admin.Role != domain.RoleAdmin {
		t.Fatalf("admin role = %v, want %v", admin.Role, domain.RoleAdmin)
	}

	viewer, err := svc.ProvisionExternalViewer(ctx, "oidc-", "alice@example.com")
	if err != nil {
		t.Fatalf("ProvisionExternalViewer after setup: %v", err)
	}
	if viewer.Role != domain.RoleViewer {
		t.Fatalf("viewer role = %v, want %v", viewer.Role, domain.RoleViewer)
	}
}

func TestProvisionExternalViewer_PositiveAndReuse(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	ctx := t.Context()

	if _, err := svc.Setup(ctx, adminName, adminPass); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	viewer1, err := svc.ProvisionExternalViewer(ctx, "oidc-", "alice@example.com")
	if err != nil {
		t.Fatalf("ProvisionExternalViewer: %v", err)
	}
	wantUsername := auth.ExternalViewerUsername("oidc-", "alice@example.com")
	if viewer1.Role != domain.RoleViewer || viewer1.Username != wantUsername {
		t.Fatalf("viewer1 = %+v, want role viewer and username %q", viewer1, wantUsername)
	}

	viewer2, err := svc.ProvisionExternalViewer(ctx, "oidc-", "alice@example.com")
	if err != nil {
		t.Fatalf("ProvisionExternalViewer second call: %v", err)
	}
	if viewer2.ID != viewer1.ID || viewer2.Username != viewer1.Username {
		t.Fatalf("viewer2 = %+v, want same as viewer1 %+v", viewer2, viewer1)
	}
}

func TestProvisionExternalViewer_ConcurrentSignInsOneRow(t *testing.T) {
	t.Parallel()
	svc, _, st := newServiceWithStore(t)
	ctx := t.Context()

	if _, err := svc.Setup(ctx, adminName, adminPass); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	const concurrency = 10
	var (
		wg      sync.WaitGroup
		userIDs = make([]uuid.UUID, concurrency)
		errs    = make([]error, concurrency)
	)

	wg.Add(concurrency)
	for i := range concurrency {
		go func(idx int) {
			defer wg.Done()
			u, pErr := svc.ProvisionExternalViewer(ctx, "oidc-", "concurrent-identity@domain.invalid")
			userIDs[idx] = u.ID
			errs[idx] = pErr
		}(i)
	}
	wg.Wait()

	for i, pErr := range errs {
		if pErr != nil {
			t.Fatalf("worker %d failed: %v", i, pErr)
		}
		if userIDs[i] != userIDs[0] {
			t.Fatalf("worker %d got user ID %v, want %v", i, userIDs[i], userIDs[0])
		}
	}

	count, err := st.Q().CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if count != 2 {
		t.Fatalf("CountUsers = %d, want 2 (1 admin, 1 viewer)", count)
	}
}

func TestLogin_ViewerRowPasswordRefused401(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	ctx := t.Context()

	if _, err := svc.Setup(ctx, adminName, adminPass); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	viewer, err := svc.ProvisionExternalViewer(ctx, "oidc-", "alice@example.com")
	if err != nil {
		t.Fatalf("ProvisionExternalViewer: %v", err)
	}

	_, err = svc.Login(ctx, viewer.Username, "some-password-attempt", "127.0.0.1")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Login against viewer row = %v, want ErrUnauthorized", err)
	}
}

func TestExternalViewerUsername_Boundary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		prefix   string
		identity string
	}{
		{"boundary standard oidc", "oidc-", "user-123"},
		{"boundary standard proxy", "proxy-", "user-456"},
		{"boundary subject longer than 64 characters", "oidc-", strings.Repeat("long-subject-segment-", 5)},
		{"boundary containing colons and at symbol", "oidc-", "auth0|user:123@sub.example.com"},
		{"boundary empty prefix", "", "user@example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			username := auth.ExternalViewerUsername(tc.prefix, tc.identity)
			if !strings.HasPrefix(username, tc.prefix) {
				t.Fatalf("username %q missing prefix %q", username, tc.prefix)
			}
			if err := domain.ValidateUsername(username); err != nil {
				t.Fatalf("ValidateUsername(%q) = %v, want valid", username, err)
			}
		})
	}
}
