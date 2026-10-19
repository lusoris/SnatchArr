// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package policyoas_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/policyoas"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

func TestPolicyRoundTrip(t *testing.T) {
	t.Parallel()
	instID := uuid.New()
	in := domain.Policy{
		InstanceID: instID, MissingPerCycle: 2, UpgradePerCycle: 1, CycleInterval: 15 * time.Minute, HourlyCap: 30,
		Selection: domain.SelectionSequential, MonitoredOnly: true, SkipFutureReleases: false, RadarrReleaseType: "digital",
		SonarrMissingMode: "shows", SonarrUpgradeMode: "season_packs", LidarrMissingMode: "album", ProcessedTTL: 48 * time.Hour,
		MaxQueueSize: 5, AwaitCommand: true, PageSize: 200, UpdatedAt: time.Unix(1_700_000_000, 0).UTC(),
		IncludeTags: []int32{1, 2}, ExcludeTags: []int32{3}, IncludeQualityProfiles: []int32{4},
		ExcludeQualityProfiles: []int32{}, ReleaseDelay: 36 * time.Hour,
	}
	out := policyoas.PolicyFromOAS(instID, policyoas.PolicyToOAS(in))
	in.UpdatedAt = time.Time{}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip changed the policy:\n got %+v\nwant %+v", out, in)
	}
	if got := policyoas.PolicyToOAS(in); got.CycleIntervalS != 900 || got.ProcessedTTLH != 48 || got.Selection != oas.SnatchPolicySelectionSequential {
		t.Fatalf("policyToOAS: %+v", got)
	}
	// Boundary: a request without the filter fields means no filter, never null lists.
	bare := policyoas.PolicyToOAS(in)
	bare.IncludeTags, bare.ExcludeTags, bare.IncludeQualityProfiles, bare.ExcludeQualityProfiles = nil, nil, nil, nil
	bare.ReleaseDelayH = oas.OptInt{}
	bare.SkipUnavailableMovies = oas.OptBool{}
	got := policyoas.PolicyFromOAS(instID, bare)
	if got.IncludeTags == nil || len(got.ExcludeQualityProfiles) != 0 || got.ReleaseDelay != 0 {
		t.Fatalf("policyFromOAS without filters: %+v", got)
	}
	// Boundary: a client that predates skip_unavailable_movies keeps the default, true.
	if !got.SkipUnavailableMovies {
		t.Fatal("policyFromOAS without skip_unavailable_movies must skip unavailable movies")
	}
	// Boundary: a policy without lists shows empty lists, not absent ones.
	if got := policyoas.PolicyToOAS(domain.Policy{}); got.IncludeTags == nil || got.ExcludeQualityProfiles == nil {
		t.Fatalf("policyToOAS of empty lists: %+v", got)
	}
}

func TestQueuePolicyRoundTrip(t *testing.T) {
	t.Parallel()
	blocklist := true
	minCFS := int32(15)
	in := queue.Settings{
		Enabled: true,
		Rules: map[queue.Rule]queue.RuleSettings{
			queue.RuleStalled:             {Mode: queue.ModeObserve, Threshold: 5, Blocklist: &blocklist},
			queue.RuleSlow:                {Mode: queue.ModeAct, Threshold: 3, Blocklist: &blocklist},
			queue.RuleFailed:              {Mode: queue.ModeAct, Threshold: 2, Blocklist: &blocklist},
			queue.RuleImportBlocked:       {Mode: queue.ModeObserve, Threshold: 6, Blocklist: &blocklist},
			queue.RuleUnwantedScore:       {Mode: queue.ModeAct, Threshold: 1, Blocklist: &blocklist},
			queue.RuleUnwantedUnmonitored: {Mode: queue.ModeObserve, Threshold: 1, Blocklist: &blocklist},
			queue.RuleNotAnUpgrade:        {Mode: queue.ModeObserve, Threshold: 4, Blocklist: &blocklist},
			queue.RuleDuplicate:           {Mode: queue.ModeAct, Threshold: 1, Blocklist: &blocklist},
			queue.RuleIDMatched:           {Mode: queue.ModeAct, Threshold: 1, Blocklist: &blocklist},
		},
		Grace:                30 * time.Minute,
		MinBytesPerSecond:    1024,
		MinCustomFormatScore: &minCFS,
		MaxRemovalsPerSweep:  12,
		MaxImportsPerSweep:   20,
	}

	oasReq := policyoas.QueuePolicyToOAS(in)
	out := policyoas.QueuePolicyFromOAS(oasReq)

	if out.Enabled != in.Enabled || out.Grace != in.Grace || out.MinBytesPerSecond != in.MinBytesPerSecond ||
		*out.MinCustomFormatScore != *in.MinCustomFormatScore || out.MaxRemovalsPerSweep != in.MaxRemovalsPerSweep ||
		out.MaxImportsPerSweep != in.MaxImportsPerSweep {
		t.Fatalf("QueuePolicy roundtrip mismatch: got %+v, want %+v", out, in)
	}
	for r, rs := range in.Rules {
		gotRS, ok := out.Rules[r]
		if !ok || gotRS.Mode != rs.Mode || gotRS.Threshold != rs.Threshold {
			t.Fatalf("Rule %v mismatch: got %+v, want %+v", r, gotRS, rs)
		}
	}
}
