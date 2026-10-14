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
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
)

func TestSeerrLinkRead(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		seed       bool
		useUnknown bool
		wantErr    error
	}{
		{"positive list seerr links", true, false, nil},
		{"boundary list seerr links empty", false, false, nil},
		{"positive get seerr link", true, false, nil},
		{"negative get seerr link unknown", false, true, domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			linkID := uuid.New()
			if tc.seed {
				link := seedTestSeerrLink(t, f)
				linkID = link.ID
			}
			testSeerrLinkReadCase(t, f, tc.name, linkID, tc.wantErr)
		})
	}
}

func testSeerrLinkReadCase(t *testing.T, f *integrationFixture, name string, id uuid.UUID, wantErr error) {
	t.Helper()
	ctx := t.Context()
	switch {
	case strings.Contains(name, "list seerr links"):
		list, err := f.h.ListSeerrLinks(ctx)
		if err != nil {
			t.Fatalf("ListSeerrLinks() = %v", err)
		}
		wantLen := 0
		if strings.HasPrefix(name, "positive") {
			wantLen = 1
		}
		if len(list) != wantLen {
			t.Fatalf("ListSeerrLinks() len = %d, want %d", len(list), wantLen)
		}
		if wantLen > 0 && (list[0].ID != id || list[0].Name == "") {
			t.Fatalf("ListSeerrLinks() item = %+v, want ID %s", list[0], id)
		}
	case strings.Contains(name, "get seerr link"):
		got, err := f.h.GetSeerrLink(ctx, oas.GetSeerrLinkParams{LinkId: id})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("GetSeerrLink() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil || got.ID != id || got.Name == "" {
			t.Fatalf("GetSeerrLink() = %+v, %v; want ID %s", got, err, id)
		}
	}
}

func TestSeerrLinkCreate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		user    *domain.User
		in      oas.SeerrLinkInput
		wantErr error
	}{
		{"positive admin creates seerr link", &domain.User{Role: domain.RoleAdmin}, validSeerrLinkInput(), nil},
		{"negative no user", nil, validSeerrLinkInput(), domain.ErrUnauthorized},
		{"negative viewer forbidden", &domain.User{Role: domain.RoleViewer}, validSeerrLinkInput(), domain.ErrForbidden},
		{"boundary empty name", &domain.User{Role: domain.RoleAdmin}, oas.SeerrLinkInput{BaseURL: "http://seerr.invalid:5055"}, domain.ErrInvalid},
		{"boundary invalid url", &domain.User{Role: domain.RoleAdmin}, oas.SeerrLinkInput{Name: "seerr", BaseURL: "invalid-url"}, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			got, err := f.h.CreateSeerrLink(ctx, &tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("CreateSeerrLink() err = %v, want %v", err, tc.wantErr)
				}
				list, listErr := f.h.ListSeerrLinks(t.Context())
				if listErr != nil || len(list) != 0 {
					t.Fatalf("after refused create: len = %d, %v", len(list), listErr)
				}
				return
			}
			if err != nil || got.Name != tc.in.Name || got.ID == uuid.Nil {
				t.Fatalf("CreateSeerrLink() = %+v, %v; want name %s", got, err, tc.in.Name)
			}
			list, listErr := f.h.ListSeerrLinks(t.Context())
			if listErr != nil || len(list) != 1 {
				t.Fatalf("ListSeerrLinks() after create = %d, want 1", len(list))
			}
		})
	}
}

func TestSeerrLinkUpdateAndDelete(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		isDelete   bool
		user       *domain.User
		useUnknown bool
		in         oas.SeerrLinkInput
		wantErr    error
	}{
		{"positive update seerr link", false, &domain.User{Role: domain.RoleAdmin}, false, oas.SeerrLinkInput{Name: "seerr-renamed", BaseURL: "http://seerr.invalid:5055"}, nil},
		{"negative update no user", false, nil, false, oas.SeerrLinkInput{Name: "seerr-renamed", BaseURL: "http://seerr.invalid:5055"}, domain.ErrUnauthorized},
		{"negative update viewer", false, &domain.User{Role: domain.RoleViewer}, false, oas.SeerrLinkInput{Name: "seerr-renamed", BaseURL: "http://seerr.invalid:5055"}, domain.ErrForbidden},
		{"negative update unknown", false, &domain.User{Role: domain.RoleAdmin}, true, oas.SeerrLinkInput{Name: "seerr-renamed", BaseURL: "http://seerr.invalid:5055"}, domain.ErrNotFound},
		{"boundary update empty name", false, &domain.User{Role: domain.RoleAdmin}, false, oas.SeerrLinkInput{BaseURL: "http://seerr.invalid:5055"}, domain.ErrInvalid},
		{"positive delete seerr link", true, &domain.User{Role: domain.RoleAdmin}, false, oas.SeerrLinkInput{}, nil},
		{"negative delete no user", true, nil, false, oas.SeerrLinkInput{}, domain.ErrUnauthorized},
		{"negative delete viewer", true, &domain.User{Role: domain.RoleViewer}, false, oas.SeerrLinkInput{}, domain.ErrForbidden},
		{"negative delete unknown", true, &domain.User{Role: domain.RoleAdmin}, true, oas.SeerrLinkInput{}, domain.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			link := seedTestSeerrLink(t, f)
			targetID := link.ID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			testSeerrUpdateOrDelete(t, f, ctx, tc.isDelete, targetID, link, tc.in, tc.wantErr)
		})
	}
}

func testSeerrUpdateOrDelete(t *testing.T, f *integrationFixture, ctx context.Context, isDelete bool, targetID uuid.UUID, orig domain.SeerrLink, in oas.SeerrLinkInput, wantErr error) {
	t.Helper()
	if isDelete {
		err := f.h.DeleteSeerrLink(ctx, oas.DeleteSeerrLinkParams{LinkId: targetID})
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("DeleteSeerrLink() err = %v, want %v", err, wantErr)
			}
			if _, getErr := f.h.GetSeerrLink(ctx, oas.GetSeerrLinkParams{LinkId: orig.ID}); getErr != nil {
				t.Fatalf("after refused delete: link missing %v", getErr)
			}
			return
		}
		if err != nil {
			t.Fatalf("DeleteSeerrLink() = %v", err)
		}
		if _, getErr := f.h.GetSeerrLink(ctx, oas.GetSeerrLinkParams{LinkId: orig.ID}); !errors.Is(getErr, domain.ErrNotFound) {
			t.Fatalf("after delete: GetSeerrLink() = %v, want not found", getErr)
		}
		return
	}
	got, err := f.h.UpdateSeerrLink(ctx, &in, oas.UpdateSeerrLinkParams{LinkId: targetID})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("UpdateSeerrLink() err = %v, want %v", err, wantErr)
		}
		cur, getErr := f.h.GetSeerrLink(ctx, oas.GetSeerrLinkParams{LinkId: orig.ID})
		if getErr != nil || cur.Name != orig.Name {
			t.Fatalf("after refused update: name = %s, want %s", cur.Name, orig.Name)
		}
		return
	}
	if err != nil || got.Name != in.Name {
		t.Fatalf("UpdateSeerrLink() = %+v, %v; want name %s", got, err, in.Name)
	}
}

func TestSeerrLinkProbe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		isInput    bool
		user       *domain.User
		useUnknown bool
		in         oas.SeerrLinkInput
		probeErr   error
		wantErr    error
	}{
		{"positive test seerr link", false, nil, false, oas.SeerrLinkInput{}, nil, nil},
		{"negative test seerr link unknown", false, nil, true, oas.SeerrLinkInput{}, nil, domain.ErrNotFound},
		{"boundary test seerr link unreachable", false, nil, false, oas.SeerrLinkInput{}, domain.ErrUnreachable, domain.ErrUnreachable},
		{"positive test seerr link input", true, &domain.User{Role: domain.RoleAdmin}, false, validSeerrLinkInput(), nil, nil},
		{"negative test seerr link input no user", true, nil, false, validSeerrLinkInput(), nil, domain.ErrUnauthorized},
		{"negative test seerr link input viewer", true, &domain.User{Role: domain.RoleViewer}, false, validSeerrLinkInput(), nil, domain.ErrForbidden},
		{"boundary test seerr link input invalid", true, &domain.User{Role: domain.RoleAdmin}, false, oas.SeerrLinkInput{BaseURL: "http://seerr.invalid"}, nil, domain.ErrInvalid},
		{"boundary test seerr link input unreachable", true, &domain.User{Role: domain.RoleAdmin}, false, validSeerrLinkInput(), domain.ErrUnreachable, domain.ErrUnreachable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			f.seerrClient.status = seerrclient.Status{Version: "2.5.0"}
			f.seerrClient.statusErr = tc.probeErr
			linkID := uuid.New()
			if !tc.isInput && !tc.useUnknown {
				link := seedTestSeerrLink(t, f)
				linkID = link.ID
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			testSeerrProbeCase(t, f, ctx, tc.isInput, linkID, tc.in, tc.wantErr)
		})
	}
}

func testSeerrProbeCase(t *testing.T, f *integrationFixture, ctx context.Context, isInput bool, linkID uuid.UUID, in oas.SeerrLinkInput, wantErr error) {
	t.Helper()
	if isInput {
		got, err := f.h.TestSeerrLinkInput(ctx, &in)
		if wantErr != nil {
			if !errors.Is(err, wantErr) {
				t.Fatalf("TestSeerrLinkInput() err = %v, want %v", err, wantErr)
			}
			return
		}
		if err != nil || got.AppName != "Seerr" || got.Version != "2.5.0" {
			t.Fatalf("TestSeerrLinkInput() = %+v, %v; want Seerr 2.5.0", got, err)
		}
		return
	}
	got, err := f.h.TestSeerrLink(ctx, oas.TestSeerrLinkParams{LinkId: linkID})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("TestSeerrLink() err = %v, want %v", err, wantErr)
		}
		return
	}
	if err != nil || got.AppName != "Seerr" || got.Version != "2.5.0" {
		t.Fatalf("TestSeerrLink() = %+v, %v; want Seerr 2.5.0", got, err)
	}
}

func TestSeerrSync(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		user       *domain.User
		useUnknown bool
		syncErr    error
		wantErr    error
	}{
		{"positive sync seerr link", &domain.User{Role: domain.RoleAdmin}, false, nil, nil},
		{"negative sync no user", nil, false, nil, domain.ErrUnauthorized},
		{"negative sync viewer", &domain.User{Role: domain.RoleViewer}, false, nil, domain.ErrForbidden},
		{"negative sync unknown", &domain.User{Role: domain.RoleAdmin}, true, nil, domain.ErrNotFound},
		{"boundary sync unreachable", &domain.User{Role: domain.RoleAdmin}, false, domain.ErrUnreachable, domain.ErrUnreachable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			link := seedTestSeerrLink(t, f)
			targetID := link.ID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			if tc.syncErr != nil {
				f.seerrClient.reqsErr = tc.syncErr
			} else {
				f.seerrClient.reqs = []seerrclient.Request{
					{ID: 101, Type: "movie", Status: 2},
				}
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			res, err := f.h.SyncSeerrLink(ctx, oas.SyncSeerrLinkParams{LinkId: targetID})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("SyncSeerrLink() err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || res.Seen != 1 {
				t.Fatalf("SyncSeerrLink() = %+v, %v; want seen=1", res, err)
			}
		})
	}
}

func TestSeerrImport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		user       *domain.User
		useUnknown bool
		duplicate  bool
		wantErr    error
	}{
		{"positive import seerr instances", &domain.User{Role: domain.RoleAdmin}, false, false, nil},
		{"negative import no user", nil, false, false, domain.ErrUnauthorized},
		{"negative import viewer", &domain.User{Role: domain.RoleViewer}, false, false, domain.ErrForbidden},
		{"negative import unknown", &domain.User{Role: domain.RoleAdmin}, true, false, domain.ErrNotFound},
		{"boundary import duplicate skipped", &domain.User{Role: domain.RoleAdmin}, false, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			link := seedTestSeerrLink(t, f)
			targetID := link.ID
			if tc.useUnknown {
				targetID = uuid.New()
			}
			serverBase := "http://" + uuid.NewString() + ".invalid:8989"
			f.seerrClient.servers = []seerrclient.Server{
				{Kind: domain.KindSonarr, Name: "ImportedTV", BaseURL: serverBase, APIKey: "imported-key-0001"},
			}
			if tc.duplicate {
				_, err := f.h.instances.Create(t.Context(), instances.Input{
					Kind: domain.KindSonarr, Name: "PreExisting", BaseURL: serverBase, APIKey: "imported-key-0001", Enabled: true,
				})
				if err != nil {
					t.Fatalf("instances.Create: %v", err)
				}
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			res, err := f.h.ImportSeerrInstances(ctx, oas.ImportSeerrInstancesParams{LinkId: targetID})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ImportSeerrInstances() err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ImportSeerrInstances() err = %v", err)
			}
			if tc.duplicate {
				if len(res.Created) != 0 || len(res.Skipped) != 1 {
					t.Fatalf("ImportSeerrInstances() duplicate = %+v; want 1 skipped", res)
				}
				return
			}
			if len(res.Created) != 1 || res.Created[0] != "Seerr: ImportedTV" {
				t.Fatalf("ImportSeerrInstances() = %+v; want 1 created", res)
			}
		})
	}
}

func TestSeerrRequests(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		isSnatch   bool
		user       *domain.User
		seedReq    bool
		resolved   bool
		unknownReq bool
		wantErr    error
	}{
		{"positive list seerr requests", false, nil, true, true, false, nil},
		{"boundary list seerr requests empty", false, nil, false, false, false, nil},
		{"positive snatch seerr request", true, &domain.User{Role: domain.RoleAdmin}, true, true, false, nil},
		{"negative snatch seerr request no user", true, nil, true, true, false, domain.ErrUnauthorized},
		{"negative snatch seerr request viewer", true, &domain.User{Role: domain.RoleViewer}, true, true, false, domain.ErrForbidden},
		{"negative snatch seerr request unknown", true, &domain.User{Role: domain.RoleAdmin}, true, true, true, domain.ErrNotFound},
		{"boundary snatch seerr request unresolved", true, &domain.User{Role: domain.RoleAdmin}, true, false, false, domain.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			link := seedTestSeerrLink(t, f)
			instID := seedTestInstance(t, f)
			reqID := 0
			if tc.seedReq {
				var entPtr *int64
				var instPtr *uuid.UUID
				if tc.resolved {
					val := int64(100)
					entPtr = &val
					instPtr = &instID
				}
				reqID = seedTestSeerrRequest(t, f, link.ID, instPtr, entPtr)
			}
			if tc.unknownReq {
				reqID = 999999
			}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			testSeerrRequestCase(t, f, ctx, tc.isSnatch, link.ID, instID, reqID, tc.wantErr)
		})
	}
}

func testSeerrRequestCase(t *testing.T, f *integrationFixture, ctx context.Context, isSnatch bool, linkID, instID uuid.UUID, reqID int, wantErr error) {
	t.Helper()
	if !isSnatch {
		list, err := f.h.ListSeerrRequests(ctx)
		if err != nil {
			t.Fatalf("ListSeerrRequests() = %v", err)
		}
		if reqID > 0 {
			if len(list) != 1 || list[0].RequestID != reqID || list[0].Title == "" {
				t.Fatalf("ListSeerrRequests() = %+v, want reqID %d", list, reqID)
			}
		} else if len(list) != 0 {
			t.Fatalf("ListSeerrRequests() len = %d, want 0", len(list))
		}
		return
	}
	run, err := f.h.SnatchSeerrRequest(ctx, oas.SnatchSeerrRequestParams{LinkId: linkID, RequestId: reqID})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("SnatchSeerrRequest() err = %v, want %v", err, wantErr)
		}
		return
	}
	if err != nil || run.InstanceID != instID || run.Kind != oas.SnatchKindMissing {
		t.Fatalf("SnatchSeerrRequest() = %+v, %v; want inst %s", run, err, instID)
	}
}
