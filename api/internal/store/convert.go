// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt32(v *int32) *int {
	if v == nil {
		return nil
	}
	return new(int(*v))
}

func ptrInt32(v *int) *int32 {
	if v == nil {
		return nil
	}
	return new(int32(*v)) // #nosec G115 -- validated 1..100
}

// InstancePriorityFromValues converts nullable int32 columns to domain.InstancePriority.
func InstancePriorityFromValues(search, download *int32) domain.InstancePriority {
	return domain.InstancePriority{
		Search:   derefInt32(search),
		Download: derefInt32(download),
	}
}

// InstancePriorityToValues converts domain.InstancePriority to nullable int32 values.
func InstancePriorityToValues(p domain.InstancePriority) (search, download *int32) {
	return ptrInt32(p.Search), ptrInt32(p.Download)
}

// InstanceFromRow converts a row to the domain type. The API key is never carried.
func InstanceFromRow(r sqlcgen.Instance) domain.Instance {
	return domain.Instance{
		ID:               r.ID,
		Kind:             domain.AppKind(r.Kind),
		Name:             r.Name,
		BaseURL:          r.BaseUrl,
		Enabled:          r.Enabled,
		Source:           domain.InstanceSource(r.Source),
		ConfigarrKey:     deref(r.ConfigarrKey),
		SearchPriority:   derefInt32(r.SearchPriority),
		DownloadPriority: derefInt32(r.DownloadPriority),
		LastSeenVersion:  deref(r.LastSeenVersion),
		LastSeenProduct:  domain.Product(deref(r.LastSeenProduct)),
		LastCheckAt:      r.LastCheckAt,
		LastError:        deref(r.LastError),
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
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
func PolicyFromRow(r sqlcgen.SnatchPolicy) domain.Policy {
	return domain.Policy{
		InstanceID:             r.InstanceID,
		MissingPerCycle:        int(r.MissingPerCycle),
		UpgradePerCycle:        int(r.UpgradePerCycle),
		CycleInterval:          time.Duration(r.CycleIntervalS) * time.Second,
		HourlyCap:              int(r.HourlyCap),
		Selection:              domain.Selection(r.Selection),
		MonitoredOnly:          r.MonitoredOnly,
		SkipFutureReleases:     r.SkipFutureReleases,
		RadarrReleaseType:      r.RadarrReleaseType,
		SonarrMissingMode:      r.SonarrMissingMode,
		SonarrUpgradeMode:      r.SonarrUpgradeMode,
		LidarrMissingMode:      r.LidarrMissingMode,
		ProcessedTTL:           time.Duration(r.ProcessedTtlH) * time.Hour,
		MaxQueueSize:           int(r.MaxQueueSize),
		AwaitCommand:           r.AwaitCommand,
		PageSize:               int(r.PageSize),
		RecentGrabWindow:       time.Duration(r.RecentGrabWindowH) * time.Hour,
		AfterglowMax:           time.Duration(r.AfterglowMaxH) * time.Hour,
		IncludeTags:            r.IncludeTags,
		ExcludeTags:            r.ExcludeTags,
		IncludeQualityProfiles: r.IncludeQualityProfiles,
		ExcludeQualityProfiles: r.ExcludeQualityProfiles,
		ReleaseDelay:           time.Duration(r.ReleaseDelayH) * time.Hour,
		DryRun:                 r.DryRun,
		SkipUnavailableMovies:  r.SkipUnavailableMovies,
		UpdatedAt:              r.UpdatedAt,
	}
}

// PolicyParams builds the upsert parameters from a validated policy.
func PolicyParams(p domain.Policy, now time.Time) sqlcgen.UpsertPolicyParams {
	return sqlcgen.UpsertPolicyParams{
		InstanceID:             p.InstanceID,
		MissingPerCycle:        int32(p.MissingPerCycle),             // #nosec G115 -- validated 0..100
		UpgradePerCycle:        int32(p.UpgradePerCycle),             // #nosec G115 -- validated 0..100
		CycleIntervalS:         int32(p.CycleInterval / time.Second), // #nosec G115 -- validated <= 7 days
		HourlyCap:              int32(p.HourlyCap),                   // #nosec G115 -- validated 1..500
		Selection:              string(p.Selection),
		MonitoredOnly:          p.MonitoredOnly,
		SkipFutureReleases:     p.SkipFutureReleases,
		RadarrReleaseType:      p.RadarrReleaseType,
		SonarrMissingMode:      p.SonarrMissingMode,
		SonarrUpgradeMode:      p.SonarrUpgradeMode,
		LidarrMissingMode:      p.LidarrMissingMode,
		ProcessedTtlH:          int32(p.ProcessedTTL / time.Hour), // #nosec G115 -- validated 1..8760
		MaxQueueSize:           int32(p.MaxQueueSize),             // #nosec G115 -- validated -1..100000
		AwaitCommand:           p.AwaitCommand,
		PageSize:               int32(p.PageSize),                     // #nosec G115 -- validated 10..1000
		RecentGrabWindowH:      int32(p.RecentGrabWindow / time.Hour), // #nosec G115 -- validated 0..720
		AfterglowMaxH:          int32(p.AfterglowMax / time.Hour),     // #nosec G115 -- validated 1..8760
		IncludeTags:            domain.FilterIDs(p.IncludeTags),
		ExcludeTags:            domain.FilterIDs(p.ExcludeTags),
		IncludeQualityProfiles: domain.FilterIDs(p.IncludeQualityProfiles),
		ExcludeQualityProfiles: domain.FilterIDs(p.ExcludeQualityProfiles),
		ReleaseDelayH:          int32(p.ReleaseDelay / time.Hour), // #nosec G115 -- validated 0..8760
		DryRun:                 p.DryRun,
		SkipUnavailableMovies:  p.SkipUnavailableMovies,
		UpdatedAt:              now,
	}
}

func presetFromJSON(raw []byte) domain.PriorityPreset {
	if len(raw) == 0 {
		return domain.MediaFirstPreset()
	}
	var p domain.PriorityPreset
	if err := json.Unmarshal(raw, &p); err != nil || len(p) == 0 {
		return domain.MediaFirstPreset()
	}
	return p
}

// SettingsFromRow converts the singleton settings row.
func SettingsFromRow(r sqlcgen.Setting) domain.Settings {
	writeTools := r.LlmWriteTools
	if writeTools == nil {
		writeTools = []string{}
	}
	return domain.Settings{
		HistoryRetentionDays:   int(r.HistoryRetentionDays),
		UserAgent:              r.UserAgent,
		GlobalHourlyCap:        int(r.GlobalHourlyCap),
		SearchPriorityPreset:   presetFromJSON(r.SearchPriorityPreset),
		DownloadPriorityPreset: presetFromJSON(r.DownloadPriorityPreset),
		LLMEnabled:             r.LlmEnabled,
		LLMWriteTools:          writeTools,
		UpdatedAt:              r.UpdatedAt,
	}
}

// ProwlarrLinkFromRow converts a row; the API key is never carried.
func ProwlarrLinkFromRow(r sqlcgen.ProwlarrLink) domain.ProwlarrLink {
	return domain.ProwlarrLink{
		ID: r.ID, Name: r.Name, BaseURL: r.BaseUrl, Enabled: r.Enabled,
		LastSeenVersion: deref(r.LastSeenVersion), LastCheckAt: r.LastCheckAt, LastError: deref(r.LastError),
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// SeerrLinkFromRow converts a row; the API key is never carried.
func SeerrLinkFromRow(r sqlcgen.SeerrLink) domain.SeerrLink {
	return domain.SeerrLink{
		ID: r.ID, Name: r.Name, BaseURL: r.BaseUrl, Enabled: r.Enabled,
		SonarrInstanceID: r.SonarrInstanceID, RadarrInstanceID: r.RadarrInstanceID,
		LastSeenVersion: deref(r.LastSeenVersion), LastCheckAt: r.LastCheckAt, LastError: deref(r.LastError),
		LastSyncAt: r.LastSyncAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// SeerrRequestFromRow converts a cached request row.
func SeerrRequestFromRow(r sqlcgen.SeerrRequest) domain.SeerrRequest {
	return seerrRequest(r.LinkID, r.RequestID, r.MediaType, r.TmdbID, r.TvdbID, r.Title, r.RequestStatus, r.MediaStatus, r.Is4k,
		r.RequestedBy, r.Seasons, r.SeerrServerID, r.InstanceID, r.EntityID, r.RequestedAt, r.LastSnatchedAt, r.LastSeenAt, r.UpdatedAt)
}

// SeerrRequestFromListRow converts a dashboard row (same columns plus names).
func SeerrRequestFromListRow(r sqlcgen.ListSeerrRequestsRow) domain.SeerrRequest {
	return seerrRequest(r.LinkID, r.RequestID, r.MediaType, r.TmdbID, r.TvdbID, r.Title, r.RequestStatus, r.MediaStatus, r.Is4k,
		r.RequestedBy, r.Seasons, r.SeerrServerID, r.InstanceID, r.EntityID, r.RequestedAt, r.LastSnatchedAt, r.LastSeenAt, r.UpdatedAt)
}

func seerrRequest(linkID uuid.UUID, requestID int32, mediaType string, tmdb, tvdb int32, title string, reqStatus, mediaStatus int32, is4k bool,
	requestedBy string, seasons []int32, serverID *int32, instanceID *uuid.UUID, entityID *int64, requestedAt, lastSnatched *time.Time, lastSeen, updated time.Time,
) domain.SeerrRequest {
	out := domain.SeerrRequest{
		LinkID: linkID, RequestID: int(requestID), MediaType: domain.SeerrMediaType(mediaType), TmdbID: int(tmdb), TvdbID: int(tvdb),
		Title: title, RequestStatus: int(reqStatus), MediaStatus: int(mediaStatus), Is4K: is4k, RequestedBy: requestedBy,
		InstanceID: instanceID, EntityID: entityID, RequestedAt: requestedAt, LastSnatchedAt: lastSnatched, LastSeenAt: lastSeen, UpdatedAt: updated,
	}
	if serverID != nil {
		out.SeerrServerID = int(*serverID)
	}
	out.Seasons = make([]int, 0, len(seasons))
	for _, s := range seasons {
		out.Seasons = append(out.Seasons, int(s))
	}
	return out
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
		ConfigarrKey:       deref(r.ConfigarrKey),
		MaxActive:          int(r.MaxActive),
		MaxQueued:          int(r.MaxQueued),
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

// NotificationRuleFromRow converts a notification rule row.
func NotificationRuleFromRow(r sqlcgen.NotificationRule) domain.NotificationRule {
	return domain.NotificationRule{
		ID:         r.ID,
		Name:       r.Name,
		EventKind:  domain.RuleEventKind(r.EventKind),
		Threshold:  int(r.Threshold),
		Enabled:    r.Enabled,
		ChannelIDs: r.ChannelIds,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}

// NotificationRulesFromRows converts a slice of rule rows.
func NotificationRulesFromRows(rows []sqlcgen.NotificationRule) []domain.NotificationRule {
	out := make([]domain.NotificationRule, 0, len(rows))
	for _, r := range rows {
		out = append(out, NotificationRuleFromRow(r))
	}
	return out
}
