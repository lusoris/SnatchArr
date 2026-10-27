// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// stubProber answers every probe with one result and counts the probes.
type stubProber struct {
	res        arrclient.Result
	err        error
	asked      int
	queueCalls int
	imports    int
	items      []queue.Item
}

func (p *stubProber) Probe(context.Context, domain.AppKind, string, string) (arrclient.Result, error) {
	p.asked++
	return p.res, p.err
}

func (p *stubProber) Queue(context.Context, domain.AppKind, string, string, int, int) ([]queue.Item, error) {
	p.queueCalls++
	if p.err != nil {
		return nil, p.err
	}
	return p.items, nil
}

func (p *stubProber) RemoveQueueItem(context.Context, domain.AppKind, string, string, int32, bool, bool) error {
	return p.err
}

func (p *stubProber) ForceImport(context.Context, domain.AppKind, string, string, string, queue.ImportMode) (int, error) {
	if p.err != nil {
		return 0, p.err
	}
	p.imports++
	return 1, nil
}

// TestTestInstanceInput covers the probe of credentials that are not stored. That path asks
// the prober and nothing else, so the service needs no store here.
func TestTestInstanceInput(t *testing.T) {
	t.Parallel()
	admin := &domain.User{Role: domain.RoleAdmin}
	bookshelf := arrclient.Result{AppName: "Readarr", Version: "0.4.21.182", Product: domain.ProductBookshelf}
	sonarr := arrclient.Result{AppName: "Sonarr", Version: "4.0.10.2544"}
	valid := oas.InstanceInput{Kind: oas.AppKindReadarr, Name: "books", BaseURL: "http://books:8787", APIKey: "a-key-for-the-stub"}
	noURL := valid
	noURL.BaseURL = ""
	tests := []struct {
		name      string
		user      *domain.User
		in        oas.InstanceInput
		probe     stubProber
		want      oas.ProbeResult
		wantErr   error
		wantAsked int
	}{
		// Positive.
		{
			name: "a fork is named", user: admin, in: valid, probe: stubProber{res: bookshelf}, wantAsked: 1,
			want: oas.ProbeResult{AppName: "Readarr", Version: "0.4.21.182", Product: oas.NewOptArrProduct(oas.ArrProductBookshelf)},
		},
		{
			name: "a kind that one product serves carries no product", user: admin, in: valid, probe: stubProber{res: sonarr}, wantAsked: 1,
			want: oas.ProbeResult{AppName: "Sonarr", Version: "4.0.10.2544"},
		},
		// Negative.
		{name: "nobody is signed in", in: valid, probe: stubProber{res: bookshelf}, wantErr: domain.ErrUnauthorized},
		{name: "a viewer asks", user: &domain.User{Role: domain.RoleViewer}, in: valid, probe: stubProber{res: bookshelf}, wantErr: domain.ErrForbidden},
		{name: "the instance does not answer", user: admin, in: valid, probe: stubProber{err: domain.ErrUnreachable}, wantErr: domain.ErrUnreachable, wantAsked: 1},
		// Boundary: an input that fails validation never reaches the instance.
		{name: "no base URL", user: admin, in: noURL, probe: stubProber{res: bookshelf}, wantErr: domain.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &Handlers{instances: instances.New(nil, nil, nil, nil, &tc.probe, slog.New(slog.DiscardHandler))}
			ctx := t.Context()
			if tc.user != nil {
				ctx = withUser(ctx, *tc.user)
			}
			got, err := h.TestInstanceInput(ctx, &tc.in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("TestInstanceInput() error = %v, want %v", err, tc.wantErr)
			}
			if tc.probe.asked != tc.wantAsked {
				t.Errorf("the prober was asked %d times, want %d", tc.probe.asked, tc.wantAsked)
			}
			if tc.wantErr != nil {
				return
			}
			if *got != tc.want {
				t.Errorf("TestInstanceInput() = %+v, want %+v", *got, tc.want)
			}
		})
	}
}
