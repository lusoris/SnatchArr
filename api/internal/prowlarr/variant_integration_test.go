// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

type variantClient struct {
	*stubClient
	searchMu     sync.Mutex
	searchCalls  int
	searchErr    error
	empty        bool
	releases     []prowlarrclient.Release
	beforeStatus func(context.Context) error
}

func (c *variantClient) Status(ctx context.Context, baseURL, key string) (prowlarrclient.Status, error) {
	if c.beforeStatus != nil {
		if err := c.beforeStatus(ctx); err != nil {
			return prowlarrclient.Status{}, err
		}
	}
	return c.stubClient.Status(ctx, baseURL, key)
}

func (c *variantClient) Search(context.Context, string, string, prowlarrclient.SearchQuery) ([]prowlarrclient.Release, error) {
	c.searchMu.Lock()
	defer c.searchMu.Unlock()
	c.searchCalls++
	if c.empty {
		return nil, c.searchErr
	}
	return c.releases, c.searchErr
}

func (c *variantClient) searches() int {
	c.searchMu.Lock()
	defer c.searchMu.Unlock()
	return c.searchCalls
}

type variantFixture struct {
	svc    *prowlarr.Service
	st     *store.Store
	clk    *clockwork.FakeClock
	client *variantClient
	linkID uuid.UUID
}

func newVariantFixture(t *testing.T) variantFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x24}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	client := &variantClient{
		stubClient: &stubClient{},
		releases:   []prowlarrclient.Release{{GUID: "variant-release", Categories: []int{2000}}},
	}
	svc := prowlarr.New(st, enc, clk, id.New(), client, &stubDiscoverer{}, nil,
		slog.New(slog.DiscardHandler), config.Default())
	link, err := svc.Create(t.Context(), prowlarr.Input{
		Name: "variant", BaseURL: linkURL, APIKey: firstKey, Enabled: true, HourlyCap: 100,
	})
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	return variantFixture{svc: svc, st: st, clk: clk, client: client, linkID: link.ID}
}

func (f variantFixture) seedBudget(t *testing.T, linkCap, globalCap, linkUsed, globalUsed int32) {
	t.Helper()
	ctx := t.Context()
	if _, err := f.st.Pool().Exec(ctx, "UPDATE prowlarr_links SET hourly_cap = $1 WHERE id = $2", linkCap, f.linkID); err != nil {
		t.Fatalf("link cap: %v", err)
	}
	if _, err := f.st.Pool().Exec(ctx, "UPDATE settings SET global_hourly_cap = $1", globalCap); err != nil {
		t.Fatalf("global cap: %v", err)
	}
	if err := f.st.Q().AddProwlarrBucketUsed(ctx, sqlcgen.AddProwlarrBucketUsedParams{
		LinkID: f.linkID, WindowStart: snatch.Minute(f.clk.Now()), Used: linkUsed,
	}); err != nil {
		t.Fatalf("seed link usage: %v", err)
	}
	if err := f.st.Q().AddGlobalBucketUsed(ctx, sqlcgen.AddGlobalBucketUsedParams{
		WindowStart: snatch.Minute(f.clk.Now()), Used: globalUsed,
	}); err != nil {
		t.Fatalf("seed global usage: %v", err)
	}
}

func (f variantFixture) assertUsage(t *testing.T, linkWant, globalWant int64) {
	t.Helper()
	lu, err := linkUsage(t.Context(), f.st, f.linkID, f.clk.Now())
	if err != nil {
		t.Fatalf("link usage: %v", err)
	}
	gu, err := globalUsage(t.Context(), f.st, f.clk.Now())
	if err != nil {
		t.Fatalf("global usage: %v", err)
	}
	if lu != linkWant || gu != globalWant {
		t.Fatalf("usage link/global = %d/%d, want %d/%d", lu, gu, linkWant, globalWant)
	}
}

func TestSearchVariant_DebitCacheExpiry(t *testing.T) {
	t.Parallel()
	f := newVariantFixture(t)
	f.seedBudget(t, 100, 100, 0, 0)
	q := prowlarrclient.SearchQuery{Query: "Title", IndexerIDs: []int{1, 2, 3}}
	for _, tc := range []struct {
		name    string
		advance time.Duration
		debit   int
		used    int
		calls   int
	}{
		{name: "miss", debit: 3, used: 3, calls: 1},
		{name: "hit", used: 3, calls: 1},
		{name: "exact hour hit", advance: time.Hour, used: 3, calls: 1},
		{name: "expired miss", advance: time.Nanosecond, debit: 3, used: 6, calls: 2},
	} {
		f.clk.Advance(tc.advance)
		releases, budget, err := f.svc.SearchVariant(t.Context(), f.linkID, q)
		if err != nil || len(releases) != 1 || releases[0].GUID != "variant-release" {
			t.Fatalf("%s: SearchVariant() = %+v, %+v, %v", tc.name, releases, budget, err)
		}
		if budget.Debited != tc.debit || budget.LinkUsed != tc.used || budget.LinkCap != 100 || budget.Warn {
			t.Fatalf("%s: budget = %+v, want debit %d, usage %d/100 without warning", tc.name, budget, tc.debit, tc.used)
		}
		f.assertUsage(t, int64(tc.used), int64(tc.used))
		if calls := f.client.searches(); calls != tc.calls {
			t.Fatalf("%s: Search calls = %d, want %d", tc.name, calls, tc.calls)
		}
	}
}

func TestSearchVariant_BudgetBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                 string
		linkCap, globalCap   int32
		linkUsed, globalUsed int32
		warn                 bool
		refused              bool
	}{
		{name: "link exactly cap", linkCap: 10, globalCap: 100, linkUsed: 8, warn: true},
		{name: "link one over", linkCap: 10, globalCap: 100, linkUsed: 9, refused: true},
		{name: "global exactly cap", linkCap: 100, globalCap: 10, globalUsed: 8, warn: true},
		{name: "global one over", linkCap: 100, globalCap: 10, globalUsed: 9, refused: true},
		{name: "link below warning", linkCap: 10, globalCap: 100, linkUsed: 5},
		{name: "link at warning", linkCap: 10, globalCap: 100, linkUsed: 6, warn: true},
		{name: "global below warning", linkCap: 100, globalCap: 10, globalUsed: 5},
		{name: "global at warning", linkCap: 100, globalCap: 10, globalUsed: 6, warn: true},
		{name: "global disabled", linkCap: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newVariantFixture(t)
			f.seedBudget(t, tc.linkCap, tc.globalCap, tc.linkUsed, tc.globalUsed)
			q := prowlarrclient.SearchQuery{Query: "Title", IndexerIDs: []int{1, 2}}
			_, budget, err := f.svc.SearchVariant(t.Context(), f.linkID, q)
			if tc.refused {
				if !errors.Is(err, domain.ErrTooManyRequests) || budget != (prowlarr.SearchBudget{}) || f.client.searches() != 0 {
					t.Fatalf("refusal = %+v, %v; calls %d", budget, err, f.client.searches())
				}
				f.assertUsage(t, int64(tc.linkUsed), int64(tc.globalUsed))
				return
			}
			want := prowlarr.SearchBudget{Debited: 2, LinkUsed: int(tc.linkUsed) + 2, LinkCap: int(tc.linkCap), Warn: tc.warn}
			if err != nil || budget != want {
				t.Fatalf("budget = %+v, %v; want %+v", budget, err, want)
			}
			globalWant := int64(tc.globalUsed)
			if tc.globalCap > 0 {
				globalWant += 2
			}
			f.assertUsage(t, int64(tc.linkUsed)+2, globalWant)
		})
	}
}

func TestSearchVariant_IndexerValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		count int
		err   error
	}{
		{count: 0, err: domain.ErrInvalid},
		{count: 1},
		// Persisted caps top out at 500: valid input, refused by the budget.
		{count: 1000, err: domain.ErrTooManyRequests},
		{count: 1001, err: domain.ErrInvalid},
	} {
		t.Run(fmt.Sprintf("indexers=%d", tc.count), func(t *testing.T) {
			t.Parallel()
			f := newVariantFixture(t)
			f.seedBudget(t, 500, 1000, 0, 0)
			ids := make([]int, tc.count)
			for i := range tc.count {
				ids[i] = i + 1
			}
			releases, budget, err := f.svc.SearchVariant(t.Context(), f.linkID, prowlarrclient.SearchQuery{IndexerIDs: ids})
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if tc.err != nil {
				f.assertUsage(t, 0, 0)
				if budget.Debited != 0 || f.client.searches() != 0 {
					t.Fatalf("rejected input charged or searched: %+v, calls %d", budget, f.client.searches())
				}
			} else {
				f.assertUsage(t, 1, 1)
				if calls := f.client.searches(); calls != 1 || !reflect.DeepEqual(releases, f.client.releases) {
					t.Fatalf("valid search = %+v, calls %d; want %+v, calls 1", releases, calls, f.client.releases)
				}
			}
		})
	}
}

func TestSearchVariant_NormalizedCache(t *testing.T) {
	t.Parallel()
	f := newVariantFixture(t)
	f.seedBudget(t, 100, 100, 0, 0)
	for i, q := range []prowlarrclient.SearchQuery{
		{Query: "Title", IndexerIDs: []int{2, 1, 2}, Categories: []int{5000, 2000}},
		{Query: "Title", IndexerIDs: []int{1, 2}, Categories: []int{2000, 5000}},
		{Query: "Title", IndexerIDs: []int{1, 2}, Categories: []int{2000}},
	} {
		_, budget, err := f.svc.SearchVariant(t.Context(), f.linkID, q)
		if err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
		// A duplicate indexer is searched once, so it is charged once (codacy, #1009).
		wantDebit := []int{2, 0, 2}[i]
		if budget.Debited != wantDebit {
			t.Fatalf("search %d debit = %d, want %d", i, budget.Debited, wantDebit)
		}
	}
	f.assertUsage(t, 4, 4)
	if calls := f.client.searches(); calls != 2 {
		t.Fatalf("Search calls = %d, want 2", calls)
	}
}

func TestSearchVariant_SearchErrorKeepsDebit(t *testing.T) {
	t.Parallel()
	f := newVariantFixture(t)
	f.seedBudget(t, 100, 100, 0, 0)
	f.client.searchErr = errRefused
	q := prowlarrclient.SearchQuery{Query: "Title", IndexerIDs: []int{1, 2}}
	for i := range 2 {
		releases, budget, err := f.svc.SearchVariant(t.Context(), f.linkID, q)
		if !errors.Is(err, errRefused) || releases != nil || budget.Debited != 2 || budget.LinkUsed != (i+1)*2 {
			t.Fatalf("failed search = %+v, %+v, %v", releases, budget, err)
		}
	}
	f.assertUsage(t, 4, 4)
	if calls := f.client.searches(); calls != 2 {
		t.Fatalf("Search calls = %d, want 2 (errors never cached)", calls)
	}
}

func TestSearchVariant_PreflightOnCacheHit(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"disabled", "unreachable", "missing"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newVariantFixture(t)
			f.seedBudget(t, 100, 100, 0, 0)
			q := prowlarrclient.SearchQuery{IndexerIDs: []int{1, 2}}
			if _, _, err := f.svc.SearchVariant(t.Context(), f.linkID, q); err != nil {
				t.Fatalf("prime cache: %v", err)
			}
			wantErr := domain.ErrUnreachable
			switch mode {
			case "disabled":
				wantErr = domain.ErrForbidden
				if _, err := f.st.Pool().Exec(t.Context(), "UPDATE prowlarr_links SET enabled = false WHERE id = $1", f.linkID); err != nil {
					t.Fatalf("disable link: %v", err)
				}
			case "unreachable":
				f.client.err = errRefused
				f.clk.Advance(prowlarr.StatusTTL)
			case "missing":
				wantErr = domain.ErrNotFound
				if err := f.svc.Delete(t.Context(), f.linkID); err != nil {
					t.Fatalf("delete link: %v", err)
				}
			}
			_, budget, err := f.svc.SearchVariant(t.Context(), f.linkID, q)
			if !errors.Is(err, wantErr) || budget.Debited != 0 || f.client.searches() != 1 {
				t.Fatalf("preflight = %+v, %v; calls %d", budget, err, f.client.searches())
			}
		})
	}
}

func TestSearchVariant_ConcurrentBudget(t *testing.T) {
	t.Parallel()
	f := newVariantFixture(t)
	f.seedBudget(t, 10, 10, 0, 0)
	results := make(chan error, 4)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			_, _, err := f.svc.SearchVariant(t.Context(), f.linkID, prowlarrclient.SearchQuery{
				Query: fmt.Sprintf("Title %d", i), IndexerIDs: []int{1, 2, 3},
			})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	refused := 0
	for err := range results {
		if errors.Is(err, domain.ErrTooManyRequests) {
			refused++
		} else if err != nil {
			t.Fatalf("search: %v", err)
		}
	}
	if refused != 1 || f.client.searches() != 3 {
		t.Fatalf("refused %d, searches %d; want 1, 3", refused, f.client.searches())
	}
	f.assertUsage(t, 9, 9)
}

func TestSearchVariant_EmptyResultsCached(t *testing.T) {
	t.Parallel()
	f := newVariantFixture(t)
	f.seedBudget(t, 100, 100, 0, 0)
	f.client.empty = true
	q := prowlarrclient.SearchQuery{IndexerIDs: []int{1, 2}}
	for _, wantDebit := range []int{2, 0} {
		releases, budget, err := f.svc.SearchVariant(t.Context(), f.linkID, q)
		if err != nil || len(releases) != 0 || budget.Debited != wantDebit {
			t.Fatalf("empty search = %+v, %+v, %v; want debit %d", releases, budget, err, wantDebit)
		}
	}
	f.assertUsage(t, 2, 2)
	if calls := f.client.searches(); calls != 1 {
		t.Fatalf("Search calls = %d, want 1", calls)
	}
}

func TestSearchVariant_UpdateInvalidatesCache(t *testing.T) {
	t.Parallel()
	f := newVariantFixture(t)
	f.seedBudget(t, 100, 100, 0, 0)
	q := prowlarrclient.SearchQuery{Query: "Title", IndexerIDs: []int{1, 2}}
	for _, step := range []string{"prime", "cached", "updated"} {
		if step == "updated" {
			if _, err := f.svc.Update(t.Context(), f.linkID, prowlarr.Input{
				Name: "renamed", BaseURL: linkURL, Enabled: true,
			}); err != nil {
				t.Fatalf("update link: %v", err)
			}
		}
		_, budget, err := f.svc.SearchVariant(t.Context(), f.linkID, q)
		wantDebit, wantCalls, wantUsage := 2, 1, int64(2)
		if step == "cached" {
			wantDebit = 0
		}
		if step == "updated" {
			wantCalls, wantUsage = 2, 4
		}
		if err != nil || budget.Debited != wantDebit || f.client.searches() != wantCalls {
			t.Fatalf("%s: budget %+v, error %v, calls %d; want debit %d, calls %d",
				step, budget, err, f.client.searches(), wantDebit, wantCalls)
		}
		f.assertUsage(t, wantUsage, wantUsage)
	}
}

func TestSearchVariant_CapChangedAfterPreflight(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"variant", "grab"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			f := newVariantFixture(t)
			f.seedBudget(t, 100, 100, 8, 8)
			changed := false
			// Status runs after GetProwlarrLink captured cap 100, before the budget transaction.
			f.client.beforeStatus = func(ctx context.Context) error {
				_, err := f.st.Pool().Exec(ctx, "UPDATE prowlarr_links SET hourly_cap = 8 WHERE id = $1", f.linkID)
				changed = err == nil
				return err
			}
			q := prowlarrclient.SearchQuery{Query: "Title", IndexerIDs: []int{1, 2}}
			var err error
			if method == "grab" {
				_, err = f.svc.GrabForItem(t.Context(), f.linkID, q)
			} else {
				var budget prowlarr.SearchBudget
				_, budget, err = f.svc.SearchVariant(t.Context(), f.linkID, q)
				if budget != (prowlarr.SearchBudget{}) {
					t.Fatalf("refused search returned budget %+v", budget)
				}
			}
			if !changed || !errors.Is(err, domain.ErrTooManyRequests) || f.client.searches() != 0 {
				t.Fatalf("cap changed %v, error %v, calls %d; want refusal before search", changed, err, f.client.searches())
			}
			f.assertUsage(t, 8, 8)
		})
	}
}

func TestSearchVariant_ReleaseCacheBound(t *testing.T) {
	t.Parallel()
	maxReleases := config.Default().Prowlarr.MaxCachedReleases
	for _, tc := range []struct {
		count       int
		secondDebit int
		calls       int
	}{
		{count: maxReleases, calls: 1},
		{count: maxReleases + 1, secondDebit: 2, calls: 2},
	} {
		t.Run(fmt.Sprintf("releases=%d", tc.count), func(t *testing.T) {
			t.Parallel()
			f := newVariantFixture(t)
			f.seedBudget(t, 100, 100, 0, 0)
			f.client.releases = make([]prowlarrclient.Release, tc.count)
			for i := range tc.count {
				f.client.releases[i] = prowlarrclient.Release{GUID: strconv.Itoa(i), Categories: []int{2000}}
			}
			q := prowlarrclient.SearchQuery{IndexerIDs: []int{1, 2}}
			for _, wantDebit := range []int{2, tc.secondDebit} {
				releases, budget, err := f.svc.SearchVariant(t.Context(), f.linkID, q)
				if err != nil || budget.Debited != wantDebit || !reflect.DeepEqual(releases, f.client.releases) {
					t.Fatalf("search returned %d releases, budget %+v, error %v; want %d releases, debit %d",
						len(releases), budget, err, tc.count, wantDebit)
				}
			}
			if calls := f.client.searches(); calls != tc.calls {
				t.Fatalf("Search calls = %d, want %d", calls, tc.calls)
			}
			f.assertUsage(t, int64(2+tc.secondDebit), int64(2+tc.secondDebit))
		})
	}
}
