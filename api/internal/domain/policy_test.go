// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestDefaultPolicyIsGentleAndValid(t *testing.T) {
	t.Parallel()
	p := domain.DefaultPolicy(uuid.New())
	if p.MissingPerCycle != 1 || p.UpgradePerCycle != 0 || p.HourlyCap != 20 {
		t.Fatalf("defaults drifted from newtarr's gentle baseline: %+v", p)
	}
	if p.CycleInterval != 15*time.Minute || p.ProcessedTTL != 168*time.Hour {
		t.Fatalf("interval/ttl defaults drifted: %+v", p)
	}
	if !p.MonitoredOnly || !p.SkipFutureReleases {
		t.Fatal("monitored_only and skip_future_releases must default to true")
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("default policy must validate: %v", err)
	}
}

func TestPolicyValidate(t *testing.T) {
	t.Parallel()
	base := domain.DefaultPolicy(uuid.New())
	tests := []struct {
		name    string
		mutate  func(*domain.Policy)
		wantErr bool
	}{
		{"positive untouched", func(*domain.Policy) {}, false},
		{"boundary cap 500 ok", func(p *domain.Policy) { p.HourlyCap = 500 }, false},
		{"boundary cap 501", func(p *domain.Policy) { p.HourlyCap = 501 }, true},
		{"boundary cap 0", func(p *domain.Policy) { p.HourlyCap = 0 }, true},
		{"boundary interval 60s ok", func(p *domain.Policy) { p.CycleInterval = 60 * time.Second }, false},
		{"boundary interval 59s", func(p *domain.Policy) { p.CycleInterval = 59 * time.Second }, true},
		{"boundary missing 100 ok", func(p *domain.Policy) { p.MissingPerCycle = 100 }, false},
		{"boundary missing 101", func(p *domain.Policy) { p.MissingPerCycle = 101 }, true},
		{"boundary ttl 8760h ok", func(p *domain.Policy) { p.ProcessedTTL = 8760 * time.Hour }, false},
		{"boundary ttl 8761h", func(p *domain.Policy) { p.ProcessedTTL = 8761 * time.Hour }, true},
		{"boundary queue -1 ok", func(p *domain.Policy) { p.MaxQueueSize = -1 }, false},
		{"boundary queue -2", func(p *domain.Policy) { p.MaxQueueSize = -2 }, true},
		{"negative selection", func(p *domain.Policy) { p.Selection = "chaos" }, true},
		{"negative release type", func(p *domain.Policy) { p.RadarrReleaseType = "streaming" }, true},
		{"negative sonarr upgrade shows", func(p *domain.Policy) { p.SonarrUpgradeMode = "shows" }, true},
		{"positive sonarr missing shows", func(p *domain.Policy) { p.SonarrMissingMode = "shows" }, false},
		{"negative lidarr mode", func(p *domain.Policy) { p.LidarrMissingMode = "track" }, true},
		{"boundary page size 10 ok", func(p *domain.Policy) { p.PageSize = 10 }, false},
		{"boundary page size 9", func(p *domain.Policy) { p.PageSize = 9 }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := base
			tc.mutate(&p)
			err := p.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}
