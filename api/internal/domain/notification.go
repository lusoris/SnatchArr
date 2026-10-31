// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"time"

	"github.com/google/uuid"
)

// ChannelKind identifies the notification delivery service.
type ChannelKind string

// Channel kinds.
const (
	ChannelDiscord  ChannelKind = "discord"
	ChannelSlack    ChannelKind = "slack"
	ChannelTelegram ChannelKind = "telegram"
	ChannelNtfy     ChannelKind = "ntfy"
	ChannelGotify   ChannelKind = "gotify"
	ChannelTeams    ChannelKind = "teams"
	ChannelWebhook  ChannelKind = "webhook"
	ChannelApprise  ChannelKind = "apprise"
)

// RuleEventKind names the event that triggers a notification rule.
type RuleEventKind string

// Rule event kinds.
const (
	RuleRunFailureStreak      RuleEventKind = "run_failure_streak"
	RuleCircuitOpened         RuleEventKind = "circuit_opened"
	RuleInstanceUnreachable   RuleEventKind = "instance_unreachable"
	RuleStaminaExhausted      RuleEventKind = "stamina_exhausted"
	RuleSeerrRequestSnatched  RuleEventKind = "seerr_request_snatched"
	RuleWatchawareGateDecided RuleEventKind = "watchaware_gate_decided"
)

// ChannelConfig holds the union of configuration options across all channel kinds.
type ChannelConfig struct {
	// Secret fields (never returned in responses; encrypted at rest)
	WebhookURL string `json:"webhook_url,omitempty"`
	Token      string `json:"token,omitempty"`
	BotToken   string `json:"bot_token,omitempty"`
	AppToken   string `json:"app_token,omitempty"`
	Password   string `json:"password,omitempty"`
	Secret     string `json:"secret,omitempty"`

	// Public fields
	ServerURL           string   `json:"server_url,omitempty"`
	Topic               string   `json:"topic,omitempty"`
	ChatID              string   `json:"chat_id,omitempty"`
	Channel             string   `json:"channel,omitempty"`
	Username            string   `json:"username,omitempty"`
	AvatarURL           string   `json:"avatar_url,omitempty"`
	IconEmoji           string   `json:"icon_emoji,omitempty"`
	ParseMode           string   `json:"parse_mode,omitempty"`
	ThemeColor          string   `json:"theme_color,omitempty"`
	Priority            int      `json:"priority,omitempty"`
	Tags                []string `json:"tags,omitempty"`
	URL                 string   `json:"url,omitempty"`
	AllowPrivateNetwork bool     `json:"allow_private_network,omitempty"`
	AllowInsecureHTTP   bool     `json:"allow_insecure_http,omitempty"`
}

// NotificationChannel is a delivery destination.
type NotificationChannel struct {
	ID        uuid.UUID
	Name      string
	Kind      ChannelKind
	Enabled   bool
	HasSecret bool
	Config    ChannelConfig
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NotificationRule determines when notifications are dispatched and to which channels.
type NotificationRule struct {
	ID         uuid.UUID
	Name       string
	EventKind  RuleEventKind
	Threshold  int
	Enabled    bool
	ChannelIDs []uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Valid reports whether the channel kind is recognized.
func (k ChannelKind) Valid() bool {
	switch k {
	case ChannelDiscord, ChannelSlack, ChannelTelegram, ChannelNtfy, ChannelGotify, ChannelTeams, ChannelWebhook, ChannelApprise:
		return true
	default:
		return false
	}
}

// Valid reports whether the rule event kind is recognized.
func (k RuleEventKind) Valid() bool {
	switch k {
	case RuleRunFailureStreak, RuleCircuitOpened, RuleInstanceUnreachable, RuleStaminaExhausted, RuleSeerrRequestSnatched, RuleWatchawareGateDecided:
		return true
	default:
		return false
	}
}

// HasSecret returns true if any secret field is populated.
func (c ChannelConfig) HasSecret() bool {
	return c.WebhookURL != "" || c.Token != "" || c.BotToken != "" ||
		c.AppToken != "" || c.Password != "" || c.Secret != ""
}

// Redacted returns a copy of the config with all secret fields cleared.
func (c ChannelConfig) Redacted() ChannelConfig {
	cp := c
	cp.WebhookURL = ""
	cp.Token = ""
	cp.BotToken = ""
	cp.AppToken = ""
	cp.Password = ""
	cp.Secret = ""
	return cp
}

// MergeSecrets returns a copy of c, taking any missing secret field from existing.
func (c ChannelConfig) MergeSecrets(existing ChannelConfig) ChannelConfig {
	cp := c
	if cp.WebhookURL == "" {
		cp.WebhookURL = existing.WebhookURL
	}
	if cp.Token == "" {
		cp.Token = existing.Token
	}
	if cp.BotToken == "" {
		cp.BotToken = existing.BotToken
	}
	if cp.AppToken == "" {
		cp.AppToken = existing.AppToken
	}
	if cp.Password == "" {
		cp.Password = existing.Password
	}
	if cp.Secret == "" {
		cp.Secret = existing.Secret
	}
	return cp
}

// NotificationChannelInput is input for creating or updating a channel.
type NotificationChannelInput struct {
	Name        string
	Kind        ChannelKind
	Enabled     bool
	Config      ChannelConfig
	KeepConfig  bool // KeepConfig preserves stored configuration (including secrets) on update.
	KeepEnabled bool // KeepEnabled preserves stored enabled flag on update.
}

// NotificationRuleInput is input for creating or updating a rule.
type NotificationRuleInput struct {
	Name       string
	EventKind  RuleEventKind
	Threshold  int
	Enabled    bool
	ChannelIDs []uuid.UUID
}

// UserNotifyTarget represents a per-user notification target with write-only config.
type UserNotifyTarget struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Kind      ChannelKind
	Enabled   bool
	Config    ChannelConfig
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserNotifyTargetInput is input for creating a user notification target.
type UserNotifyTargetInput struct {
	Kind    ChannelKind
	Enabled bool
	Config  ChannelConfig
}

// UserNotifyTargetUpdateInput is input for updating a user notification target.
type UserNotifyTargetUpdateInput struct {
	Kind    ChannelKind
	Enabled bool
	Config  *ChannelConfig // nil preserves stored config
}
