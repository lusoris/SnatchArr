// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
)

func validProwlarrLinkInput() oas.ProwlarrLinkInput {
	return oas.ProwlarrLinkInput{
		Name:    "prowlarr-test",
		BaseURL: "http://prowlarr.invalid:9696",
		APIKey:  oas.NewOptString("test-secret-key-1234"),
		Enabled: oas.NewOptBool(true),
	}
}

func seedTestProwlarrLink(t *testing.T, f *integrationFixture) domain.ProwlarrLink {
	t.Helper()
	link, err := f.prowlarrSvc.Create(t.Context(), prowlarr.Input{
		Name:    "prowlarr-primary",
		BaseURL: f.prowlarrServer.URL,
		APIKey:  "test-secret-key-1234",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("seedTestProwlarrLink: %v", err)
	}
	return link
}

func TestProwlarrLinkRead(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		seed       bool
		useUnknown bool
		wantErr    error
	}{
		{"positive list prowlarr links", true, false, nil},
		{"boundary list prowlarr links empty", false, false, nil},
		{"positive get prowlarr link", true, false, nil},
		{"negative get prowlarr link unknown", false, true, domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			linkID := uuid.New()
			if tc.seed {
				link := seedTestProwlarrLink(t, f)
				linkID = link.ID
			}
			testProwlarrLinkReadCase(t, f, tc.name, linkID, tc.wantErr)
			f.prowlarrServer.assertNoWrites(t)
		})
	}
}

func testProwlarrLinkReadCase(t *testing.T, f *integrationFixture, name string, id uuid.UUID, wantErr error) {
	t.Helper()
	ctx := t.Context()
	switch {
	case strings.Contains(name, "list prowlarr links"):
		list, err := f.h.ListProwlarrLinks(ctx)
		if err != nil {
			t.Fatalf("ListProwlarrLinks() = %v", err)
		}
		wantLen := 0
		if strings.HasPrefix(name, "positive") {
			wantLen = 1
		}
		if len(list) != wantLen {
			t.Fatalf("ListProwlarrLinks() len = %d, want %d", len(list), wantLen)
		}
		if wantLen > 0 && (list[0].ID != id || list[0].Name == "") {
			t.Fatalf("ListProwlarrLinks() item = %+v, want ID %s", list[0], id)
		}
	case strings.Contains(name, "get prowlarr link"):
		got, err := f.h.GetProwlarrLink(ctx, oas.GetProwlarrLinkParams{LinkId: id})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("GetProwlarrLink() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil || got.ID != id || got.Name == "" {
			t.Fatalf("GetProwlarrLink() = %+v, %v; want ID %s", got, err, id)
		}
	}
}

func TestProwlarrLinkCreate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		user    *domain.User
		in      oas.ProwlarrLinkInput
		wantErr error
	}{
		{"positive admin creates prowlarr link", &domain.User{Role: domain.RoleAdmin}, validProwlarrLinkInput(), nil},
		{"negative no user", nil, validProwlarrLinkInput(), domain.ErrUnauthorized},
		{"negative viewer forbidden", &domain.User{Role: domain.RoleViewer}, validProwlarrLinkInput(), domain.ErrForbidden},
		{"boundary empty name", &domain.User{Role: domain.RoleAdmin}, oas.ProwlarrLinkInput{BaseURL: "http://prowlarr.invalid:9696"}, domain.ErrInvalid},
		{"boundary invalid url", &domain.User{Role: domain.RoleAdmin}, oas.ProwlarrLinkInput{Name: "p", BaseURL: "invalid-url"}, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			got, err := f.h.CreateProwlarrLink(ctx, &tc.in)
			f.prowlarrServer.assertNoWrites(t)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("CreateProwlarrLink() err = %v, want %v", err, tc.wantErr)
				}
				list, listErr := f.h.ListProwlarrLinks(t.Context())
				if listErr != nil || len(list) != 0 {
					t.Fatalf("after refused create: len = %d, %v", len(list), listErr)
				}
				return
			}
			if err != nil || got.Name != tc.in.Name || got.ID == uuid.Nil {
				t.Fatalf("CreateProwlarrLink() = %+v, %v; want name %s", got, err, tc.in.Name)
			}
			list, listErr := f.h.ListProwlarrLinks(t.Context())
			if listErr != nil || len(list) != 1 {
				t.Fatalf("ListProwlarrLinks() after create = %d, want 1", len(list))
			}
		})
	}
}

func TestProwlarrLinkUpdateAndDelete(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		isDelete   bool
		user       *domain.User
		useUnknown bool
		in         oas.ProwlarrLinkInput
		wantErr    error
	}{
		{"positive update prowlarr link", false, &domain.User{Role: domain.RoleAdmin}, false, oas.ProwlarrLinkInput{Name: "prowl-renamed", BaseURL: "http://prowlarr.invalid:9696"}, nil},
		{"negative update no user", false, nil, false, oas.ProwlarrLinkInput{Name: "prowl-renamed", BaseURL: "http://prowlarr.invalid:9696"}, domain.ErrUnauthorized},
		{"negative update viewer", false, &domain.User{Role: domain.RoleViewer}, false, oas.ProwlarrLinkInput{Name: "prowl-renamed", BaseURL: "http://prowlarr.invalid:9696"}, domain.ErrForbidden},
		{"negative update unknown", false, &domain.User{Role: domain.RoleAdmin}, true, oas.ProwlarrLinkInput{Name: "prowl-renamed", BaseURL: "http://prowlarr.invalid:9696"}, domain.ErrNotFound},
		{"boundary update empty name", false, &domain.User{Role: domain.RoleAdmin}, false, oas.ProwlarrLinkInput{BaseURL: "http://prowlarr.invalid:9696"}, domain.ErrInvalid},
		{"positive delete prowlarr link", true, &domain.User{Role: domain.RoleAdmin}, false, oas.ProwlarrLinkInput{}, nil},
		{"negative delete no user", true, nil, false, oas.ProwlarrLinkInput{}, domain.ErrUnauthorized},
		{"negative delete viewer", true, &domain.User{Role: domain.RoleViewer}, false, oas.ProwlarrLinkInput{}, domain.ErrForbidden},
		{"negative delete unknown", true, &domain.User{Role: domain.RoleAdmin}, true, oas.ProwlarrLinkInput{}, domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			link := seedTestProwlarrLink(t, f)
			targetID := link.ID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			testProwlarrUpdateOrDelete(t, f, ctx, tc.isDelete, targetID, link, tc.in, tc.wantErr)
			f.prowlarrServer.assertNoWrites(t)
		})
	}
}

func testProwlarrUpdateOrDelete(t *testing.T, f *integrationFixture, ctx context.Context, isDelete bool, targetID uuid.UUID, orig domain.ProwlarrLink, in oas.ProwlarrLinkInput, wantErr error) {
	t.Helper()
	if isDelete {
		err := f.h.DeleteProwlarrLink(ctx, oas.DeleteProwlarrLinkParams{LinkId: targetID})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("DeleteProwlarrLink() err = %v, want %v", err, wantErr)
			}
			if _, getErr := f.h.GetProwlarrLink(ctx, oas.GetProwlarrLinkParams{LinkId: orig.ID}); getErr != nil {
				t.Fatalf("after refused delete: link missing %v", getErr)
			}
			return
		}
		if err != nil {
			t.Fatalf("DeleteProwlarrLink() = %v", err)
		}
		if _, getErr := f.h.GetProwlarrLink(ctx, oas.GetProwlarrLinkParams{LinkId: orig.ID}); !errors.Is(getErr, domain.ErrNotFound) {
			t.Fatalf("after delete: GetProwlarrLink() = %v, want not found", getErr)
		}
		return
	}
	got, err := f.h.UpdateProwlarrLink(ctx, &in, oas.UpdateProwlarrLinkParams{LinkId: targetID})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("UpdateProwlarrLink() err = %v, want %v", err, wantErr)
		}
		cur, getErr := f.h.GetProwlarrLink(ctx, oas.GetProwlarrLinkParams{LinkId: orig.ID})
		if getErr != nil || cur.Name != orig.Name {
			t.Fatalf("after refused update: name = %s, want %s", cur.Name, orig.Name)
		}
		return
	}
	if err != nil || got.Name != in.Name {
		t.Fatalf("UpdateProwlarrLink() = %+v, %v; want name %s", got, err, in.Name)
	}
}

func TestProwlarrLinkProbe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		isInput    bool
		user       *domain.User
		useUnknown bool
		in         oas.ProwlarrLinkInput
		fail500    bool
		probeErr   error
		wantErr    error
	}{
		{"positive test prowlarr link", false, &domain.User{Role: domain.RoleAdmin}, false, oas.ProwlarrLinkInput{}, false, nil, nil},
		{"negative test prowlarr link no user", false, nil, false, oas.ProwlarrLinkInput{}, false, nil, domain.ErrUnauthorized},
		{"negative test prowlarr link viewer", false, &domain.User{Role: domain.RoleViewer}, false, oas.ProwlarrLinkInput{}, false, nil, domain.ErrForbidden},
		{"negative test prowlarr link unknown", false, &domain.User{Role: domain.RoleAdmin}, true, oas.ProwlarrLinkInput{}, false, nil, domain.ErrNotFound},
		{"boundary test prowlarr link 500 fail", false, &domain.User{Role: domain.RoleAdmin}, false, oas.ProwlarrLinkInput{}, true, domain.ErrUnreachable, domain.ErrUnreachable},
		{"boundary test prowlarr link unreachable", false, &domain.User{Role: domain.RoleAdmin}, false, oas.ProwlarrLinkInput{}, false, domain.ErrUnreachable, domain.ErrUnreachable},
		{"positive test prowlarr link input", true, &domain.User{Role: domain.RoleAdmin}, false, validProwlarrLinkInput(), false, nil, nil},
		{"negative test prowlarr link input no user", true, nil, false, validProwlarrLinkInput(), false, nil, domain.ErrUnauthorized},
		{"negative test prowlarr link input viewer", true, &domain.User{Role: domain.RoleViewer}, false, validProwlarrLinkInput(), false, nil, domain.ErrForbidden},
		{"boundary test prowlarr link input invalid", true, &domain.User{Role: domain.RoleAdmin}, false, oas.ProwlarrLinkInput{BaseURL: "http://prowlarr.invalid"}, false, nil, domain.ErrInvalid},
		{"boundary test prowlarr link input unreachable", true, &domain.User{Role: domain.RoleAdmin}, false, validProwlarrLinkInput(), false, domain.ErrUnreachable, domain.ErrUnreachable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			if tc.fail500 {
				f.prowlarrServer.setFail(true)
			} else if tc.probeErr != nil {
				f.prowlarrServer.setUnreachable()
			}
			linkID := uuid.New()
			if !tc.isInput && !tc.useUnknown {
				link := seedTestProwlarrLink(t, f)
				linkID = link.ID
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			in := tc.in
			if tc.isInput && tc.name != "boundary test prowlarr link input invalid" {
				in.BaseURL = f.prowlarrServer.URL
			}
			testProwlarrProbeCase(t, f, ctx, tc.isInput, linkID, in, tc.wantErr)
			f.prowlarrServer.assertNoWrites(t)
		})
	}
}

func testProwlarrProbeCase(t *testing.T, f *integrationFixture, ctx context.Context, isInput bool, linkID uuid.UUID, in oas.ProwlarrLinkInput, wantErr error) {
	t.Helper()
	if isInput {
		got, err := f.h.TestProwlarrLinkInput(ctx, &in)
		f.prowlarrServer.assertNoWrites(t)
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("TestProwlarrLinkInput() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil || got.AppName != "Prowlarr" || got.Version != "1.15.0" {
			t.Fatalf("TestProwlarrLinkInput() = %+v, %v; want Prowlarr 1.15.0", got, err)
		}
		return
	}
	got, err := f.h.TestProwlarrLink(ctx, oas.TestProwlarrLinkParams{LinkId: linkID})
	f.prowlarrServer.assertNoWrites(t)
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("TestProwlarrLink() err = %v, want %v", err, wantErr)
		}
		return
	}
	if err != nil || got.AppName != "Prowlarr" || got.Version != "1.15.0" {
		t.Fatalf("TestProwlarrLink() = %+v, %v; want Prowlarr 1.15.0", got, err)
	}
}

func TestProwlarrStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		seed      bool
		fail500   bool
		statusErr error
		wantLen   int
		reachable bool
	}{
		{"positive prowlarr status", true, false, nil, 1, true},
		{"boundary prowlarr status empty", false, false, nil, 0, false},
		{"boundary prowlarr status 500 fail", true, true, domain.ErrUnreachable, 1, false},
		{"boundary prowlarr status unreachable", true, false, domain.ErrUnreachable, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			if tc.fail500 {
				f.prowlarrServer.setFail(true)
			} else if tc.statusErr != nil {
				f.prowlarrServer.setUnreachable()
			}
			if tc.seed {
				_ = seedTestProwlarrLink(t, f)
			}
			ctx := t.Context()
			list, err := f.h.GetProwlarrStatus(ctx)
			f.prowlarrServer.assertNoWrites(t)
			if err != nil {
				t.Fatalf("GetProwlarrStatus() = %v", err)
			}
			if len(list) != tc.wantLen {
				t.Fatalf("GetProwlarrStatus() len = %d, want %d", len(list), tc.wantLen)
			}
			if tc.wantLen > 0 {
				if list[0].Reachable != tc.reachable {
					t.Fatalf("GetProwlarrStatus() reachable = %v, want %v", list[0].Reachable, tc.reachable)
				}
				if tc.reachable && (len(list[0].Indexers) != 1 || list[0].Name == "") {
					t.Fatalf("GetProwlarrStatus() item = %+v", list[0])
				}
				if !tc.reachable && list[0].Error.Or("") == "" {
					t.Fatalf("GetProwlarrStatus() expected error message, got empty")
				}
			}
		})
	}
}

func TestProwlarrNoWrites(t *testing.T) {
	t.Parallel()
	t.Run("no writes to prowlarr", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		link := seedTestProwlarrLink(t, f)
		_, _ = f.h.TestProwlarrLink(t.Context(), oas.TestProwlarrLinkParams{LinkId: link.ID})
		f.prowlarrServer.assertNoWrites(t)
	})
}
