// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

func optString(s string) oas.OptString {
	if s == "" {
		return oas.OptString{}
	}
	return oas.NewOptString(s)
}

func optTime(t *time.Time) oas.OptDateTime {
	if t == nil {
		return oas.OptDateTime{}
	}
	return oas.NewOptDateTime(*t)
}

func userToOAS(u domain.User) oas.User {
	return oas.User{ID: u.ID, Username: u.Username, Role: oas.UserRole(u.Role)}
}

func instanceToOAS(i domain.Instance) oas.Instance {
	u, err := url.Parse(i.BaseURL)
	if err != nil {
		u = &url.URL{}
	}
	return oas.Instance{
		ID:              i.ID,
		Kind:            oas.AppKind(i.Kind),
		Name:            i.Name,
		BaseURL:         *u,
		Enabled:         i.Enabled,
		Source:          oas.InstanceSource(i.Source),
		ConfigarrKey:    optString(i.ConfigarrKey),
		LastSeenVersion: optString(i.LastSeenVersion),
		LastCheckAt:     optTime(i.LastCheckAt),
		LastError:       optString(i.LastError),
		CreatedAt:       i.CreatedAt,
		UpdatedAt:       i.UpdatedAt,
	}
}

func instancesToOAS(list []domain.Instance) []oas.Instance {
	out := make([]oas.Instance, 0, len(list))
	for _, i := range list {
		out = append(out, instanceToOAS(i))
	}
	return out
}

func inputFromOAS(in *oas.InstanceInput) instances.Input {
	return instances.Input{
		Kind:    domain.AppKind(in.Kind),
		Name:    in.Name,
		BaseURL: in.BaseURL,
		APIKey:  in.APIKey,
		Enabled: in.Enabled.Or(true),
	}
}

func updateFromOAS(in *oas.InstanceUpdate) instances.Input {
	return instances.Input{
		Kind:    domain.AppKind(in.Kind),
		Name:    in.Name,
		BaseURL: in.BaseURL,
		APIKey:  in.APIKey.Or(""),
		Enabled: in.Enabled.Or(true),
	}
}

func runToOAS(r domain.Run) oas.Run {
	return oas.Run{
		ID: r.ID, InstanceID: r.InstanceID, Kind: oas.HuntKind(r.Kind), Status: oas.RunStatus(r.Status),
		LeasedBy: optString(r.LeasedBy), LeaseExpiresAt: optTime(r.LeaseExpiresAt), QueuedAt: r.QueuedAt,
		StartedAt: optTime(r.StartedAt), FinishedAt: optTime(r.FinishedAt), SearchedCount: r.SearchedCount,
		Error: optString(r.Error),
	}
}

func eventToOAS(e domain.Event) oas.Event {
	out := oas.Event{
		ID: e.ID, InstanceID: e.InstanceID, Ts: e.Timestamp, Level: oas.EventLevel(e.Level), Type: e.Type,
		EntityType: optString(e.EntityType), Title: optString(e.Title), Detail: optString(e.Detail),
	}
	if e.RunID != nil {
		out.RunID = oas.NewOptUUID(*e.RunID)
	}
	if e.EntityID != 0 {
		out.EntityID = oas.NewOptInt64(e.EntityID)
	}
	return out
}

func policyToOAS(p domain.Policy) *oas.HuntPolicy {
	return &oas.HuntPolicy{
		MissingPerCycle:    p.MissingPerCycle,
		UpgradePerCycle:    p.UpgradePerCycle,
		CycleIntervalS:     int(p.CycleInterval / time.Second),
		HourlyCap:          p.HourlyCap,
		Selection:          oas.HuntPolicySelection(p.Selection),
		MonitoredOnly:      p.MonitoredOnly,
		SkipFutureReleases: p.SkipFutureReleases,
		RadarrReleaseType:  oas.HuntPolicyRadarrReleaseType(p.RadarrReleaseType),
		SonarrMissingMode:  oas.HuntPolicySonarrMissingMode(p.SonarrMissingMode),
		SonarrUpgradeMode:  oas.HuntPolicySonarrUpgradeMode(p.SonarrUpgradeMode),
		LidarrMissingMode:  oas.HuntPolicyLidarrMissingMode(p.LidarrMissingMode),
		ProcessedTTLH:      int(p.ProcessedTTL / time.Hour),
		MaxQueueSize:       p.MaxQueueSize,
		AwaitCommand:       p.AwaitCommand,
		PageSize:           p.PageSize,
		UpdatedAt:          oas.NewOptDateTime(p.UpdatedAt),
	}
}

func policyFromOAS(instanceID uuid.UUID, in *oas.HuntPolicy) domain.Policy {
	return domain.Policy{
		InstanceID:         instanceID,
		MissingPerCycle:    in.MissingPerCycle,
		UpgradePerCycle:    in.UpgradePerCycle,
		CycleInterval:      time.Duration(in.CycleIntervalS) * time.Second,
		HourlyCap:          in.HourlyCap,
		Selection:          domain.Selection(in.Selection),
		MonitoredOnly:      in.MonitoredOnly,
		SkipFutureReleases: in.SkipFutureReleases,
		RadarrReleaseType:  string(in.RadarrReleaseType),
		SonarrMissingMode:  string(in.SonarrMissingMode),
		SonarrUpgradeMode:  string(in.SonarrUpgradeMode),
		LidarrMissingMode:  string(in.LidarrMissingMode),
		ProcessedTTL:       time.Duration(in.ProcessedTTLH) * time.Hour,
		MaxQueueSize:       in.MaxQueueSize,
		AwaitCommand:       in.AwaitCommand,
		PageSize:           in.PageSize,
	}
}
