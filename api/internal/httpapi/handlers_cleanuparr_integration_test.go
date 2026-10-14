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
)

func TestCleanuparrLinkRead(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		seed       bool
		useUnknown bool
		wantErr    error
	}{
		{"positive list cleanuparr links", true, false, nil},
		{"boundary list cleanuparr links empty", false, false, nil},
		{"positive get cleanuparr link", true, false, nil},
		{"negative get cleanuparr link unknown", false, true, domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			linkID := uuid.New()
			if tc.seed {
				link := seedTestCleanuparrLink(t, f)
				linkID = link.ID
			}
			testCleanuparrLinkReadCase(t, f, tc.name, linkID, tc.wantErr)
			f.cleanupServer.assertNoWrites(t)
		})
	}
}

func testCleanuparrLinkReadCase(t *testing.T, f *integrationFixture, name string, id uuid.UUID, wantErr error) {
	t.Helper()
	ctx := t.Context()
	switch {
	case strings.Contains(name, "list cleanuparr links"):
		list, err := f.h.ListCleanuparrLinks(ctx)
		if err != nil {
			t.Fatalf("ListCleanuparrLinks() = %v", err)
		}
		wantLen := 0
		if strings.HasPrefix(name, "positive") {
			wantLen = 1
		}
		if len(list) != wantLen {
			t.Fatalf("ListCleanuparrLinks() len = %d, want %d", len(list), wantLen)
		}
		if wantLen > 0 && (list[0].ID != id || list[0].Name == "") {
			t.Fatalf("ListCleanuparrLinks() item = %+v, want ID %s", list[0], id)
		}
	case strings.Contains(name, "get cleanuparr link"):
		got, err := f.h.GetCleanuparrLink(ctx, oas.GetCleanuparrLinkParams{LinkId: id})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("GetCleanuparrLink() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil || got.ID != id || got.Name == "" {
			t.Fatalf("GetCleanuparrLink() = %+v, %v; want ID %s", got, err, id)
		}
	}
}

func TestCleanuparrLinkCreate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		user    *domain.User
		in      oas.CleanuparrLinkInput
		wantErr error
	}{
		{"positive admin creates cleanuparr link", &domain.User{Role: domain.RoleAdmin}, validCleanuparrLinkInput(), nil},
		{"negative no user", nil, validCleanuparrLinkInput(), domain.ErrUnauthorized},
		{"negative viewer forbidden", &domain.User{Role: domain.RoleViewer}, validCleanuparrLinkInput(), domain.ErrForbidden},
		{"boundary empty name", &domain.User{Role: domain.RoleAdmin}, oas.CleanuparrLinkInput{BaseURL: "http://cleanuparr.invalid:5060"}, domain.ErrInvalid},
		{"boundary invalid url", &domain.User{Role: domain.RoleAdmin}, oas.CleanuparrLinkInput{Name: "c", BaseURL: "invalid-url"}, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			got, err := f.h.CreateCleanuparrLink(ctx, &tc.in)
			f.cleanupServer.assertNoWrites(t)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("CreateCleanuparrLink() err = %v, want %v", err, tc.wantErr)
				}
				list, listErr := f.h.ListCleanuparrLinks(t.Context())
				if listErr != nil || len(list) != 0 {
					t.Fatalf("after refused create: len = %d, %v", len(list), listErr)
				}
				return
			}
			if err != nil || got.Name != tc.in.Name || got.ID == uuid.Nil {
				t.Fatalf("CreateCleanuparrLink() = %+v, %v; want name %s", got, err, tc.in.Name)
			}
			list, listErr := f.h.ListCleanuparrLinks(t.Context())
			if listErr != nil || len(list) != 1 {
				t.Fatalf("ListCleanuparrLinks() after create = %d, want 1", len(list))
			}
		})
	}
}

func TestCleanuparrLinkUpdateAndDelete(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		isDelete   bool
		user       *domain.User
		useUnknown bool
		in         oas.CleanuparrLinkInput
		wantErr    error
	}{
		{"positive update cleanuparr link", false, &domain.User{Role: domain.RoleAdmin}, false, oas.CleanuparrLinkInput{Name: "cleanup-renamed", BaseURL: "http://cleanuparr.invalid:5060"}, nil},
		{"negative update no user", false, nil, false, oas.CleanuparrLinkInput{Name: "cleanup-renamed", BaseURL: "http://cleanuparr.invalid:5060"}, domain.ErrUnauthorized},
		{"negative update viewer", false, &domain.User{Role: domain.RoleViewer}, false, oas.CleanuparrLinkInput{Name: "cleanup-renamed", BaseURL: "http://cleanuparr.invalid:5060"}, domain.ErrForbidden},
		{"negative update unknown", false, &domain.User{Role: domain.RoleAdmin}, true, oas.CleanuparrLinkInput{Name: "cleanup-renamed", BaseURL: "http://cleanuparr.invalid:5060"}, domain.ErrNotFound},
		{"boundary update empty name", false, &domain.User{Role: domain.RoleAdmin}, false, oas.CleanuparrLinkInput{BaseURL: "http://cleanuparr.invalid:5060"}, domain.ErrInvalid},
		{"positive delete cleanuparr link", true, &domain.User{Role: domain.RoleAdmin}, false, oas.CleanuparrLinkInput{}, nil},
		{"negative delete no user", true, nil, false, oas.CleanuparrLinkInput{}, domain.ErrUnauthorized},
		{"negative delete viewer", true, &domain.User{Role: domain.RoleViewer}, false, oas.CleanuparrLinkInput{}, domain.ErrForbidden},
		{"negative delete unknown", true, &domain.User{Role: domain.RoleAdmin}, true, oas.CleanuparrLinkInput{}, domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			link := seedTestCleanuparrLink(t, f)
			targetID := link.ID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			testCleanuparrUpdateOrDelete(t, f, ctx, tc.isDelete, targetID, link, tc.in, tc.wantErr)
			f.cleanupServer.assertNoWrites(t)
		})
	}
}

func testCleanuparrUpdateOrDelete(t *testing.T, f *integrationFixture, ctx context.Context, isDelete bool, targetID uuid.UUID, orig domain.CleanuparrLink, in oas.CleanuparrLinkInput, wantErr error) {
	t.Helper()
	if isDelete {
		err := f.h.DeleteCleanuparrLink(ctx, oas.DeleteCleanuparrLinkParams{LinkId: targetID})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("DeleteCleanuparrLink() err = %v, want %v", err, wantErr)
			}
			if _, getErr := f.h.GetCleanuparrLink(ctx, oas.GetCleanuparrLinkParams{LinkId: orig.ID}); getErr != nil {
				t.Fatalf("after refused delete: link missing %v", getErr)
			}
			return
		}
		if err != nil {
			t.Fatalf("DeleteCleanuparrLink() = %v", err)
		}
		if _, getErr := f.h.GetCleanuparrLink(ctx, oas.GetCleanuparrLinkParams{LinkId: orig.ID}); !errors.Is(getErr, domain.ErrNotFound) {
			t.Fatalf("after delete: GetCleanuparrLink() = %v, want not found", getErr)
		}
		return
	}
	got, err := f.h.UpdateCleanuparrLink(ctx, &in, oas.UpdateCleanuparrLinkParams{LinkId: targetID})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("UpdateCleanuparrLink() err = %v, want %v", err, wantErr)
		}
		cur, getErr := f.h.GetCleanuparrLink(ctx, oas.GetCleanuparrLinkParams{LinkId: orig.ID})
		if getErr != nil || cur.Name != orig.Name {
			t.Fatalf("after refused update: name = %s, want %s", cur.Name, orig.Name)
		}
		return
	}
	if err != nil || got.Name != in.Name {
		t.Fatalf("UpdateCleanuparrLink() = %+v, %v; want name %s", got, err, in.Name)
	}
}

func TestCleanuparrLinkProbe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		isInput    bool
		user       *domain.User
		useUnknown bool
		in         oas.CleanuparrLinkInput
		fail500    bool
		probeErr   error
		wantErr    error
	}{
		{"positive test cleanuparr link (ADR-0007)", false, nil, false, oas.CleanuparrLinkInput{}, false, nil, nil},
		{"negative test cleanuparr link unknown", false, nil, true, oas.CleanuparrLinkInput{}, false, nil, domain.ErrNotFound},
		{"boundary test cleanuparr link 500 fail", false, nil, false, oas.CleanuparrLinkInput{}, true, domain.ErrUnreachable, domain.ErrUnreachable},
		{"boundary test cleanuparr link unreachable", false, nil, false, oas.CleanuparrLinkInput{}, false, domain.ErrUnreachable, domain.ErrUnreachable},
		{"positive test cleanuparr link input", true, &domain.User{Role: domain.RoleAdmin}, false, validCleanuparrLinkInput(), false, nil, nil},
		{"negative test cleanuparr link input no user", true, nil, false, validCleanuparrLinkInput(), false, nil, domain.ErrUnauthorized},
		{"negative test cleanuparr link input viewer", true, &domain.User{Role: domain.RoleViewer}, false, validCleanuparrLinkInput(), false, nil, domain.ErrForbidden},
		{"boundary test cleanuparr link input invalid", true, &domain.User{Role: domain.RoleAdmin}, false, oas.CleanuparrLinkInput{BaseURL: "http://cleanuparr.invalid"}, false, nil, domain.ErrInvalid},
		{"boundary test cleanuparr link input unreachable", true, &domain.User{Role: domain.RoleAdmin}, false, validCleanuparrLinkInput(), false, domain.ErrUnreachable, domain.ErrUnreachable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			if tc.fail500 {
				f.cleanupServer.setFail(true)
			} else if tc.probeErr != nil {
				f.cleanupServer.setUnreachable()
			}
			linkID := uuid.New()
			if !tc.isInput && !tc.useUnknown {
				link := seedTestCleanuparrLink(t, f)
				linkID = link.ID
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			in := tc.in
			if tc.isInput && tc.name != "boundary test cleanuparr link input invalid" {
				in.BaseURL = f.cleanupServer.URL
			}
			testCleanuparrProbeCase(t, f, ctx, tc.isInput, linkID, in, tc.wantErr)
			f.cleanupServer.assertNoWrites(t)
		})
	}
}

func testCleanuparrProbeCase(t *testing.T, f *integrationFixture, ctx context.Context, isInput bool, linkID uuid.UUID, in oas.CleanuparrLinkInput, wantErr error) {
	t.Helper()
	if isInput {
		got, err := f.h.TestCleanuparrLinkInput(ctx, &in)
		f.cleanupServer.assertNoWrites(t)
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("TestCleanuparrLinkInput() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil || got.AppName != "Cleanuparr" || got.Version != "1.4.0" {
			t.Fatalf("TestCleanuparrLinkInput() = %+v, %v; want Cleanuparr 1.4.0", got, err)
		}
		return
	}
	got, err := f.h.TestCleanuparrLink(ctx, oas.TestCleanuparrLinkParams{LinkId: linkID})
	f.cleanupServer.assertNoWrites(t)
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("TestCleanuparrLink() err = %v, want %v", err, wantErr)
		}
		return
	}
	if err != nil || got.AppName != "Cleanuparr" || got.Version != "1.4.0" {
		t.Fatalf("TestCleanuparrLink() = %+v, %v; want Cleanuparr 1.4.0", got, err)
	}
}

func TestCleanuparrStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		seed      bool
		fail500   bool
		statusErr error
		wantLen   int
		reachable bool
	}{
		{"positive cleanuparr status (ADR-0007)", true, false, nil, 1, true},
		{"boundary cleanuparr status empty", false, false, nil, 0, false},
		{"boundary cleanuparr status 500 fail", true, true, domain.ErrUnreachable, 1, false},
		{"boundary cleanuparr status unreachable", true, false, domain.ErrUnreachable, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			if tc.fail500 {
				f.cleanupServer.setFail(true)
			} else if tc.statusErr != nil {
				f.cleanupServer.setUnreachable()
			}
			if tc.seed {
				_ = seedTestCleanuparrLink(t, f)
			}
			ctx := t.Context()
			list, err := f.h.GetCleanuparrStatus(ctx)
			f.cleanupServer.assertNoWrites(t)
			if err != nil {
				t.Fatalf("GetCleanuparrStatus() = %v", err)
			}
			if len(list) != tc.wantLen {
				t.Fatalf("GetCleanuparrStatus() len = %d, want %d", len(list), tc.wantLen)
			}
			if tc.wantLen > 0 {
				if list[0].Reachable != tc.reachable {
					t.Fatalf("GetCleanuparrStatus() reachable = %v, want %v", list[0].Reachable, tc.reachable)
				}
				if tc.reachable && (list[0].StruckDownloads != 5 || list[0].Name == "") {
					t.Fatalf("GetCleanuparrStatus() item = %+v", list[0])
				}
				if !tc.reachable && list[0].Error.Or("") == "" {
					t.Fatalf("GetCleanuparrStatus() expected error message, got empty")
				}
			}
		})
	}
}

func TestCleanuparrADR0007NoWrites(t *testing.T) {
	t.Parallel()
	t.Run("ADR-0007 no writes to cleanuparr", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		link := seedTestCleanuparrLink(t, f)
		_, _ = f.h.TestCleanuparrLink(t.Context(), oas.TestCleanuparrLinkParams{LinkId: link.ID})
		f.cleanupServer.assertNoWrites(t)
	})
}
