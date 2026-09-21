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
		MissingPerCycle:    int32(p.MissingPerCycle),             // #nosec G115 -- validated 0..100
		UpgradePerCycle:    int32(p.UpgradePerCycle),             // #nosec G115 -- validated 0..100
		CycleIntervalS:     int32(p.CycleInterval / time.Second), // #nosec G115 -- validated <= 7 days
		HourlyCap:          int32(p.HourlyCap),                   // #nosec G115 -- validated 1..500
		Selection:          string(p.Selection),
		MonitoredOnly:      p.MonitoredOnly,
		SkipFutureReleases: p.SkipFutureReleases,
		RadarrReleaseType:  p.RadarrReleaseType,
		SonarrMissingMode:  p.SonarrMissingMode,
		SonarrUpgradeMode:  p.SonarrUpgradeMode,
		LidarrMissingMode:  p.LidarrMissingMode,
		ProcessedTtlH:      int32(p.ProcessedTTL / time.Hour), // #nosec G115 -- validated 1..8760
		MaxQueueSize:       int32(p.MaxQueueSize),             // #nosec G115 -- validated -1..100000
		AwaitCommand:       p.AwaitCommand,
		PageSize:           int32(p.PageSize), // #nosec G115 -- validated 10..1000
		UpdatedAt:          now,
	}
}

// DownloadClientFromRow converts a row to the domain type. The secret is never carried.
func DownloadClientFromRow(r sqlcgen.DownloadClient) domain.DownloadClient {
	remote := 0
	if r.RemoteID != nil {
		remote = int(*r.RemoteID)
	}
	return domain.DownloadClient{
		ID:                 r.ID,
		InstanceID:         r.InstanceID,
		Kind:               domain.DownloadClientKind(r.Kind),
		Name:               r.Name,
		BaseURL:            r.BaseUrl,
		Username:           r.Username,
		Enabled:            r.Enabled,
		Source:             domain.DownloadClientSource(r.Source),
		RemoteID:           remote,
		MaxActive:          int(r.MaxActive),
		BandwidthBudgetBPS: r.BandwidthBudgetBps,
		LastCheckAt:        r.LastCheckAt,
		LastError:          deref(r.LastError),
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

// DownloadClientsFromRows converts a slice.
func DownloadClientsFromRows(rows []sqlcgen.DownloadClient) []domain.DownloadClient {
	out := make([]domain.DownloadClient, 0, len(rows))
	for _, r := range rows {
		out = append(out, DownloadClientFromRow(r))
	}
	return out
}
