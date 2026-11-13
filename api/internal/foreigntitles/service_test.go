// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package foreigntitles

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	tvdbprovider "github.com/lusoris/SnatchArr/api/internal/metadata/tvdb"
)

type fakeInstances struct {
	kind domain.AppKind
	err  error
}

func (f fakeInstances) GetWithCredentials(context.Context, uuid.UUID) (domain.Instance, instances.Credentials, error) {
	return domain.Instance{Kind: f.kind}, instances.Credentials{BaseURL: "http://arr", APIKey: "k"}, f.err
}

type fakeTitler struct {
	titles arrclient.ItemTitles
	err    error
	gotURL string
}

func (f *fakeTitler) ItemTitles(_ context.Context, _ domain.AppKind, baseURL, _ string, _ int) (arrclient.ItemTitles, error) {
	f.gotURL = baseURL
	return f.titles, f.err
}

type fakeProvider struct {
	enabled bool
	titles  []foreigntitle.LanguageTitle
	err     error
	calls   int
	ref     tvdbprovider.Ref
}

func (f *fakeProvider) Enabled() bool { return f.enabled }

func (f *fakeProvider) TitlesFor(_ context.Context, ref tvdbprovider.Ref, _ string) ([]foreigntitle.LanguageTitle, error) {
	f.calls++
	f.ref = ref
	return f.titles, f.err
}

func arrTitles() arrclient.ItemTitles {
	return arrclient.ItemTitles{
		Kind: domain.KindSonarr, ItemID: 7, Title: "Breaking Bad", TVDBID: 81189, TMDBID: 1396, IMDBID: "tt0903747",
		Aliases: []arrclient.Alias{{Title: "Der Titel aus Sonarr"}},
	}
}

func newTestService(t *testing.T, kind domain.AppKind, titler *fakeTitler, p Provider) *Service {
	t.Helper()
	s, err := newService(fakeInstances{kind: kind}, titler, p, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func sources(r Result) map[string]string {
	m := map[string]string{}
	for _, t := range r.Titles {
		m[t.Title] = t.Source
	}
	return m
}

func TestResolveMergesTVDB(t *testing.T) {
	t.Parallel()
	p := &fakeProvider{enabled: true, titles: []foreigntitle.LanguageTitle{
		{Language: "de", Title: "Breaking Bad DE", Source: "tvdb"},
		{Language: "de", Title: "der titel aus sonarr", Source: "tvdb"},
	}}
	titler := &fakeTitler{titles: arrTitles()}
	res, err := newTestService(t, domain.KindSonarr, titler, p).Resolve(t.Context(), uuid.New(), 7, "de", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := sources(res)
	// The shared title keeps whichever spelling came first, tagged as the *arr's own.
	if len(got) != 2 || got["Breaking Bad DE"] != "tvdb" || (got["Der Titel aus Sonarr"] != SourceArr && got["der titel aus sonarr"] != SourceArr) {
		t.Fatalf("titles = %+v", res.Titles)
	}
	if res.TVDB != StatusOK || p.ref.TVDBID != 81189 || p.ref.IMDBID != "tt0903747" || p.ref.Kind != domain.KindSonarr {
		t.Fatalf("status = %q ref = %+v", res.TVDB, p.ref)
	}
	if titler.gotURL != "http://arr" {
		t.Fatalf("base url = %q", titler.gotURL)
	}
}

func TestResolveTVDBFailureKeepsArrTitles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want Status
	}{
		{tvdbprovider.ErrUnauthorized, StatusUnauthorized},
		{fmt.Errorf("wrapped: %w", tvdbprovider.ErrRateLimited), StatusRateLimited},
		{tvdbprovider.ErrUnavailable, StatusUnavailable},
		{errors.New("boom"), StatusUnavailable},
	} {
		t.Run(string(tc.want), func(t *testing.T) {
			t.Parallel()
			p := &fakeProvider{enabled: true, err: tc.err}
			res, err := newTestService(t, domain.KindSonarr, &fakeTitler{titles: arrTitles()}, p).Resolve(t.Context(), uuid.New(), 7, "de", nil)
			if err != nil {
				t.Fatalf("Resolve() error = %v, want nil", err)
			}
			if res.TVDB != tc.want || len(res.Titles) != 1 || res.Titles[0].Source != SourceArr {
				t.Fatalf("res = %+v", res)
			}
		})
	}
}

func TestResolveDisabledNeverCallsProvider(t *testing.T) {
	t.Parallel()
	p := &fakeProvider{enabled: false}
	for _, prov := range []Provider{p, nil} {
		res, err := newTestService(t, domain.KindSonarr, &fakeTitler{titles: arrTitles()}, prov).Resolve(t.Context(), uuid.New(), 7, "de", nil)
		if err != nil || res.TVDB != StatusDisabled || len(res.Titles) != 1 {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
	}
	if p.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", p.calls)
	}
}

func TestResolveInvalidInput(t *testing.T) {
	t.Parallel()
	p := &fakeProvider{enabled: true}
	titler := &fakeTitler{titles: arrTitles()}
	if _, err := newTestService(t, domain.KindSonarr, titler, p).Resolve(t.Context(), uuid.New(), 7, "xx", nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown language error = %v", err)
	}
	if _, err := newTestService(t, domain.KindLidarr, titler, p).Resolve(t.Context(), uuid.New(), 7, "de", nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("lidarr error = %v", err)
	}
	if p.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", p.calls)
	}
}

func TestResolveArrErrorsPass(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, domain.KindSonarr, &fakeTitler{err: domain.ErrNotFound}, &fakeProvider{enabled: true})
	if _, err := svc.Resolve(t.Context(), uuid.New(), 7, "de", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	s, err := newService(fakeInstances{err: domain.ErrNotFound}, &fakeTitler{}, nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(t.Context(), uuid.New(), 7, "de", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("instance error = %v, want ErrNotFound", err)
	}
}

func TestResolveBoundedTitles(t *testing.T) {
	t.Parallel()
	many := make([]foreigntitle.LanguageTitle, 0, 3*MaxTitles)
	for i := range 3 * MaxTitles {
		many = append(many, foreigntitle.LanguageTitle{Language: "de", Title: fmt.Sprintf("Titel %d", i), Source: "tvdb"})
	}
	res, err := newTestService(t, domain.KindSonarr, &fakeTitler{titles: arrTitles()}, &fakeProvider{enabled: true, titles: many}).Resolve(t.Context(), uuid.New(), 7, "de", nil)
	if err != nil || len(res.Titles) != MaxTitles {
		t.Fatalf("titles = %d, err = %v, want %d", len(res.Titles), err, MaxTitles)
	}
}
