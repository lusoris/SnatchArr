// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package jellyfin_test

import (
	"context"
	"errors"
	"io"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/jellyfin"
)

type mockAuthenticator struct {
	userFunc func(ctx context.Context, id string) (jellyfin.Member, error)
}

func (m *mockAuthenticator) SignIn(context.Context, string, string) (jellyfin.Member, error) {
	return jellyfin.Member{}, domain.ErrNotFound
}

func (m *mockAuthenticator) QuickConnectEnabled(context.Context) (bool, error) {
	return false, domain.ErrNotFound
}

func (m *mockAuthenticator) InitiateQuickConnect(context.Context) (string, string, error) {
	return "", "", domain.ErrNotFound
}

func (m *mockAuthenticator) QuickConnectAuthorized(context.Context, string) (bool, error) {
	return false, domain.ErrNotFound
}

func (m *mockAuthenticator) SignInQuickConnect(context.Context, string) (jellyfin.Member, error) {
	return jellyfin.Member{}, domain.ErrNotFound
}

func (m *mockAuthenticator) User(ctx context.Context, id string) (jellyfin.Member, error) {
	if m.userFunc != nil {
		return m.userFunc(ctx, id)
	}
	return jellyfin.Member{}, domain.ErrNotFound
}

func TestRevalidator_Revalidate_Table(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		member      jellyfin.Member
		userErr     error
		currentRole domain.Role
		defaultRole domain.Role
		wantReason  string
	}{
		{
			name:        "positive valid member matching role",
			member:      jellyfin.Member{ID: "u1", Name: "alice", Admin: false},
			currentRole: domain.RoleRequester,
			defaultRole: domain.RoleRequester,
			wantReason:  "",
		},
		{
			name:        "positive valid admin matching role",
			member:      jellyfin.Member{ID: "u2", Name: "bob", Admin: true},
			currentRole: domain.RoleAdmin,
			defaultRole: domain.RoleRequester,
			wantReason:  "",
		},
		{
			name:        "negative member missing ErrNotFound",
			userErr:     domain.ErrNotFound,
			currentRole: domain.RoleRequester,
			defaultRole: domain.RoleRequester,
			wantReason:  jellyfin.ReasonMemberMissing,
		},
		{
			name:        "negative service unauthorized ErrUnauthorized",
			userErr:     domain.ErrUnauthorized,
			currentRole: domain.RoleRequester,
			defaultRole: domain.RoleRequester,
			wantReason:  jellyfin.ReasonServiceUnauthorized,
		},
		{
			name:        "negative member disabled",
			member:      jellyfin.Member{ID: "u3", Name: "charlie", Disabled: true},
			currentRole: domain.RoleRequester,
			defaultRole: domain.RoleRequester,
			wantReason:  jellyfin.ReasonMemberDisabled,
		},
		{
			name:        "boundary role changed admin to requester",
			member:      jellyfin.Member{ID: "u4", Name: "dana", Admin: false},
			currentRole: domain.RoleAdmin,
			defaultRole: domain.RoleRequester,
			wantReason:  jellyfin.ReasonRoleChanged,
		},
		{
			name:        "boundary role changed requester to admin",
			member:      jellyfin.Member{ID: "u5", Name: "eric", Admin: true},
			currentRole: domain.RoleRequester,
			defaultRole: domain.RoleRequester,
			wantReason:  jellyfin.ReasonRoleChanged,
		},
		{
			name:        "transient network error keeps session",
			userErr:     io.ErrUnexpectedEOF,
			currentRole: domain.RoleRequester,
			defaultRole: domain.RoleRequester,
			wantReason:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clk := clock.NewFake()
			mock := &mockAuthenticator{
				userFunc: func(_ context.Context, _ string) (jellyfin.Member, error) {
					return tc.member, tc.userErr
				},
			}
			r := jellyfin.NewRevalidator(mock, clk, tc.defaultRole, 2*time.Second, nil)
			got := r.Revalidate(context.Background(), "test-subject", tc.currentRole)
			if got != tc.wantReason {
				t.Fatalf("Revalidate() = %q, want %q", got, tc.wantReason)
			}
		})
	}
}

func TestRevalidator_CacheAndBoundary(t *testing.T) {
	t.Parallel()

	t.Run("fresh cache skips User call", func(t *testing.T) {
		t.Parallel()
		clk := clock.NewFake()
		var callCount atomic.Int32
		mock := &mockAuthenticator{
			userFunc: func(_ context.Context, _ string) (jellyfin.Member, error) {
				callCount.Add(1)
				return jellyfin.Member{ID: "u1", Admin: false}, nil
			},
		}
		r := jellyfin.NewRevalidator(mock, clk, domain.RoleRequester, 2*time.Second, nil)

		// First call hits User and populates cache.
		reason := r.Revalidate(context.Background(), "u1", domain.RoleRequester)
		if reason != "" || callCount.Load() != 1 {
			t.Fatalf("first call: reason=%q calls=%d, want '' and 1", reason, callCount.Load())
		}

		// Immediate second call should hit cache.
		reason = r.Revalidate(context.Background(), "u1", domain.RoleRequester)
		if reason != "" || callCount.Load() != 1 {
			t.Fatalf("cached call: reason=%q calls=%d, want '' and 1", reason, callCount.Load())
		}
	})

	t.Run("15m boundary: 14m59s no call, 15m calls User", func(t *testing.T) {
		t.Parallel()
		clk := clock.NewFake()
		var callCount atomic.Int32
		mock := &mockAuthenticator{
			userFunc: func(_ context.Context, _ string) (jellyfin.Member, error) {
				callCount.Add(1)
				return jellyfin.Member{ID: "u1", Admin: false}, nil
			},
		}
		r := jellyfin.NewRevalidator(mock, clk, domain.RoleRequester, 2*time.Second, nil)

		// Initial check at t=0
		_ = r.Revalidate(context.Background(), "u1", domain.RoleRequester)
		if callCount.Load() != 1 {
			t.Fatalf("initial call count = %d, want 1", callCount.Load())
		}

		// Advance 14m59s: boundary check, must NOT call User
		clk.Advance(14*time.Minute + 59*time.Second)
		_ = r.Revalidate(context.Background(), "u1", domain.RoleRequester)
		if callCount.Load() != 1 {
			t.Fatalf("at 14m59s call count = %d, want 1 (cache hit)", callCount.Load())
		}

		// Advance 1 more second to reach 15m: must call User
		clk.Advance(1 * time.Second)
		_ = r.Revalidate(context.Background(), "u1", domain.RoleRequester)
		if callCount.Load() != 2 {
			t.Fatalf("at 15m call count = %d, want 2 (cache expired)", callCount.Load())
		}
	})

	t.Run("transient error does not update cache so next request retries", func(t *testing.T) {
		t.Parallel()
		clk := clock.NewFake()
		var callCount atomic.Int32
		mock := &mockAuthenticator{
			userFunc: func(_ context.Context, _ string) (jellyfin.Member, error) {
				callCount.Add(1)
				return jellyfin.Member{}, errors.New("temporary 503 Service Unavailable")
			},
		}
		r := jellyfin.NewRevalidator(mock, clk, domain.RoleRequester, 2*time.Second, nil)

		// First call fails with transient error: session kept (reason ""), cache untouched
		reason := r.Revalidate(context.Background(), "u1", domain.RoleRequester)
		if reason != "" {
			t.Fatalf("transient error reason = %q, want '' (kept)", reason)
		}
		if callCount.Load() != 1 {
			t.Fatalf("call count = %d, want 1", callCount.Load())
		}

		// Second call immediately must retry since cache was not updated
		reason = r.Revalidate(context.Background(), "u1", domain.RoleRequester)
		if reason != "" {
			t.Fatalf("second call reason = %q, want ''", reason)
		}
		if callCount.Load() != 2 {
			t.Fatalf("call count on retry = %d, want 2", callCount.Load())
		}
	})

	t.Run("two sessions same subject: promotion revokes without stale cache hit", func(t *testing.T) {
		t.Parallel()
		clk := clock.NewFake()
		var callCount atomic.Int32
		mock := &mockAuthenticator{
			userFunc: func(_ context.Context, _ string) (jellyfin.Member, error) {
				callCount.Add(1)
				// Member is promoted to admin in Jellyfin
				return jellyfin.Member{ID: "u1", Admin: true}, nil
			},
		}
		r := jellyfin.NewRevalidator(mock, clk, domain.RoleRequester, 2*time.Second, nil)

		// Session 1 is Requester: Revalidate re-reads, detects promotion, revokes
		reason1 := r.Revalidate(context.Background(), "u1", domain.RoleRequester)
		if reason1 != jellyfin.ReasonRoleChanged {
			t.Fatalf("session 1 reason = %q, want %q", reason1, jellyfin.ReasonRoleChanged)
		}
		if callCount.Load() != 1 {
			t.Fatalf("session 1 callCount = %d, want 1", callCount.Load())
		}

		// Session 2 is also Requester for the same subject u1:
		// Cache MUST NOT be touched by session 1! Session 2 must also detect promotion and revoke.
		reason2 := r.Revalidate(context.Background(), "u1", domain.RoleRequester)
		if reason2 != jellyfin.ReasonRoleChanged {
			t.Fatalf("session 2 reason = %q, want %q (stale cache served!)", reason2, jellyfin.ReasonRoleChanged)
		}
		if callCount.Load() != 2 {
			t.Fatalf("session 2 callCount = %d, want 2 (stale cache hit prevented User check)", callCount.Load())
		}
	})

	t.Run("two roles same subject: cached admin role does not satisfy requester role", func(t *testing.T) {
		t.Parallel()
		clk := clock.NewFake()
		var callCount atomic.Int32
		mock := &mockAuthenticator{
			userFunc: func(_ context.Context, _ string) (jellyfin.Member, error) {
				callCount.Add(1)
				return jellyfin.Member{ID: "u1", Admin: true}, nil
			},
		}
		r := jellyfin.NewRevalidator(mock, clk, domain.RoleRequester, 2*time.Second, nil)

		// Session 1 holds Admin role, matches Jellyfin Admin: true -> valid, touches cache for (u1, admin)
		reason1 := r.Revalidate(context.Background(), "u1", domain.RoleAdmin)
		if reason1 != "" {
			t.Fatalf("session 1 reason = %q, want ''", reason1)
		}
		if callCount.Load() != 1 {
			t.Fatalf("session 1 callCount = %d, want 1", callCount.Load())
		}

		// Session 2 holds Requester role for the same subject u1:
		// Must NOT hit the cache entry for (u1, admin)!
		reason2 := r.Revalidate(context.Background(), "u1", domain.RoleRequester)
		if reason2 != jellyfin.ReasonRoleChanged {
			t.Fatalf("session 2 reason = %q, want %q", reason2, jellyfin.ReasonRoleChanged)
		}
		if callCount.Load() != 2 {
			t.Fatalf("session 2 callCount = %d, want 2 (admin cache satisfied requester)", callCount.Load())
		}
	})
}

func TestRevalidator_CacheCapacity_EvictsOldest(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	mock := &mockAuthenticator{
		userFunc: func(_ context.Context, id string) (jellyfin.Member, error) {
			return jellyfin.Member{ID: id, Admin: false}, nil
		},
	}
	r := jellyfin.NewRevalidator(mock, clk, domain.RoleRequester, 2*time.Second, nil)

	// Pre-populate cache up to capacity (4096 entries)
	const capacity = jellyfin.JellyfinCacheCapacity
	baseTime := clk.Now()
	for i := range capacity {
		subj := "subj-" + strconv.Itoa(i)
		// Give each entry an increasing timestamp
		r.SetCache(subj, domain.RoleRequester, baseTime.Add(time.Duration(i)*time.Second))
	}
	if r.CacheLen() != capacity {
		t.Fatalf("CacheLen() = %d, want %d", r.CacheLen(), capacity)
	}

	// Now insert the 4097th entry at an even later time
	clk.Advance(1 * time.Hour)
	reason := r.Revalidate(context.Background(), "subj-new", domain.RoleRequester)
	if reason != "" {
		t.Fatalf("Revalidate new entry = %q, want ''", reason)
	}

	// Cache capacity should still be bounded to 4096
	if r.CacheLen() != capacity {
		t.Fatalf("after overflow, CacheLen() = %d, want %d", r.CacheLen(), capacity)
	}

	// Verify that the oldest entry (subj-0) was evicted!
	// Calling Revalidate on subj-0 now should trigger a User call.
	var subj0Called atomic.Bool
	mock.userFunc = func(_ context.Context, id string) (jellyfin.Member, error) {
		if id == "subj-0" {
			subj0Called.Store(true)
		}
		return jellyfin.Member{ID: id, Admin: false}, nil
	}
	_ = r.Revalidate(context.Background(), "subj-0", domain.RoleRequester)
	if !subj0Called.Load() {
		t.Fatal("expected oldest entry subj-0 to have been evicted, but it was not called")
	}
}

func TestRevalidator_PlantedDefect_BypassRoleCheck(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	mock := &mockAuthenticator{
		userFunc: func(_ context.Context, id string) (jellyfin.Member, error) {
			// Member is admin in Jellyfin
			return jellyfin.Member{ID: id, Admin: true}, nil
		},
	}
	r := jellyfin.NewRevalidator(mock, clk, domain.RoleRequester, 2*time.Second, nil)

	// Session holds RoleViewer, but member is Admin. Must revoke with ReasonRoleChanged.
	reason := r.Revalidate(context.Background(), "user-role-test", domain.RoleViewer)
	if reason != jellyfin.ReasonRoleChanged {
		t.Fatalf("PLANTED DEFECT TRIGGERED: Revalidate() = %q, want %q", reason, jellyfin.ReasonRoleChanged)
	}
}
