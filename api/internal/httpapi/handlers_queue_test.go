// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"errors"
	"net/http"
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
		for _, opt := range optionalRules {
			rs, ok := opt.field(&got.Rules).Get()
			if !ok || rs.Mode != oas.QueueRuleModeObserve || rs.Threshold != 1 || rs.Blocklist.Or(true) {
				t.Errorf("GetQueuePolicy() %s = %+v (set %v), want observe, 1, no blocklist", opt.rule, rs, ok)
			}
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

func TestListQueueItems(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		unknownID  bool
		arrErr     error
		seedItems  bool
		wantStatus int
		wantErr    error
		wantCount  int
		wantTrunc  bool
	}{
		{
			name:       "positive live queue items returned",
			seedItems:  true,
			wantStatus: http.StatusOK,
			wantCount:  1,
			wantTrunc:  false,
		},
		{
			name:       "negative unknown instance answers not found",
			unknownID:  true,
			wantStatus: http.StatusNotFound,
			wantErr:    domain.ErrNotFound,
		},
		{
			name:       "negative arr unreachable answers bad gateway",
			arrErr:     domain.ErrUnreachable,
			wantStatus: http.StatusBadGateway,
			wantErr:    domain.ErrUnreachable,
		},
		{
			name:       "boundary empty queue",
			seedItems:  false,
			wantStatus: http.StatusOK,
			wantCount:  0,
			wantTrunc:  false,
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
			if tc.arrErr != nil {
				f.probe.err = tc.arrErr
			}
			if tc.seedItems {
				f.probe.items = []queue.Item{
					{
						ID:                    42,
						DownloadID:            "dl-01",
						Title:                 "Test Item",
						Status:                "downloading",
						TrackedDownloadStatus: "ok",
						TrackedDownloadState:  "downloading",
						SizeLeft:              1000,
						Added:                 f.clk.Now().Add(-5 * time.Minute),
					},
				}
			}
			got, err := f.h.ListQueueItems(t.Context(), oas.ListQueueItemsParams{InstanceId: instID})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ListQueueItems() err = %v, want %v", err, tc.wantErr)
				}
				prob := f.h.NewError(t.Context(), err)
				if prob.StatusCode != tc.wantStatus {
					t.Errorf("NewError status = %d, want %d", prob.StatusCode, tc.wantStatus)
				}
				return
			}
			if err != nil {
				t.Fatalf("ListQueueItems() err = %v", err)
			}
			if len(got.Items) != tc.wantCount {
				t.Errorf("len(Items) = %d, want %d", len(got.Items), tc.wantCount)
			}
			if got.Truncated != tc.wantTrunc {
				t.Errorf("Truncated = %v, want %v", got.Truncated, tc.wantTrunc)
			}
		})
	}
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

// optionalRules are the rules older clients may leave out of a write.
var optionalRules = []struct {
	rule  queue.Rule
	field func(*oas.QueueRules) *oas.OptQueueRuleSettings
}{
	{queue.RuleNotAnUpgrade, func(r *oas.QueueRules) *oas.OptQueueRuleSettings { return &r.NotAnUpgrade }},
	{queue.RuleDuplicate, func(r *oas.QueueRules) *oas.OptQueueRuleSettings { return &r.Duplicate }},
	{queue.RuleIDMatched, func(r *oas.QueueRules) *oas.OptQueueRuleSettings { return &r.IDMatched }},
}

func TestQueueMaxImportsMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   oas.OptInt
		want int
	}{
		{"positive/stored value survives the write", oas.NewOptInt(40), 40},
		{"negative/left out takes the default", oas.OptInt{}, 25},
		{"boundary/one is kept", oas.NewOptInt(1), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := validQueuePolicyOAS()
			req.MaxImportsPerSweep = tt.in
			s := queuePolicyFromOAS(req)
			if s.MaxImportsPerSweep != tt.want {
				t.Fatalf("queuePolicyFromOAS() MaxImportsPerSweep = %d, want %d", s.MaxImportsPerSweep, tt.want)
			}
			if got, ok := queuePolicyToOAS(s).MaxImportsPerSweep.Get(); !ok || got != tt.want {
				t.Fatalf("queuePolicyToOAS() max_imports_per_sweep = %d (set %v), want %d", got, ok, tt.want)
			}
		})
	}
}

func TestQueueStrikeImportMapping(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		strike      queue.Strike
		wantSet     bool
		wantRefused bool
	}{
		{"positive/import stamp is returned", queue.Strike{Rule: queue.RuleIDMatched, ImportedAt: at}, true, false},
		{"positive/refusal is returned", queue.Strike{Rule: queue.RuleIDMatched, ImportedAt: at, ImportRefused: true}, true, true},
		{"negative/no import leaves both out", queue.Strike{Rule: queue.RuleStalled}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := queueStrikeToOAS(queue.StrikeItem{DownloadID: "dl", Strike: tt.strike}, 1)
			got, ok := out.ImportedAt.Get()
			if ok != tt.wantSet || (ok && !got.Equal(at)) || out.ImportRefused.Or(false) != tt.wantRefused || out.ImportRefused.IsSet() != tt.wantSet {
				t.Fatalf("queueStrikeToOAS() imported_at = %v (set %v) refused = %+v, want set %v refused %v", got, ok, out.ImportRefused, tt.wantSet, tt.wantRefused)
			}
		})
	}
}

func TestQueueOptionalRuleMapping(t *testing.T) {
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
	for _, opt := range optionalRules {
		for _, tt := range tests {
			t.Run(string(opt.rule)+" "+tt.name, func(t *testing.T) {
				t.Parallel()
				req := validQueuePolicyOAS()
				if tt.set != nil {
					opt.field(&req.Rules).SetTo(*tt.set)
				}
				s := queuePolicyFromOAS(req)
				got := queue.ResolveRule(s, opt.rule)
				if got.Mode != tt.want.Mode || got.Threshold != tt.want.Threshold || *got.Blocklist != *tt.want.Blocklist {
					t.Fatalf("ResolveRule(%s) = %+v blocklist %v, want %+v blocklist %v", opt.rule, got, *got.Blocklist, tt.want, *tt.want.Blocklist)
				}
				back, ok := opt.field(&queuePolicyToOAS(s).Rules).Get()
				if !ok || back.Threshold != tt.want.Threshold || back.Blocklist.Or(!*tt.want.Blocklist) != *tt.want.Blocklist {
					t.Fatalf("queuePolicyToOAS() %s = %+v (set %v), want threshold %d", opt.rule, back, ok, tt.want.Threshold)
				}
			})
		}
	}
}

type sweepQueueCase struct {
	name           string
	user           func(f *integrationFixture) (domain.User, bool)
	unknownID      bool
	enablePolicy   bool
	arrErr         error
	wantErr        error
	wantStatus     int
	wantQueueCalls int
	wantImports    int
}

func seedImportBlockedQueue(t *testing.T, f *integrationFixture, instID uuid.UUID, enabled bool) {
	t.Helper()
	f.probe.items = []queue.Item{
		{
			ID:                    841,
			DownloadID:            "dl-whisparr-841",
			Title:                 "Example.Scene",
			Status:                "completed",
			TrackedDownloadStatus: "warning",
			TrackedDownloadState:  "importBlocked",
			Added:                 f.clk.Now().Add(-10 * time.Minute),
			StatusLines:           []string{"Movie title mismatch, automatic import is not possible. Manual Import required."},
			Monitored:             true,
		},
	}
	cfg := queuePolicyFromOAS(validQueuePolicyOAS())
	cfg.Enabled = enabled
	cfg.Grace = 0
	cfg.Rules[queue.RuleImportFixable] = queue.RuleSettings{Mode: queue.ModeAct, Threshold: 1}
	if err := f.h.queue.SaveSettings(t.Context(), instID, cfg); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
}

func TestSweepInstanceQueue(t *testing.T) {
	t.Parallel()
	cases := []sweepQueueCase{
		{
			name:           "positive admin sweeps enabled queue and imports",
			user:           func(f *integrationFixture) (domain.User, bool) { return f.admin, true },
			enablePolicy:   true,
			wantQueueCalls: 1,
			wantImports:    1,
		},
		{
			name:       "negative viewer rejected with forbidden",
			user:       func(_ *integrationFixture) (domain.User, bool) { return domain.User{Role: domain.RoleViewer}, true },
			wantErr:    domain.ErrForbidden,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "negative unauthenticated rejected with unauthorized",
			user:       func(_ *integrationFixture) (domain.User, bool) { return domain.User{}, false },
			wantErr:    domain.ErrUnauthorized,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "negative unknown instance returns not found",
			user:       func(f *integrationFixture) (domain.User, bool) { return f.admin, true },
			unknownID:  true,
			wantErr:    domain.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			name:           "negative service error maps to problem response",
			user:           func(f *integrationFixture) (domain.User, bool) { return f.admin, true },
			enablePolicy:   true,
			arrErr:         domain.ErrUnreachable,
			wantErr:        domain.ErrUnreachable,
			wantStatus:     http.StatusBadGateway,
			wantQueueCalls: 1,
		},
		{
			name:           "boundary disabled policy returns no content without import",
			user:           func(f *integrationFixture) (domain.User, bool) { return f.admin, true },
			enablePolicy:   false,
			wantQueueCalls: 0,
			wantImports:    0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runSweepInstanceQueueCase(t, tc)
		})
	}
}

func runSweepInstanceQueueCase(t *testing.T, tc sweepQueueCase) {
	t.Helper()
	f := newIntegrationFixture(t)
	instID := seedTestInstance(t, f)
	seedImportBlockedQueue(t, f, instID, tc.enablePolicy)
	if tc.unknownID {
		instID = uuid.New()
	}
	f.probe.err = tc.arrErr
	ctx := t.Context()
	if u, ok := tc.user(f); ok {
		ctx = withUser(ctx, u)
	}
	err := f.h.SweepInstanceQueue(ctx, oas.SweepInstanceQueueParams{InstanceId: instID})
	if tc.wantErr != nil {
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("SweepInstanceQueue() err = %v, want %v", err, tc.wantErr)
		}
		if prob := f.h.NewError(ctx, err); prob.StatusCode != tc.wantStatus {
			t.Errorf("NewError status = %d, want %d", prob.StatusCode, tc.wantStatus)
		}
		return
	}
	if err != nil {
		t.Fatalf("SweepInstanceQueue() unexpected err = %v", err)
	}
	if f.probe.queueCalls != tc.wantQueueCalls || f.probe.imports != tc.wantImports {
		t.Errorf("probe queueCalls=%d imports=%d, want queueCalls=%d imports=%d", f.probe.queueCalls, f.probe.imports, tc.wantQueueCalls, tc.wantImports)
	}
}
