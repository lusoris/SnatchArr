// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitles"
)

// stubForeignTitles records the request it gets and answers with a fixed result or error.
type stubForeignTitles struct {
	res    foreigntitles.Result
	err    error
	calls  int
	season *int
	lang   string
}

func (s *stubForeignTitles) Resolve(_ context.Context, _ uuid.UUID, _ int, lang string, season *int) (foreigntitles.Result, error) {
	s.calls++
	s.lang, s.season = lang, season
	return s.res, s.err
}

func TestGetForeignTitles(t *testing.T) {
	t.Parallel()
	admin := withUser(t.Context(), domain.User{Role: domain.RoleAdmin})
	stub := &stubForeignTitles{res: foreigntitles.Result{
		Language: "de", TVDB: foreigntitles.StatusOK,
		Titles: []foreigntitles.Title{{Title: "Der Titel", Source: "tvdb"}, {Title: "Alias", Source: "arr"}},
	}}
	h := &Handlers{foreignTitles: stub}
	got, err := h.GetForeignTitles(admin, oas.GetForeignTitlesParams{InstanceId: uuid.New(), ItemId: 7, Language: "de", Season: oas.NewOptInt(2)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Titles) != 2 || got.Titles[0].Source != oas.ForeignTitleSourceTvdb || got.Tvdb != oas.ForeignTitlesTvdbOk {
		t.Fatalf("got = %+v", got)
	}
	if got.Attribution != "Data courtesy of TheTVDB.com" {
		t.Fatalf("attribution = %q", got.Attribution)
	}
	if stub.season == nil || *stub.season != 2 || stub.lang != "de" {
		t.Fatalf("service got lang %q season %v", stub.lang, stub.season)
	}
	if _, err := h.GetForeignTitles(admin, oas.GetForeignTitlesParams{ItemId: 7, Language: "de"}); err != nil || stub.season != nil {
		t.Fatalf("no season: err = %v, season = %v", err, stub.season)
	}
}

func TestGetForeignTitlesNegative(t *testing.T) {
	t.Parallel()
	params := oas.GetForeignTitlesParams{InstanceId: uuid.New(), ItemId: 7, Language: "de"}
	for _, tc := range []struct {
		name string
		ctx  context.Context
		h    *Handlers
		want error
	}{
		{"viewer forbidden", withUser(t.Context(), domain.User{Role: domain.RoleViewer}), &Handlers{foreignTitles: &stubForeignTitles{}}, domain.ErrForbidden},
		{"anonymous", t.Context(), &Handlers{foreignTitles: &stubForeignTitles{}}, domain.ErrUnauthorized},
		{"not configured", withUser(t.Context(), domain.User{Role: domain.RoleAdmin}), &Handlers{}, domain.ErrInvalid},
		{"service error passes", withUser(t.Context(), domain.User{Role: domain.RoleAdmin}), &Handlers{foreignTitles: &stubForeignTitles{err: domain.ErrNotFound}}, domain.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := tc.h.GetForeignTitles(tc.ctx, params); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestGetForeignTitlesViewerMakesNoCall checks the admin gate runs before the service.
func TestGetForeignTitlesViewerMakesNoCall(t *testing.T) {
	t.Parallel()
	stub := &stubForeignTitles{}
	h := &Handlers{foreignTitles: stub}
	_, err := h.GetForeignTitles(withUser(t.Context(), domain.User{Role: domain.RoleViewer}), oas.GetForeignTitlesParams{ItemId: 7, Language: "de"})
	if !errors.Is(err, domain.ErrForbidden) || stub.calls != 0 {
		t.Fatalf("error = %v, calls = %d", err, stub.calls)
	}
}
