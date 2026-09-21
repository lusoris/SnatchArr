// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestPolicyRoundTrip(t *testing.T) {
	t.Parallel()
	instID := uuid.New()
	in := domain.Policy{
		InstanceID: instID, MissingPerCycle: 2, UpgradePerCycle: 1, CycleInterval: 15 * time.Minute, HourlyCap: 30,
		Selection: domain.SelectionSequential, MonitoredOnly: true, SkipFutureReleases: false, RadarrReleaseType: "digital",
		SonarrMissingMode: "shows", SonarrUpgradeMode: "season_packs", LidarrMissingMode: "album", ProcessedTTL: 48 * time.Hour,
		MaxQueueSize: 5, AwaitCommand: true, PageSize: 200, UpdatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	out := policyFromOAS(instID, policyToOAS(in))
	in.UpdatedAt = time.Time{}
	if out != in {
		t.Fatalf("round trip changed the policy:\n got %+v\nwant %+v", out, in)
	}
	if got := policyToOAS(in); got.CycleIntervalS != 900 || got.ProcessedTTLH != 48 || got.Selection != oas.HuntPolicySelectionSequential {
		t.Fatalf("policyToOAS: %+v", got)
	}
}

func TestInstanceAndInputConversions(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	inst := domain.Instance{
		ID: uuid.New(), Kind: domain.KindRadarr, Name: "movies", BaseURL: "http://radarr:7878", Enabled: true,
		Source: domain.SourceConfigarr, ConfigarrKey: "radarr.main", LastSeenVersion: "5.14", LastCheckAt: &now, LastError: "",
		CreatedAt: now, UpdatedAt: now,
	}
	got := instanceToOAS(inst)
	if got.Kind != oas.AppKindRadarr || got.BaseURL.String() != inst.BaseURL || !got.ConfigarrKey.Set || got.LastError.Set || !got.LastCheckAt.Set {
		t.Fatalf("instanceToOAS: %+v", got)
	}
	if list := instancesToOAS([]domain.Instance{inst, inst}); len(list) != 2 {
		t.Fatal("instancesToOAS length")
	}
	in := inputFromOAS(&oas.InstanceInput{Kind: oas.AppKindSonarr, Name: "tv", BaseURL: "http://s", APIKey: "k"})
	if in.Kind != domain.KindSonarr || !in.Enabled || in.APIKey != "k" {
		t.Fatalf("inputFromOAS: %+v", in)
	}
	up := updateFromOAS(&oas.InstanceUpdate{Kind: oas.AppKindSonarr, Name: "tv", BaseURL: "http://s", Enabled: oas.NewOptBool(false)})
	if up.APIKey != "" || up.Enabled {
		t.Fatalf("updateFromOAS: %+v", up)
	}
}

func TestRunAndEventConversions(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	runID := uuid.New()
	r := domain.Run{ID: runID, InstanceID: uuid.New(), Kind: domain.HuntMissing, Status: domain.RunLeased, LeasedBy: "w1", LeaseExpiresAt: &now, QueuedAt: now, StartedAt: &now, SearchedCount: 4, Error: ""}
	got := runToOAS(r)
	if got.Status != oas.RunStatusLeased || !got.LeasedBy.Set || got.Error.Set || got.FinishedAt.Set || got.SearchedCount != 4 {
		t.Fatalf("runToOAS: %+v", got)
	}
	e := domain.Event{ID: 9, RunID: &runID, InstanceID: r.InstanceID, Timestamp: now, Level: "info", Type: "search_dispatched", EntityType: "episode", EntityID: 1029, Title: "S01E01"}
	ev := eventToOAS(e)
	if !ev.RunID.Set || ev.RunID.Value != runID || ev.EntityID.Value != 1029 || ev.Level != oas.EventLevelInfo || !ev.Title.Set || ev.Detail.Set {
		t.Fatalf("eventToOAS: %+v", ev)
	}
	if noRun := eventToOAS(domain.Event{InstanceID: r.InstanceID, Level: "warn", Type: "x"}); noRun.RunID.Set || noRun.EntityID.Set {
		t.Fatalf("eventToOAS without run: %+v", noRun)
	}
}

func TestDownloadClientConversions(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0).UTC()
	instID := uuid.New()
	c := domain.DownloadClient{
		ID: uuid.New(), InstanceID: &instID, Kind: domain.ClientSABnzbd, Name: "sab", BaseURL: "http://sab:8080", Username: "",
		Enabled: true, Source: domain.ClientSourceDiscovered, RemoteID: 3, MaxActive: 4, BandwidthBudgetBPS: 5_000_000,
		LastCheckAt: &now, LastError: "boom", CreatedAt: now, UpdatedAt: now,
	}
	got := downloadClientToOAS(c)
	if got.Protocol != oas.DownloadClientProtocolUsenet || !got.InstanceID.Set || got.InstanceID.Value != instID || got.LastError.Value != "boom" || got.BandwidthBudgetBps != 5_000_000 {
		t.Fatalf("downloadClientToOAS: %+v", got)
	}
	global := c
	global.InstanceID = nil
	if downloadClientToOAS(global).InstanceID.Set {
		t.Fatal("global client must not carry an instance id")
	}
	in := downloadClientInputFromOAS(&oas.DownloadClientInput{
		Kind: oas.DownloadClientKindQbittorrent, Name: "q", BaseURL: "http://q", Secret: oas.NewOptString("pw"),
		InstanceID: oas.NewOptUUID(instID), MaxActive: oas.NewOptInt(2),
	})
	if in.Kind != domain.ClientQBittorrent || in.Secret != "pw" || !in.Enabled || in.MaxActive != 2 || in.InstanceID == nil || *in.InstanceID != instID {
		t.Fatalf("downloadClientInputFromOAS: %+v", in)
	}
	snap := snapshotToOAS(dlclients.Snapshot{ClientID: c.ID, Name: "sab", Kind: domain.ClientSABnzbd, Reachable: true, Active: 1, DownloadRate: 10, CheckedAt: now})
	if !snap.ClientID.Set || snap.Version.Set || snap.Error.Set || snap.DownloadRateBps != 10 || !snap.Reachable {
		t.Fatalf("snapshotToOAS: %+v", snap)
	}
	if unsaved := snapshotToOAS(dlclients.Snapshot{Name: "n", Error: "nope"}); unsaved.ClientID.Set || !unsaved.Error.Set {
		t.Fatalf("snapshotToOAS unsaved: %+v", unsaved)
	}
	if got := emptyIfNil(nil); got == nil || len(got) != 0 {
		t.Fatal("emptyIfNil")
	}
}
