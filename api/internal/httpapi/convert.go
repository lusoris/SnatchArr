// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
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

func optInt(p *int) oas.OptInt {
	if p == nil {
		return oas.OptInt{}
	}
	return oas.NewOptInt(*p)
}

func userToOAS(u domain.User) oas.User {
	return oas.User{ID: u.ID, Username: u.Username, Role: oas.UserRole(u.Role)}
}

// probeResultToOAS is the answer of an instance probe. The product is left out for a kind
// that one product serves.
func probeResultToOAS(res arrclient.Result) *oas.ProbeResult {
	return &oas.ProbeResult{AppName: res.AppName, Version: res.Version, Product: productToOAS(res.Product)}
}

// productToOAS names a product in the API. A value outside the enumeration is a row a newer
// build wrote: this build cannot name it, and the encoder would refuse it, so it is `unknown`.
func productToOAS(p domain.Product) oas.OptArrProduct {
	if p == domain.ProductNone {
		return oas.OptArrProduct{}
	}
	out := oas.ArrProduct(p)
	if out.Validate() != nil {
		out = oas.ArrProductUnknown
	}
	return oas.NewOptArrProduct(out)
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
		LastSeenProduct: productToOAS(i.LastSeenProduct),
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
		ID: r.ID, InstanceID: r.InstanceID, Kind: oas.SnatchKind(r.Kind), Status: oas.RunStatus(r.Status),
		LeasedBy: optString(r.LeasedBy), LeaseExpiresAt: optTime(r.LeaseExpiresAt), QueuedAt: r.QueuedAt,
		StartedAt: optTime(r.StartedAt), FinishedAt: optTime(r.FinishedAt), SearchedCount: r.SearchedCount,
		Error: optString(r.Error), FocusEntityID: optInt64(r.FocusEntityID), FocusGroupID: optInt64(r.FocusGroupID),
	}
}

func optInt64(p *int64) oas.OptInt64 {
	if p == nil {
		return oas.OptInt64{}
	}
	return oas.NewOptInt64(*p)
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

func policyToOAS(p domain.Policy) *oas.SnatchPolicy {
	return &oas.SnatchPolicy{
		MissingPerCycle:        p.MissingPerCycle,
		UpgradePerCycle:        p.UpgradePerCycle,
		CycleIntervalS:         int(p.CycleInterval / time.Second),
		HourlyCap:              p.HourlyCap,
		Selection:              oas.SnatchPolicySelection(p.Selection),
		MonitoredOnly:          p.MonitoredOnly,
		SkipFutureReleases:     p.SkipFutureReleases,
		RadarrReleaseType:      oas.SnatchPolicyRadarrReleaseType(p.RadarrReleaseType),
		SonarrMissingMode:      oas.SnatchPolicySonarrMissingMode(p.SonarrMissingMode),
		SonarrUpgradeMode:      oas.SnatchPolicySonarrUpgradeMode(p.SonarrUpgradeMode),
		LidarrMissingMode:      oas.SnatchPolicyLidarrMissingMode(p.LidarrMissingMode),
		ProcessedTTLH:          int(p.ProcessedTTL / time.Hour),
		MaxQueueSize:           p.MaxQueueSize,
		AwaitCommand:           p.AwaitCommand,
		PageSize:               p.PageSize,
		RecentGrabWindowH:      int(p.RecentGrabWindow / time.Hour),
		AfterglowMaxH:          int(p.AfterglowMax / time.Hour),
		IncludeTags:            domain.FilterIDs(p.IncludeTags),
		ExcludeTags:            domain.FilterIDs(p.ExcludeTags),
		IncludeQualityProfiles: domain.FilterIDs(p.IncludeQualityProfiles),
		ExcludeQualityProfiles: domain.FilterIDs(p.ExcludeQualityProfiles),
		ReleaseDelayH:          oas.NewOptInt(int(p.ReleaseDelay / time.Hour)),
		UpdatedAt:              oas.NewOptDateTime(p.UpdatedAt),
	}
}

func policyFromOAS(instanceID uuid.UUID, in *oas.SnatchPolicy) domain.Policy {
	return domain.Policy{
		InstanceID:             instanceID,
		MissingPerCycle:        in.MissingPerCycle,
		UpgradePerCycle:        in.UpgradePerCycle,
		CycleInterval:          time.Duration(in.CycleIntervalS) * time.Second,
		HourlyCap:              in.HourlyCap,
		Selection:              domain.Selection(in.Selection),
		MonitoredOnly:          in.MonitoredOnly,
		SkipFutureReleases:     in.SkipFutureReleases,
		RadarrReleaseType:      string(in.RadarrReleaseType),
		SonarrMissingMode:      string(in.SonarrMissingMode),
		SonarrUpgradeMode:      string(in.SonarrUpgradeMode),
		LidarrMissingMode:      string(in.LidarrMissingMode),
		ProcessedTTL:           time.Duration(in.ProcessedTTLH) * time.Hour,
		MaxQueueSize:           in.MaxQueueSize,
		AwaitCommand:           in.AwaitCommand,
		PageSize:               in.PageSize,
		RecentGrabWindow:       time.Duration(in.RecentGrabWindowH) * time.Hour,
		AfterglowMax:           time.Duration(in.AfterglowMaxH) * time.Hour,
		IncludeTags:            domain.FilterIDs(in.IncludeTags),
		ExcludeTags:            domain.FilterIDs(in.ExcludeTags),
		IncludeQualityProfiles: domain.FilterIDs(in.IncludeQualityProfiles),
		ExcludeQualityProfiles: domain.FilterIDs(in.ExcludeQualityProfiles),
		ReleaseDelay:           time.Duration(in.ReleaseDelayH.Or(0)) * time.Hour,
	}
}

func settingsToOAS(s domain.Settings) *oas.Settings {
	return &oas.Settings{
		HistoryRetentionDays: s.HistoryRetentionDays, UserAgent: s.UserAgent, GlobalHourlyCap: s.GlobalHourlyCap,
		UpdatedAt: oas.NewOptDateTime(s.UpdatedAt),
	}
}

func settingsFromOAS(in *oas.Settings) domain.Settings {
	return domain.Settings{HistoryRetentionDays: in.HistoryRetentionDays, UserAgent: in.UserAgent, GlobalHourlyCap: in.GlobalHourlyCap}
}

func notificationChannelToOAS(c domain.NotificationChannel) oas.NotificationChannel {
	return oas.NotificationChannel{
		ID:        c.ID,
		Name:      c.Name,
		Kind:      oas.NotificationChannelKind(c.Kind),
		Enabled:   c.Enabled,
		HasSecret: c.HasSecret,
		Config:    notificationConfigToOAS(c.Config),
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

func notificationConfigToOAS(c domain.ChannelConfig) oas.NotificationChannelConfig {
	return oas.NotificationChannelConfig{
		WebhookURL:          optString(c.WebhookURL),
		Token:               optString(c.Token),
		BotToken:            optString(c.BotToken),
		AppToken:            optString(c.AppToken),
		Password:            optString(c.Password),
		Secret:              optString(c.Secret),
		ServerURL:           optString(c.ServerURL),
		Topic:               optString(c.Topic),
		ChatID:              optString(c.ChatID),
		Channel:             optString(c.Channel),
		Username:            optString(c.Username),
		AvatarURL:           optString(c.AvatarURL),
		IconEmoji:           optString(c.IconEmoji),
		ParseMode:           optString(c.ParseMode),
		ThemeColor:          optString(c.ThemeColor),
		Priority:            oas.NewOptInt(c.Priority),
		Tags:                c.Tags,
		URL:                 optString(c.URL),
		AllowPrivateNetwork: oas.NewOptBool(c.AllowPrivateNetwork),
		AllowInsecureHTTP:   oas.NewOptBool(c.AllowInsecureHTTP),
	}
}

func notificationConfigFromOAS(c oas.NotificationChannelConfig) domain.ChannelConfig {
	return domain.ChannelConfig{
		WebhookURL:          c.WebhookURL.Value,
		Token:               c.Token.Value,
		BotToken:            c.BotToken.Value,
		AppToken:            c.AppToken.Value,
		Password:            c.Password.Value,
		Secret:              c.Secret.Value,
		ServerURL:           c.ServerURL.Value,
		Topic:               c.Topic.Value,
		ChatID:              c.ChatID.Value,
		Channel:             c.Channel.Value,
		Username:            c.Username.Value,
		AvatarURL:           c.AvatarURL.Value,
		IconEmoji:           c.IconEmoji.Value,
		ParseMode:           c.ParseMode.Value,
		ThemeColor:          c.ThemeColor.Value,
		Priority:            c.Priority.Value,
		Tags:                c.Tags,
		URL:                 c.URL.Value,
		AllowPrivateNetwork: c.AllowPrivateNetwork.Value,
		AllowInsecureHTTP:   c.AllowInsecureHTTP.Value,
	}
}

func notificationRuleToOAS(r domain.NotificationRule) oas.NotificationRule {
	return oas.NotificationRule{
		ID:         r.ID,
		Name:       r.Name,
		EventKind:  oas.NotificationRuleEventKind(r.EventKind),
		Threshold:  r.Threshold,
		Enabled:    r.Enabled,
		ChannelIds: r.ChannelIDs,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}
