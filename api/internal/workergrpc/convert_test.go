// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
)

func TestEnumConversions(t *testing.T) {
	t.Parallel()
	apps := map[domain.AppKind]snatcharrv1.AppKind{
		domain.KindSonarr: snatcharrv1.AppKind_APP_KIND_SONARR, domain.KindRadarr: snatcharrv1.AppKind_APP_KIND_RADARR,
		domain.KindLidarr: snatcharrv1.AppKind_APP_KIND_LIDARR, domain.KindReadarr: snatcharrv1.AppKind_APP_KIND_READARR,
		domain.KindWhisparrV2: snatcharrv1.AppKind_APP_KIND_WHISPARR_V2, domain.KindWhisparrV3: snatcharrv1.AppKind_APP_KIND_WHISPARR_V3,
		"bogus": snatcharrv1.AppKind_APP_KIND_UNSPECIFIED,
	}
	for in, want := range apps {
		if got := appKind(in); got != want {
			t.Errorf("appKind(%q) = %v, want %v", in, got, want)
		}
	}
	if snatchKind(domain.SnatchMissing) != snatcharrv1.SnatchKind_SNATCH_KIND_MISSING || snatchKind(domain.SnatchUpgrade) != snatcharrv1.SnatchKind_SNATCH_KIND_UPGRADE || snatchKind("x") != snatcharrv1.SnatchKind_SNATCH_KIND_UNSPECIFIED {
		t.Error("snatchKind mapping")
	}
	if selection(domain.SelectionSequential) != snatcharrv1.Selection_SELECTION_SEQUENTIAL || selection(domain.SelectionRandom) != snatcharrv1.Selection_SELECTION_RANDOM {
		t.Error("selection mapping")
	}
	if sonarrMode("season_packs") != snatcharrv1.SonarrMode_SONARR_MODE_SEASON_PACKS || sonarrMode("shows") != snatcharrv1.SonarrMode_SONARR_MODE_SHOWS || sonarrMode("episodes") != snatcharrv1.SonarrMode_SONARR_MODE_EPISODES {
		t.Error("sonarrMode mapping")
	}
	if lidarrMode("album") != snatcharrv1.LidarrMode_LIDARR_MODE_ALBUM || lidarrMode("artist") != snatcharrv1.LidarrMode_LIDARR_MODE_ARTIST {
		t.Error("lidarrMode mapping")
	}
	if releaseType("digital") != snatcharrv1.RadarrReleaseType_RADARR_RELEASE_TYPE_DIGITAL || releaseType("cinema") != snatcharrv1.RadarrReleaseType_RADARR_RELEASE_TYPE_CINEMA || releaseType("physical") != snatcharrv1.RadarrReleaseType_RADARR_RELEASE_TYPE_PHYSICAL {
		t.Error("releaseType mapping")
	}
}

func TestPolicyProtoPicksTheSnatchKindsFields(t *testing.T) {
	t.Parallel()
	p := domain.Policy{
		MissingPerCycle: 3, UpgradePerCycle: 2, Selection: domain.SelectionRandom, MonitoredOnly: true, SkipFutureReleases: true,
		SonarrMissingMode: "shows", SonarrUpgradeMode: "season_packs", LidarrMissingMode: "album", RadarrReleaseType: "digital",
		AwaitCommand: true, PageSize: 250, CycleInterval: time.Minute,
	}
	missing := policyProto(p, domain.SnatchMissing)
	if missing.GetPerCycle() != 3 || missing.GetSonarrMode() != snatcharrv1.SonarrMode_SONARR_MODE_SHOWS || !missing.GetMonitoredOnly() || missing.GetPageSize() != 250 {
		t.Fatalf("missing: %+v", missing)
	}
	upgrade := policyProto(p, domain.SnatchUpgrade)
	if upgrade.GetPerCycle() != 2 || upgrade.GetSonarrMode() != snatcharrv1.SonarrMode_SONARR_MODE_SEASON_PACKS || !upgrade.GetAwaitCommand() {
		t.Fatalf("upgrade: %+v", upgrade)
	}
	if upgrade.GetLidarrMode() != snatcharrv1.LidarrMode_LIDARR_MODE_ALBUM || upgrade.GetRadarrReleaseType() != snatcharrv1.RadarrReleaseType_RADARR_RELEASE_TYPE_DIGITAL {
		t.Fatalf("upgrade modes: %+v", upgrade)
	}
}

func TestLevelNameAndOutcome(t *testing.T) {
	t.Parallel()
	if levelName(snatcharrv1.Level_LEVEL_UNSPECIFIED) != "info" || levelName(snatcharrv1.Level_LEVEL_WARN) != "warn" {
		t.Error("levelName")
	}
	cases := map[snatcharrv1.RunOutcome]domain.RunStatus{
		snatcharrv1.RunOutcome_RUN_OUTCOME_DONE:        domain.RunDone,
		snatcharrv1.RunOutcome_RUN_OUTCOME_UNSPECIFIED: domain.RunDone,
		snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED:      domain.RunFailed,
		snatcharrv1.RunOutcome_RUN_OUTCOME_CANCELLED:   domain.RunCancelled,
	}
	for in, want := range cases {
		if got := outcome(in); got != want {
			t.Errorf("outcome(%v) = %v, want %v", in, got, want)
		}
	}
}
