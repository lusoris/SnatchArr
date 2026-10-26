// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

const (
	firstKey  = "first-key-0001"
	secondKey = "second-key-0002"
	linkURL   = "http://prowlarr.invalid:9696"
)

var errRefused = errors.New("connection refused by the test")

type stubClient struct {
	mu        sync.Mutex
	keys      []string
	calls     int
	indexers  []prowlarrclient.Indexer
	statuses  map[int]time.Time
	usage     map[int]prowlarrclient.Usage
	usageHour map[int]prowlarrclient.Usage
	err       error
}

func (c *stubClient) seen(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.keys = append(c.keys, key)
	return c.err
}

func (c *stubClient) Status(_ context.Context, _, key string) (prowlarrclient.Status, error) {
	return prowlarrclient.Status{Version: "1.15.0"}, c.seen(key)
}

func (c *stubClient) Indexers(_ context.Context, _, key string) ([]prowlarrclient.Indexer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.keys = append(c.keys, key)
	if c.err != nil {
		return nil, c.err
	}
	return c.indexers, nil
}

func (c *stubClient) Usage(_ context.Context, _, key string, start, end time.Time) (map[int]prowlarrclient.Usage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.keys = append(c.keys, key)
	if c.err != nil {
		return nil, c.err
	}
	if end.Sub(start) <= 2*time.Hour && c.usageHour != nil {
		return c.usageHour, nil
	}
	return c.usage, nil
}

func (c *stubClient) Statuses(_ context.Context, _, key string) (map[int]time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.keys = append(c.keys, key)
	if c.err != nil {
		return nil, c.err
	}
	return c.statuses, nil
}

func (c *stubClient) ProwlarrURLs(_ context.Context, _, key string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.keys = append(c.keys, key)
	if c.err != nil {
		return nil, c.err
	}
	return nil, nil
}

func (c *stubClient) Search(_ context.Context, _, key string, _ prowlarrclient.SearchQuery) ([]prowlarrclient.Release, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.keys = append(c.keys, key)
	if c.err != nil {
		return nil, c.err
	}
	return nil, nil
}

func (c *stubClient) Grab(_ context.Context, _, key string, _ prowlarrclient.Release) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.keys = append(c.keys, key)
	return c.err
}

func (c *stubClient) lastKey() (string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.keys) == 0 {
		return "", c.calls
	}
	return c.keys[len(c.keys)-1], c.calls
}

type stubDiscoverer struct {
	indexers []arr.ProviderResource
	err      error
}

func (d *stubDiscoverer) Indexers(context.Context, domain.AppKind, string, string) ([]arr.ProviderResource, error) {
	return d.indexers, d.err
}

type stubProber struct{}

func (stubProber) Probe(context.Context, domain.AppKind, string, string) (arrclient.Result, error) {
	return arrclient.Result{AppName: "Sonarr", Version: "3.0.0"}, nil
}

type fixture struct {
	svc      *prowlarr.Service
	st       *store.Store
	clk      *clockwork.FakeClock
	client   *stubClient
	discover *stubDiscoverer
	instSvc  *instances.Service
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x24}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	st := storetest.New(t)
	clk := clock.NewFake()
	client := &stubClient{}
	discover := &stubDiscoverer{}
	instSvc := instances.New(st, enc, clk, id.New(), stubProber{}, slog.New(slog.DiscardHandler))
	cfg := config.Options{
		Prowlarr: config.ProwlarrOptions{LimitThreshold: 0.9},
	}
	svc := prowlarr.New(st, enc, clk, id.New(), client, discover, instSvc, slog.New(slog.DiscardHandler), cfg)
	return fixture{svc: svc, st: st, clk: clk, client: client, discover: discover, instSvc: instSvc}
}

func (f fixture) create(t *testing.T) domain.ProwlarrLink {
	t.Helper()
	link, err := f.svc.Create(t.Context(), prowlarr.Input{Name: " prowlarr ", BaseURL: linkURL + "/", APIKey: " " + firstKey + " ", Enabled: true})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	return link
}

func TestCreateNormalisesAndSealsTheKey(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	link := f.create(t)
	if link.Name != "prowlarr" || link.BaseURL != linkURL || !link.Enabled {
		t.Errorf("Create() = %+v, want the trimmed name and URL", link)
	}
	row, err := f.st.Q().GetProwlarrLink(t.Context(), link.ID)
	if err != nil {
		t.Fatalf("GetProwlarrLink() = %v", err)
	}
	if bytes.Contains(row.ApiKeyEnc, []byte(firstKey)) {
		t.Error("the stored api key holds the plaintext key")
	}
	if _, err = f.svc.Test(t.Context(), link.ID); err != nil {
		t.Fatalf("Test() = %v", err)
	}
	if key, _ := f.client.lastKey(); key != firstKey {
		t.Errorf("Prowlarr was called with key %q, want the trimmed key %q", key, firstKey)
	}
	listed, err := f.svc.List(t.Context())
	if err != nil || len(listed) != 1 || listed[0].ID != link.ID {
		t.Errorf("List() = %+v, %v; want the one link", listed, err)
	}
}

func TestUpdateKeepsOrReplacesTheKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, key, want string
	}{
		{"positive empty key keeps the stored one", "", firstKey},
		{"positive new key replaces the stored one", secondKey, secondKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			link := f.create(t)
			updated, err := f.svc.Update(t.Context(), link.ID, prowlarr.Input{Name: "prowl-2", BaseURL: "http://prowlarr.invalid:9697", APIKey: tc.key, Enabled: false})
			if err != nil {
				t.Fatalf("Update() = %v", err)
			}
			if updated.Name != "prowl-2" || updated.Enabled {
				t.Errorf("unexpected updated link: %+v", updated)
			}
			if _, err = f.svc.Test(t.Context(), link.ID); err != nil {
				t.Fatalf("Test() after update = %v", err)
			}
			if key, _ := f.client.lastKey(); key != tc.want {
				t.Errorf("Test() used key %q, want %q", key, tc.want)
			}
		})
	}
}

func TestDeleteRemovesTheLink(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	link := f.create(t)
	if err := f.svc.Delete(t.Context(), link.ID); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if _, err := f.svc.Get(t.Context(), link.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get() after Delete() = %v, want ErrNotFound", err)
	}
	if err := f.svc.Delete(t.Context(), link.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second Delete() = %v, want ErrNotFound", err)
	}
}

func TestTestAndTestInput(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	st, err := f.svc.TestInput(t.Context(), prowlarr.Input{Name: "input", BaseURL: linkURL, APIKey: "test-api-key"})
	if err != nil || st.Version != "1.15.0" {
		t.Fatalf("TestInput() = %+v, %v", st, err)
	}
	f.client.err = errRefused
	if _, err = f.svc.TestInput(t.Context(), prowlarr.Input{Name: "input", BaseURL: linkURL, APIKey: "test-api-key"}); err == nil {
		t.Fatal("TestInput() must fail when Prowlarr refuses")
	}
}

func TestStatusCachedAndRefreshes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	qLimit := 100
	f.client.indexers = []prowlarrclient.Indexer{
		{ID: 1, Name: "Idx1", Enabled: true, QueryLimit: &qLimit, LimitsUnit: 0},
	}
	f.client.usage = map[int]prowlarrclient.Usage{1: {Queries: 50, Grabs: 5}}
	_ = f.create(t)

	views, err := f.svc.Status(t.Context())
	if err != nil || len(views) != 1 || !views[0].Reachable || len(views[0].Indexers) != 1 {
		t.Fatalf("Status() = %+v, %v", views, err)
	}
	idx := views[0].Indexers[0]
	if idx.ID != 1 || idx.UsedQueries != 50 || idx.Exhausted {
		t.Errorf("unexpected indexer status: %+v", idx)
	}
	_, calls1 := f.client.lastKey()

	// Calling status immediately reuses the cached view (no new Prowlarr calls).
	views2, err := f.svc.Status(t.Context())
	if err != nil || len(views2) != 1 {
		t.Fatalf("Status() 2 = %+v, %v", views2, err)
	}
	_, calls2 := f.client.lastKey()
	if calls2 != calls1 {
		t.Errorf("Status() called client %d times, want cached %d", calls2, calls1)
	}

	// Fast-forward past StatusTTL -> calls Prowlarr again.
	f.clk.Advance(prowlarr.StatusTTL + time.Second)
	f.client.usage = map[int]prowlarrclient.Usage{1: {Queries: 95, Grabs: 10}}
	views3, err := f.svc.Status(t.Context())
	if err != nil || len(views3) != 1 {
		t.Fatalf("Status() 3 = %+v, %v", views3, err)
	}
	if views3[0].Indexers[0].UsedQueries != 95 || !views3[0].Indexers[0].Exhausted {
		t.Errorf("refreshed indexer status: %+v, want queries=95, exhausted=true", views3[0].Indexers[0])
	}
}

func TestObserveFailsGracefullyWhenUnreachable(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.client.err = errRefused
	link := f.create(t)

	views, err := f.svc.Status(t.Context())
	if err != nil || len(views) != 1 {
		t.Fatalf("Status() = %+v, %v", views, err)
	}
	if views[0].Reachable || views[0].Error == "" {
		t.Errorf("unreachable link must report error: %+v", views[0])
	}

	row, err := f.st.Q().GetProwlarrLink(t.Context(), link.ID)
	if err != nil {
		t.Fatalf("GetProwlarrLink() = %v", err)
	}
	if row.LastError == nil || *row.LastError == "" {
		t.Error("recorded check must store last_error")
	}
}

type statusGrabLimitCase struct {
	name       string
	queryLimit *int
	grabLimit  *int
	limitsUnit int
	usageDay   prowlarrclient.Usage
	usageHour  prowlarrclient.Usage
	wantExh    bool
	wantExhBy  string
}

func runStatusGrabLimitCase(t *testing.T, tc statusGrabLimitCase) {
	t.Helper()
	f := newFixture(t)
	f.client.indexers = []prowlarrclient.Indexer{
		{ID: 1, Name: "Idx1", Enabled: true, QueryLimit: tc.queryLimit, GrabLimit: tc.grabLimit, LimitsUnit: tc.limitsUnit},
	}
	f.client.usage = map[int]prowlarrclient.Usage{1: tc.usageDay}
	f.client.usageHour = map[int]prowlarrclient.Usage{1: tc.usageHour}
	_ = f.create(t)

	views, err := f.svc.Status(t.Context())
	if err != nil || len(views) != 1 || len(views[0].Indexers) != 1 {
		t.Fatalf("Status() = %+v, %v", views, err)
	}
	idx := views[0].Indexers[0]
	if idx.Exhausted != tc.wantExh || idx.ExhaustedBy != tc.wantExhBy {
		t.Errorf("idx = %+v, want Exhausted=%v ExhaustedBy=%q", idx, tc.wantExh, tc.wantExhBy)
	}
}

func TestStatus_GrabLimitsAndExhaustedBy(t *testing.T) {
	t.Parallel()
	q100, g100, g50, g10 := 100, 100, 50, 10
	cases := []statusGrabLimitCase{
		{"positive grab at limit -> exhausted by grab_limit", nil, &g100, 0, prowlarrclient.Usage{Grabs: 90}, prowlarrclient.Usage{}, true, "grab_limit"},
		{"boundary grab one below threshold -> not exhausted", nil, &g100, 0, prowlarrclient.Usage{Grabs: 89}, prowlarrclient.Usage{}, false, ""},
		{"positive query-only limit -> exhausted by query_limit", &q100, nil, 0, prowlarrclient.Usage{Queries: 90}, prowlarrclient.Usage{}, true, "query_limit"},
		{"positive query checked first when both at limit", &q100, &g100, 0, prowlarrclient.Usage{Queries: 90, Grabs: 90}, prowlarrclient.Usage{}, true, "query_limit"},
		{"negative grab limit absent nil -> not exhausted", nil, nil, 0, prowlarrclient.Usage{Grabs: 500}, prowlarrclient.Usage{}, false, ""},
		{"boundary grabLimit set but queryLimit nil -> limited", nil, &g50, 0, prowlarrclient.Usage{Grabs: 45}, prowlarrclient.Usage{}, true, "grab_limit"},
		{"positive limitsUnit 1 uses hour usage for grabs", nil, &g10, 1, prowlarrclient.Usage{Grabs: 2}, prowlarrclient.Usage{Grabs: 9}, true, "grab_limit"},
		{"boundary limitsUnit 1 hour below threshold despite day above", nil, &g10, 1, prowlarrclient.Usage{Grabs: 10}, prowlarrclient.Usage{Grabs: 5}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStatusGrabLimitCase(t, tc)
		})
	}
}
