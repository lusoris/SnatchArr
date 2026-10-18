// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

func validQueuePolicyOAS() *oas.QueuePolicy {
	rule := oas.QueueRuleSettings{Mode: oas.QueueRuleModeObserve, Threshold: 5}
	return &oas.QueuePolicy{
		Enabled: true,
		Rules: oas.QueueRules{
			Stalled:             rule,
			Slow:                rule,
			Failed:              rule,
			ImportBlocked:       rule,
			UnwantedScore:       rule,
			UnwantedUnmonitored: rule,
		},
		GraceSeconds:        900,
		MinBytesPerSecond:   1024,
		MaxRemovalsPerSweep: 10,
	}
}

func TestGetQueuePolicy(t *testing.T) {
	t.Parallel()
	t.Run("positive returns defaults for seeded instance", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		instID := seedTestInstance(t, f)
		got, err := f.h.GetQueuePolicy(t.Context(), oas.GetQueuePolicyParams{InstanceId: instID})
		if err != nil {
			t.Fatalf("GetQueuePolicy() err = %v", err)
		}
		if got.Enabled {
			t.Errorf("GetQueuePolicy() enabled = true, want false by default")
		}
		if got.GraceSeconds != 900 {
			t.Errorf("GetQueuePolicy() grace = %d, want 900", got.GraceSeconds)
		}
		if v, ok := got.Rules.Failed.Blocklist.Get(); !ok || !v {
			t.Errorf("GetQueuePolicy() failed blocklist = %v (set %v), want true", v, ok)
		}
		if v, ok := got.Rules.UnwantedUnmonitored.Blocklist.Get(); !ok || v {
			t.Errorf("GetQueuePolicy() unwanted_unmonitored blocklist = %v (set %v), want false", v, ok)
		}
		nau, ok := got.Rules.NotAnUpgrade.Get()
		if !ok || nau.Mode != oas.QueueRuleModeObserve || nau.Threshold != 1 || nau.Blocklist.Or(true) {
			t.Errorf("GetQueuePolicy() not_an_upgrade = %+v (set %v), want observe, 1, no blocklist", nau, ok)
		}
	})

	t.Run("negative unknown instance answers not found", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		_, err := f.h.GetQueuePolicy(t.Context(), oas.GetQueuePolicyParams{InstanceId: uuid.New()})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("GetQueuePolicy() err = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func TestUpdateQueuePolicyTableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		user      func(f *integrationFixture) (domain.User, bool)
		unknownID bool
		mutate    func(p *oas.QueuePolicy)
		wantErr   error
	}{
		// Positive.
		{
			name: "positive admin saves valid policy",
			user: func(f *integrationFixture) (domain.User, bool) { return f.admin, true },
		},
		// Negative.
		{
			name:    "negative non-admin viewer receives forbidden",
			user:    func(f *integrationFixture) (domain.User, bool) { return f.viewer, true },
			wantErr: domain.ErrForbidden,
		},
		{
			name:    "negative unauthenticated caller receives unauthorized",
			user:    func(*integrationFixture) (domain.User, bool) { return domain.User{}, false },
			wantErr: domain.ErrUnauthorized,
		},
		{
			name:      "negative unknown instance receives not found",
			user:      func(f *integrationFixture) (domain.User, bool) { return f.admin, true },
			unknownID: true,
			wantErr:   domain.ErrNotFound,
		},
		{
			name: "negative invalid threshold 0",
			user: func(f *integrationFixture) (domain.User, bool) { return f.admin, true },
			mutate: func(p *oas.QueuePolicy) {
				p.Rules.Stalled.Threshold = 0
			},
			wantErr: queue.ErrInvalid,
		},
		{
			name: "negative invalid grace 86401 seconds",
			user: func(f *integrationFixture) (domain.User, bool) { return f.admin, true },
			mutate: func(p *oas.QueuePolicy) {
				p.GraceSeconds = 86401
			},
			wantErr: queue.ErrInvalid,
		},
		// Boundary.
		{
			name: "boundary threshold 1 and 100 are accepted",
			user: func(f *integrationFixture) (domain.User, bool) { return f.admin, true },
			mutate: func(p *oas.QueuePolicy) {
				p.Rules.Stalled.Threshold = 1
				p.Rules.Slow.Threshold = 100
			},
		},
		{
			name: "boundary grace 0 and 86400 are accepted",
			user: func(f *integrationFixture) (domain.User, bool) { return f.admin, true },
			mutate: func(p *oas.QueuePolicy) {
				p.GraceSeconds = 0
			},
		},
		{
			name: "boundary grace 86400 is accepted",
			user: func(f *integrationFixture) (domain.User, bool) { return f.admin, true },
			mutate: func(p *oas.QueuePolicy) {
				p.GraceSeconds = 86400
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newIntegrationFixture(t)
			instID := seedTestInstance(t, f)
			if tc.unknownID {
				instID = uuid.New()
			}
			ctx := t.Context()
			if u, ok := tc.user(f); ok {
				ctx = withUser(ctx, u)
			}
			req := validQueuePolicyOAS()
			if tc.mutate != nil {
				tc.mutate(req)
			}
			got, err := f.h.UpdateQueuePolicy(ctx, req, oas.UpdateQueuePolicyParams{InstanceId: instID})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("UpdateQueuePolicy() err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateQueuePolicy() unexpected err = %v", err)
			}
			if got.GraceSeconds != req.GraceSeconds {
				t.Errorf("UpdateQueuePolicy() grace = %d, want %d", got.GraceSeconds, req.GraceSeconds)
			}
		})
	}
}

func TestListQueueStrikes(t *testing.T) {
	t.Parallel()
	t.Run("positive empty list for new instance", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		instID := seedTestInstance(t, f)
		got, err := f.h.ListQueueStrikes(t.Context(), oas.ListQueueStrikesParams{InstanceId: instID})
		if err != nil {
			t.Fatalf("ListQueueStrikes() err = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("ListQueueStrikes() len = %d, want 0", len(got))
		}
	})

	t.Run("positive strikes mapped with threshold from settings", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		instID := seedTestInstance(t, f)

		// Seed a strike record via queue store.
		decisions := []queue.Decision{
			{
				ItemID:     42,
				DownloadID: "dl-test-01",
				Rule:       queue.RuleStalled,
				Next: queue.Strike{
					Rule:         queue.RuleStalled,
					Count:        3,
					LastSizeLeft: 1048576,
					LastSeen:     f.clk.Now().Add(-5 * time.Minute),
				},
			},
		}
		if err := f.h.queue.Record(t.Context(), instID, decisions); err != nil {
			t.Fatalf("queue.Record() err = %v", err)
		}

		got, err := f.h.ListQueueStrikes(t.Context(), oas.ListQueueStrikesParams{InstanceId: instID})
		if err != nil {
			t.Fatalf("ListQueueStrikes() err = %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("ListQueueStrikes() len = %d, want 1", len(got))
		}
		s := got[0]
		if s.DownloadID != "dl-test-01" || s.QueueID != 42 || s.Count != 3 {
			t.Errorf("ListQueueStrikes() got %+v, want dl-test-01, queueID 42, count 3", s)
		}
		// Default stalled threshold is 6
		if s.Threshold != 6 {
			t.Errorf("ListQueueStrikes() threshold = %d, want 6", s.Threshold)
		}
		if s.Rule != oas.QueueRuleStalled {
			t.Errorf("ListQueueStrikes() rule = %q, want %q", s.Rule, oas.QueueRuleStalled)
		}
	})

	t.Run("negative unknown instance answers not found", func(t *testing.T) {
		t.Parallel()
		f := newIntegrationFixture(t)
		_, err := f.h.ListQueueStrikes(t.Context(), oas.ListQueueStrikesParams{InstanceId: uuid.New()})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("ListQueueStrikes() err = %v, want %v", err, domain.ErrNotFound)
		}
	})
}

func TestQueueRuleBlocklistMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   oas.OptBool
		want *bool
	}{
		{"positive/true is kept", oas.NewOptBool(true), new(true)},
		{"negative/false is kept, not dropped as zero", oas.NewOptBool(false), new(false)},
		{"boundary/left out stays unset", oas.OptBool{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rs := queueRuleFromOAS(oas.QueueRuleSettings{Mode: oas.QueueRuleModeAct, Threshold: 3, Blocklist: tt.in})
			if (rs.Blocklist == nil) != (tt.want == nil) || (rs.Blocklist != nil && *rs.Blocklist != *tt.want) {
				t.Fatalf("queueRuleFromOAS() blocklist = %v, want %v", rs.Blocklist, tt.want)
			}
			back := queueRuleToOAS(queue.ResolveRule(queue.Settings{Rules: map[queue.Rule]queue.RuleSettings{queue.RuleUnwantedUnmonitored: rs}}, queue.RuleUnwantedUnmonitored))
			want := tt.want != nil && *tt.want // unset resolves to the unmonitored default, false
			if v, ok := back.Blocklist.Get(); !ok || v != want {
				t.Errorf("queueRuleToOAS() blocklist = %v (set %v), want %v", v, ok, want)
			}
		})
	}
}

func TestQueueNotAnUpgradeMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  *oas.QueueRuleSettings
		want queue.RuleSettings
	}{
		{
			name: "positive stored settings survive the write",
			set:  &oas.QueueRuleSettings{Mode: oas.QueueRuleModeAct, Threshold: 3},
			want: queue.RuleSettings{Mode: queue.ModeAct, Threshold: 3, Blocklist: new(false)},
		},
		{
			name: "negative left out of the write takes the default",
			want: queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 1, Blocklist: new(false)},
		},
		{
			name: "boundary threshold one with blocklist on",
			set:  &oas.QueueRuleSettings{Mode: oas.QueueRuleModeObserve, Threshold: 1, Blocklist: oas.NewOptBool(true)},
			want: queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 1, Blocklist: new(true)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := validQueuePolicyOAS()
			if tt.set != nil {
				req.Rules.NotAnUpgrade.SetTo(*tt.set)
			}
			s := queuePolicyFromOAS(req)
			got := queue.ResolveRule(s, queue.RuleNotAnUpgrade)
			if got.Mode != tt.want.Mode || got.Threshold != tt.want.Threshold || *got.Blocklist != *tt.want.Blocklist {
				t.Fatalf("ResolveRule(not_an_upgrade) = %+v blocklist %v, want %+v blocklist %v", got, *got.Blocklist, tt.want, *tt.want.Blocklist)
			}
			back, ok := queuePolicyToOAS(s).Rules.NotAnUpgrade.Get()
			if !ok || back.Threshold != tt.want.Threshold || back.Blocklist.Or(!*tt.want.Blocklist) != *tt.want.Blocklist {
				t.Fatalf("queuePolicyToOAS() not_an_upgrade = %+v (set %v), want threshold %d", back, ok, tt.want.Threshold)
			}
		})
	}
}
