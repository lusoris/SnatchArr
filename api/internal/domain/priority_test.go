// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain_test

import (
	"errors"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestPriorityPresetValidate(t *testing.T) {
	t.Parallel()
	withMut := func(fn func(domain.PriorityPreset)) domain.PriorityPreset {
		p := domain.MediaFirstPreset()
		fn(p)
		return p
	}
	cases := []struct {
		name    string
		preset  domain.PriorityPreset
		wantErr error
	}{
		// Positive.
		{
			name:   "positive shipped media_first preset",
			preset: domain.MediaFirstPreset(),
		},
		// Boundary.
		{
			name: "boundary rank 1 and 100 accepted",
			preset: withMut(func(p domain.PriorityPreset) {
				p[domain.KindSonarr] = 1
				p[domain.KindWhisparrV3] = 100
			}),
		},
		// Negative.
		{
			name: "negative missing kind refused",
			preset: withMut(func(p domain.PriorityPreset) {
				delete(p, domain.KindLidarr)
			}),
			wantErr: domain.ErrInvalid,
		},
		{
			name: "negative unknown kind refused",
			preset: withMut(func(p domain.PriorityPreset) {
				p[domain.AppKind("bazarr")] = 50
			}),
			wantErr: domain.ErrInvalid,
		},
		{
			name: "negative stash kind refused in preset",
			preset: withMut(func(p domain.PriorityPreset) {
				p[domain.KindStash] = 50
			}),
			wantErr: domain.ErrInvalid,
		},
		{
			name: "negative rank 0 refused",
			preset: withMut(func(p domain.PriorityPreset) {
				p[domain.KindSonarr] = 0
			}),
			wantErr: domain.ErrInvalid,
		},
		{
			name: "negative rank 101 refused",
			preset: withMut(func(p domain.PriorityPreset) {
				p[domain.KindRadarr] = 101
			}),
			wantErr: domain.ErrInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.preset.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestResolvePriorityRank(t *testing.T) {
	t.Parallel()
	preset := domain.MediaFirstPreset()
	customPreset := domain.MediaFirstPreset()
	customPreset[domain.KindWhisparrV3] = 75

	cases := []struct {
		name       string
		prio       domain.InstancePriority
		kind       domain.AppKind
		preset     domain.PriorityPreset
		wantSearch int
		wantSFrom  domain.PrioritySource
		wantDown   int
		wantDFrom  domain.PrioritySource
	}{
		// Positive.
		{
			name:       "positive nil overrides use preset",
			prio:       domain.InstancePriority{},
			kind:       domain.KindSonarr,
			preset:     preset,
			wantSearch: 10,
			wantSFrom:  domain.PrioritySourcePreset,
			wantDown:   10,
			wantDFrom:  domain.PrioritySourcePreset,
		},
		{
			name:       "positive instance overrides win over preset",
			prio:       domain.InstancePriority{Search: new(5), Download: new(90)},
			kind:       domain.KindWhisparrV3,
			preset:     customPreset,
			wantSearch: 5,
			wantSFrom:  domain.PrioritySourceInstance,
			wantDown:   90,
			wantDFrom:  domain.PrioritySourceInstance,
		},
		// Boundary.
		{
			name:       "boundary rank 1 override and rank 100 override",
			prio:       domain.InstancePriority{Search: new(1), Download: new(100)},
			kind:       domain.KindReadarr,
			preset:     preset,
			wantSearch: 1,
			wantSFrom:  domain.PrioritySourceInstance,
			wantDown:   100,
			wantDFrom:  domain.PrioritySourceInstance,
		},
		// Negative / fallback.
		{
			name:       "negative nil preset falls back to shipped media_first",
			prio:       domain.InstancePriority{},
			kind:       domain.KindLidarr,
			preset:     nil,
			wantSearch: 20,
			wantSFrom:  domain.PrioritySourcePreset,
			wantDown:   20,
			wantDFrom:  domain.PrioritySourcePreset,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotS, gotSSrc := tc.prio.ResolveSearch(tc.kind, tc.preset)
			if gotS != tc.wantSearch || gotSSrc != tc.wantSFrom {
				t.Errorf("ResolveSearch() = (%d, %q), want (%d, %q)", gotS, gotSSrc, tc.wantSearch, tc.wantSFrom)
			}
			gotD, gotDSrc := tc.prio.ResolveDownload(tc.kind, tc.preset)
			if gotD != tc.wantDown || gotDSrc != tc.wantDFrom {
				t.Errorf("ResolveDownload() = (%d, %q), want (%d, %q)", gotD, gotDSrc, tc.wantDown, tc.wantDFrom)
			}
		})
	}
}

func TestInstancePriorityValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		prio    domain.InstancePriority
		wantErr error
	}{
		{name: "positive both nil", prio: domain.InstancePriority{}},
		{name: "positive valid overrides", prio: domain.InstancePriority{Search: new(15), Download: new(25)}},
		{name: "boundary 1 and 100", prio: domain.InstancePriority{Search: new(1), Download: new(100)}},
		{name: "negative search 0", prio: domain.InstancePriority{Search: new(0)}, wantErr: domain.ErrInvalid},
		{name: "negative search 101", prio: domain.InstancePriority{Search: new(101)}, wantErr: domain.ErrInvalid},
		{name: "negative download 0", prio: domain.InstancePriority{Download: new(0)}, wantErr: domain.ErrInvalid},
		{name: "negative download 101", prio: domain.InstancePriority{Download: new(101)}, wantErr: domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.prio.Validate(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
