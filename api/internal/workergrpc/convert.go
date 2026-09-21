// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"errors"
	"io"
	"strings"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
)

var errEOF = io.EOF

func isEOF(err error) bool { return errors.Is(err, io.EOF) }

func appKind(k domain.AppKind) snatcharrv1.AppKind {
	switch k {
	case domain.KindSonarr:
		return snatcharrv1.AppKind_APP_KIND_SONARR
	case domain.KindRadarr:
		return snatcharrv1.AppKind_APP_KIND_RADARR
	case domain.KindLidarr:
		return snatcharrv1.AppKind_APP_KIND_LIDARR
	case domain.KindReadarr:
		return snatcharrv1.AppKind_APP_KIND_READARR
	case domain.KindWhisparrV2:
		return snatcharrv1.AppKind_APP_KIND_WHISPARR_V2
	case domain.KindWhisparrV3:
		return snatcharrv1.AppKind_APP_KIND_WHISPARR_V3
	default:
		return snatcharrv1.AppKind_APP_KIND_UNSPECIFIED
	}
}

func snatchKind(k domain.SnatchKind) snatcharrv1.SnatchKind {
	switch k {
	case domain.SnatchMissing:
		return snatcharrv1.SnatchKind_SNATCH_KIND_MISSING
	case domain.SnatchUpgrade:
		return snatcharrv1.SnatchKind_SNATCH_KIND_UPGRADE
	default:
		return snatcharrv1.SnatchKind_SNATCH_KIND_UNSPECIFIED
	}
}

func selection(s domain.Selection) snatcharrv1.Selection {
	if s == domain.SelectionSequential {
		return snatcharrv1.Selection_SELECTION_SEQUENTIAL
	}
	return snatcharrv1.Selection_SELECTION_RANDOM
}

func sonarrMode(mode string) snatcharrv1.SonarrMode {
	switch mode {
	case "season_packs":
		return snatcharrv1.SonarrMode_SONARR_MODE_SEASON_PACKS
	case "shows":
		return snatcharrv1.SonarrMode_SONARR_MODE_SHOWS
	default:
		return snatcharrv1.SonarrMode_SONARR_MODE_EPISODES
	}
}

func lidarrMode(mode string) snatcharrv1.LidarrMode {
	if mode == "album" {
		return snatcharrv1.LidarrMode_LIDARR_MODE_ALBUM
	}
	return snatcharrv1.LidarrMode_LIDARR_MODE_ARTIST
}

func releaseType(rt string) snatcharrv1.RadarrReleaseType {
	switch rt {
	case "digital":
		return snatcharrv1.RadarrReleaseType_RADARR_RELEASE_TYPE_DIGITAL
	case "cinema":
		return snatcharrv1.RadarrReleaseType_RADARR_RELEASE_TYPE_CINEMA
	default:
		return snatcharrv1.RadarrReleaseType_RADARR_RELEASE_TYPE_PHYSICAL
	}
}

// policyProto snapshots the policy for one snatch kind.
func policyProto(p domain.Policy, kind domain.SnatchKind) *snatcharrv1.Policy {
	perCycle, mode := p.MissingPerCycle, p.SonarrMissingMode
	if kind == domain.SnatchUpgrade {
		perCycle, mode = p.UpgradePerCycle, p.SonarrUpgradeMode
	}
	return &snatcharrv1.Policy{
		PerCycle:           uint32(max(perCycle, 0)), // #nosec G115 -- validated 0..100
		Selection:          selection(p.Selection),
		MonitoredOnly:      p.MonitoredOnly,
		SkipFutureReleases: p.SkipFutureReleases,
		SonarrMode:         sonarrMode(mode),
		LidarrMode:         lidarrMode(p.LidarrMissingMode),
		RadarrReleaseType:  releaseType(p.RadarrReleaseType),
		AwaitCommand:       p.AwaitCommand,
		PageSize:           uint32(max(p.PageSize, 0)), // #nosec G115 -- validated 10..1000
	}
}

func levelName(l snatcharrv1.Level) string {
	name := strings.TrimPrefix(l.String(), "LEVEL_")
	if name == "UNSPECIFIED" {
		return "info"
	}
	return strings.ToLower(name)
}

func outcome(o snatcharrv1.RunOutcome) domain.RunStatus {
	switch o {
	case snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED:
		return domain.RunFailed
	case snatcharrv1.RunOutcome_RUN_OUTCOME_CANCELLED:
		return domain.RunCancelled
	case snatcharrv1.RunOutcome_RUN_OUTCOME_DONE, snatcharrv1.RunOutcome_RUN_OUTCOME_UNSPECIFIED:
		return domain.RunDone
	default:
		return domain.RunDone
	}
}
