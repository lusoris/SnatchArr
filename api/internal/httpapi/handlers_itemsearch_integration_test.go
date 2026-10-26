// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func seedItemSearchUser(t *testing.T, f *integrationFixture, role domain.Role) domain.User {
	t.Helper()
	now := f.clk.Now()
	userID := uuid.New()
	username := fmt.Sprintf("test-user-%s-%s", role, userID.String()[:8])
	const ins = `INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)`
	if _, err := f.st.Pool().Exec(t.Context(), ins, userID, username, "dummy-hash", string(role), now); err != nil {
		t.Fatalf("seedItemSearchUser: %v", err)
	}
	return domain.User{
		ID:       userID,
		Username: username,
		Role:     role,
	}
}

func callSearchItem(
	t *testing.T,
	f *integrationFixture,
	user *domain.User,
	instanceID uuid.UUID,
	req *oas.ItemSearchRequest,
) (*oas.Run, *httptest.ResponseRecorder, error) {
	t.Helper()
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/instances/"+instanceID.String()+"/items/search", nil)
	ctx := withHTTPTest(t.Context(), rec, httpReq)
	if user != nil {
		ctx = WithUser(ctx, *user)
	}
	params := oas.SearchItemParams{InstanceId: instanceID}
	run, err := f.h.SearchItem(ctx, req, params)
	return run, rec, err
}

func makeEntitySearchReq(kind string, entityID int64) *oas.ItemSearchRequest {
	return &oas.ItemSearchRequest{
		Kind:     oas.SnatchKind(kind),
		EntityID: oas.NewOptInt64(entityID),
	}
}

func makeGroupSearchReq(kind string, groupID int64) *oas.ItemSearchRequest {
	return &oas.ItemSearchRequest{
		Kind:    oas.SnatchKind(kind),
		GroupID: oas.NewOptInt64(groupID),
	}
}

func TestSearchItem_Positive(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	cd := 60
	f.h.cfg.ItemSearch.CooldownSeconds = cd
	instID := seedTestInstance(t, f)
	user := seedItemSearchUser(t, f, domain.RoleRequester)

	req := makeEntitySearchReq("missing", 42)
	run, _, err := callSearchItem(t, f, &user, instID, req)
	if err != nil {
		t.Fatalf("SearchItem() = %v, want nil", err)
	}
	if run == nil || run.Status != oas.RunStatusQueued {
		t.Fatalf("SearchItem() run = %+v, want queued status", run)
	}
	if v, ok := run.FocusEntityID.Get(); !ok || v != 42 {
		t.Fatalf("run focus_entity_id = %v (ok=%v), want 42", v, ok)
	}

	var storedLastSearched time.Time
	err = f.st.Pool().QueryRow(t.Context(), `SELECT last_searched_at FROM item_search_cooldowns WHERE user_id = $1`, user.ID).Scan(&storedLastSearched)
	if err != nil {
		t.Fatalf("select last_searched_at: %v", err)
	}
	if diff := storedLastSearched.Sub(f.clk.Now()); diff < -time.Millisecond || diff > time.Millisecond {
		t.Fatalf("stored last_searched_at = %v, want ~%v", storedLastSearched, f.clk.Now())
	}
}

func TestSearchItem_PlantedCase5_Cooldown(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	cd := 60
	f.h.cfg.ItemSearch.CooldownSeconds = cd
	instID := seedTestInstance(t, f)
	user := seedItemSearchUser(t, f, domain.RoleRequester)

	// First search succeeds
	req1 := makeEntitySearchReq("upgrade", 100)
	run1, _, err1 := callSearchItem(t, f, &user, instID, req1)
	if err1 != nil {
		t.Fatalf("first search err = %v, want nil", err1)
	}
	if run1 == nil || run1.Status != oas.RunStatusQueued {
		t.Fatalf("first search run = %+v, want queued", run1)
	}

	// Advance clock by 10s (inside 60s cooldown window)
	f.clk.Advance(10 * time.Second)

	// Second search by same user inside cooldown window -> 429
	req2 := makeEntitySearchReq("upgrade", 101)
	run2, rec2, err2 := callSearchItem(t, f, &user, instID, req2)
	if !errors.Is(err2, domain.ErrTooManyRequests) {
		t.Fatalf("second search err = %v, want ErrTooManyRequests (429)", err2)
	}
	if run2 != nil {
		t.Fatalf("second search returned run = %+v, want nil", run2)
	}
	if retryAfter := rec2.Header().Get("Retry-After"); retryAfter != "50" {
		t.Fatalf("Retry-After header = %q, want 50", retryAfter)
	}

	runs, err := f.h.runs.List(t.Context(), nil, 10, 0)
	if err != nil {
		t.Fatalf("runs.List: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs count = %d, want 1", len(runs))
	}
}

func TestSearchItem_MultiUser(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	cd := 60
	f.h.cfg.ItemSearch.CooldownSeconds = cd
	inst1 := seedTestInstance(t, f)
	inst2 := seedTestInstance(t, f)
	userA := seedItemSearchUser(t, f, domain.RoleRequester)
	userB := seedItemSearchUser(t, f, domain.RoleRequester)

	// User A searches on inst1
	reqA := makeEntitySearchReq("missing", 201)
	_, _, errA := callSearchItem(t, f, &userA, inst1, reqA)
	if errA != nil {
		t.Fatalf("user A first search = %v", errA)
	}

	// Advance 5s; User B searches on inst2 inside User A's window (should succeed)
	f.clk.Advance(5 * time.Second)
	reqB := makeEntitySearchReq("missing", 202)
	_, _, errB := callSearchItem(t, f, &userB, inst2, reqB)
	if errB != nil {
		t.Fatalf("user B search in A window = %v, want nil", errB)
	}

	// User A searches again at 5s -> blocked
	reqA2 := makeEntitySearchReq("missing", 203)
	_, recA2, errA2 := callSearchItem(t, f, &userA, inst1, reqA2)
	if !errors.Is(errA2, domain.ErrTooManyRequests) {
		t.Fatalf("user A second search err = %v, want ErrTooManyRequests", errA2)
	}
	if recA2.Header().Get("Retry-After") != "55" {
		t.Fatalf("user A retry-after = %q, want 55", recA2.Header().Get("Retry-After"))
	}
}

func TestSearchItem_Boundary_WindowEnd(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	cd := 60
	f.h.cfg.ItemSearch.CooldownSeconds = cd
	instID := seedTestInstance(t, f)
	user := seedItemSearchUser(t, f, domain.RoleRequester)

	// Search at T0
	req1 := makeEntitySearchReq("missing", 301)
	run1, _, err1 := callSearchItem(t, f, &user, instID, req1)
	if err1 != nil {
		t.Fatalf("search at T0: %v", err1)
	}
	_ = f.h.runs.Cancel(t.Context(), run1.ID)

	// Advance to T0 + 59s -> blocked
	f.clk.Advance(59 * time.Second)
	req2 := makeEntitySearchReq("missing", 302)
	_, rec2, err2 := callSearchItem(t, f, &user, instID, req2)
	if !errors.Is(err2, domain.ErrTooManyRequests) {
		t.Fatalf("search at T0+59s err = %v, want ErrTooManyRequests", err2)
	}
	if rec2.Header().Get("Retry-After") != "1" {
		t.Fatalf("retry-after at T0+59s = %q, want 1", rec2.Header().Get("Retry-After"))
	}

	// Advance 1s to reach exactly T0 + 60s (window boundary) -> allowed
	f.clk.Advance(1 * time.Second)
	req3 := makeEntitySearchReq("missing", 303)
	run3, _, err3 := callSearchItem(t, f, &user, instID, req3)
	if err3 != nil {
		t.Fatalf("search at exactly window boundary: %v, want nil", err3)
	}
	if run3 == nil || run3.Status != oas.RunStatusQueued {
		t.Fatalf("run at boundary = %+v, want queued", run3)
	}
}

func TestSearchItem_ZeroCooldownAllowsRepeat(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	f.h.cfg.ItemSearch.CooldownSeconds = 0
	instID := seedTestInstance(t, f)
	user := seedItemSearchUser(t, f, domain.RoleRequester)

	run1, _, err := callSearchItem(t, f, &user, instID, makeEntitySearchReq("missing", 401))
	if err != nil {
		t.Fatalf("first search with cooldown 0: %v", err)
	}
	_ = f.h.runs.Cancel(t.Context(), run1.ID)

	run2, _, err := callSearchItem(t, f, &user, instID, makeEntitySearchReq("missing", 402))
	if err != nil {
		t.Fatalf("immediate second search with cooldown 0: %v, want nil", err)
	}
	if run2 == nil || run2.Status != oas.RunStatusQueued {
		t.Fatalf("second run = %+v, want queued", run2)
	}
}

func TestSearchItem_RoleAuthorization(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		hasUser bool
		role    domain.Role
		wantRun bool
		wantErr error
	}{
		{"positive admin", true, domain.RoleAdmin, true, nil},
		{"positive requester", true, domain.RoleRequester, true, nil},
		{"negative viewer", true, domain.RoleViewer, false, domain.ErrForbidden},
		{"negative anonymous", false, "", false, domain.ErrUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			cd := 60
			f.h.cfg.ItemSearch.CooldownSeconds = cd
			instID := seedTestInstance(t, f)
			var user *domain.User
			if tc.hasUser {
				u := seedItemSearchUser(t, f, tc.role)
				user = &u
			}
			req := makeEntitySearchReq("missing", 501)
			run, _, err := callSearchItem(t, f, user, instID, req)
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("SearchItem() err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantRun && (run == nil || run.Status != oas.RunStatusQueued) {
				t.Fatalf("SearchItem() run = %+v, want queued", run)
			}
		})
	}
}

func TestSearchItem_InputValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		req     *oas.ItemSearchRequest
		wantErr error
	}{
		{
			"boundary entity only",
			makeEntitySearchReq("missing", 601),
			nil,
		},
		{
			"boundary group only",
			makeGroupSearchReq("missing", 602),
			nil,
		},
		{
			"negative both entity and group",
			&oas.ItemSearchRequest{
				Kind:     oas.SnatchKindMissing,
				EntityID: oas.NewOptInt64(603),
				GroupID:  oas.NewOptInt64(604),
			},
			domain.ErrBadRequest,
		},
		{
			"negative neither entity nor group",
			&oas.ItemSearchRequest{
				Kind: oas.SnatchKindMissing,
			},
			domain.ErrBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			cd := 60
			f.h.cfg.ItemSearch.CooldownSeconds = cd
			instID := seedTestInstance(t, f)
			user := seedItemSearchUser(t, f, domain.RoleRequester)
			_, _, err := callSearchItem(t, f, &user, instID, tc.req)
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("SearchItem() err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && err != nil {
				t.Fatalf("SearchItem() err = %v, want nil", err)
			}
		})
	}
}

func TestSearchItem_ConcurrentRace(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	cd := 60
	f.h.cfg.ItemSearch.CooldownSeconds = cd
	instID := seedTestInstance(t, f)
	user := seedItemSearchUser(t, f, domain.RoleRequester)

	const concurrency = 2
	var wg sync.WaitGroup
	errs := make([]error, concurrency)
	runs := make([]*oas.Run, concurrency)

	startGate := make(chan struct{})
	for i := range concurrency {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startGate
			req := makeEntitySearchReq("missing", int64(700+idx))
			run, _, err := callSearchItem(t, f, &user, instID, req)
			runs[idx] = run
			errs[idx] = err
		}(i)
	}

	close(startGate)
	wg.Wait()

	successCount := 0
	cooldownCount := 0
	for i := range concurrency {
		if errs[i] == nil && runs[i] != nil {
			successCount++
		}
		if errors.Is(errs[i], domain.ErrTooManyRequests) {
			cooldownCount++
		}
	}
	if successCount != 1 || cooldownCount != 1 {
		t.Fatalf("concurrent results: success=%d cooldown=%d, want exactly 1 success and 1 cooldown (errs: %v)",
			successCount, cooldownCount, errs)
	}
}
