// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store

import (
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// InstanceFromRow converts a row to the domain type. The API key is never carried.
func InstanceFromRow(r sqlcgen.Instance) domain.Instance {
	return domain.Instance{
		ID:              r.ID,
		Kind:            domain.AppKind(r.Kind),
		Name:            r.Name,
		BaseURL:         r.BaseUrl,
		Enabled:         r.Enabled,
		Source:          domain.InstanceSource(r.Source),
		ConfigarrKey:    deref(r.ConfigarrKey),
		LastSeenVersion: deref(r.LastSeenVersion),
		LastCheckAt:     r.LastCheckAt,
		LastError:       deref(r.LastError),
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

// InstancesFromRows converts a slice.
func InstancesFromRows(rows []sqlcgen.Instance) []domain.Instance {
	out := make([]domain.Instance, 0, len(rows))
	for _, r := range rows {
		out = append(out, InstanceFromRow(r))
	}
	return out
}

// PolicyFromRow converts a policy row.
func PolicyFromRow(r sqlcgen.HuntPolicy) domain.Policy {
	return domain.Policy{
		InstanceID:         r.InstanceID,
		MissingPerCycle:    int(r.MissingPerCycle),
		UpgradePerCycle:    int(r.UpgradePerCycle),
		CycleInterval:      time.Duration(r.CycleIntervalS) * time.Second,
		HourlyCap:          int(r.HourlyCap),
		Selection:          domain.Selection(r.Selection),
		MonitoredOnly:      r.MonitoredOnly,
		SkipFutureReleases: r.SkipFutureReleases,
		RadarrReleaseType:  r.RadarrReleaseType,
		SonarrMissingMode:  r.SonarrMissingMode,
		SonarrUpgradeMode:  r.SonarrUpgradeMode,
		LidarrMissingMode:  r.LidarrMissingMode,
		ProcessedTTL:       time.Duration(r.ProcessedTtlH) * time.Hour,
		MaxQueueSize:       int(r.MaxQueueSize),
		AwaitCommand:       r.AwaitCommand,
		PageSize:           int(r.PageSize),
		UpdatedAt:          r.UpdatedAt,
	}
}

// PolicyParams builds the upsert parameters from a validated policy.
func PolicyParams(p domain.Policy, now time.Time) sqlcgen.UpsertPolicyParams {
	return sqlcgen.UpsertPolicyParams{
		InstanceID:         p.InstanceID,
		MissingPerCycle:    int32(p.MissingPerCycle),             //nolint:gosec // validated 0..100
		UpgradePerCycle:    int32(p.UpgradePerCycle),             //nolint:gosec // validated 0..100
		CycleIntervalS:     int32(p.CycleInterval / time.Second), //nolint:gosec // validated <= 7 days
		HourlyCap:          int32(p.HourlyCap),                   //nolint:gosec // validated 1..500
		Selection:          string(p.Selection),
		MonitoredOnly:      p.MonitoredOnly,
		SkipFutureReleases: p.SkipFutureReleases,
		RadarrReleaseType:  p.RadarrReleaseType,
		SonarrMissingMode:  p.SonarrMissingMode,
		SonarrUpgradeMode:  p.SonarrUpgradeMode,
		LidarrMissingMode:  p.LidarrMissingMode,
		ProcessedTtlH:      int32(p.ProcessedTTL / time.Hour), //nolint:gosec // validated 1..8760
		MaxQueueSize:       int32(p.MaxQueueSize),             //nolint:gosec // validated -1..100000
		AwaitCommand:       p.AwaitCommand,
		PageSize:           int32(p.PageSize), //nolint:gosec // validated 10..1000
		UpdatedAt:          now,
	}
}
