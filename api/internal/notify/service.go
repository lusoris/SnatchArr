// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"
	"github.com/golusoris/golusoris/webhooks/out"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Service provides management operations for notification channels and rules.
type Service struct {
	st     *store.Store
	ws     *WebhookStore
	uts    *UserTargetStore
	enc    *crypto.Encryptor
	clk    clock.Clock
	ids    id.Generator
	logger *slog.Logger
}

// NewService wires a Service instance.
func NewService(st *store.Store, ws *WebhookStore, enc *crypto.Encryptor, clk clock.Clock, ids id.Generator, logger *slog.Logger) *Service {
	return &Service{
		st:     st,
		ws:     ws,
		uts:    NewUserTargetStore(st, enc, clk),
		enc:    enc,
		clk:    clk,
		ids:    ids,
		logger: logger,
	}
}

// UserTargetStore returns the underlying UserTargetStore (for tests and verification).
func (s *Service) UserTargetStore() *UserTargetStore {
	return s.uts
}

// ListChannels returns all configured notification channels with redacted secrets.
func (s *Service) ListChannels(ctx context.Context) ([]domain.NotificationChannel, error) {
	rows, err := s.st.Q().ListNotificationChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("notify: list channels: %w", store.MapError(err))
	}
	channels := make([]domain.NotificationChannel, 0, len(rows))
	for _, r := range rows {
		ch, convErr := s.channelFromRow(r)
		if convErr != nil {
			return nil, convErr
		}
		ch.Config = ch.Config.Redacted()
		channels = append(channels, ch)
	}
	return channels, nil
}

// GetChannel returns a channel by ID with redacted secrets.
func (s *Service) GetChannel(ctx context.Context, chID uuid.UUID) (domain.NotificationChannel, error) {
	ch, err := s.GetChannelWithSecrets(ctx, chID)
	if err != nil {
		return domain.NotificationChannel{}, err
	}
	ch.Config = ch.Config.Redacted()
	return ch, nil
}

// GetChannelWithSecrets loads a channel with decrypted secrets for internal dispatch.
func (s *Service) GetChannelWithSecrets(ctx context.Context, chID uuid.UUID) (domain.NotificationChannel, error) {
	row, err := s.st.Q().GetNotificationChannel(ctx, chID)
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("notify: get channel: %w", store.MapError(err))
	}
	return s.channelFromRow(row)
}

func (s *Service) channelFromRow(r sqlcgen.NotificationChannel) (domain.NotificationChannel, error) {
	cfgJSON, err := s.enc.Open(r.ConfigEnc)
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("notify: open config for %s: %w", r.ID, err)
	}
	var cfg domain.ChannelConfig
	if err := json.Unmarshal(cfgJSON, &cfg); err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("notify: unmarshal config: %w", err)
	}
	return domain.NotificationChannel{
		ID:        r.ID,
		Name:      r.Name,
		Kind:      domain.ChannelKind(r.Kind),
		Enabled:   r.Enabled,
		HasSecret: r.HasSecret,
		Config:    cfg,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}, nil
}

// CreateChannel creates a new notification channel with encrypted secrets.
func (s *Service) CreateChannel(ctx context.Context, in domain.NotificationChannelInput) (domain.NotificationChannel, error) {
	if err := validateChannelInput(in); err != nil {
		return domain.NotificationChannel{}, err
	}
	chID, err := s.ids.NewUUID()
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("notify: new id: %w", err)
	}
	cfgBytes, err := json.Marshal(in.Config) // #nosec G117 -- sealed immediately with crypto.Encryptor AES-GCM at rest
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("notify: marshal config: %w", err)
	}
	sealed, err := s.enc.Seal(cfgBytes)
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("notify: seal config: %w", err)
	}
	now := s.clk.Now()
	row, err := s.st.Q().CreateNotificationChannel(ctx, sqlcgen.CreateNotificationChannelParams{
		ID:        chID,
		Name:      in.Name,
		Kind:      string(in.Kind),
		Enabled:   in.Enabled,
		ConfigEnc: sealed,
		HasSecret: in.Config.HasSecret(),
		CreatedAt: now,
	})
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("notify: create channel: %w", store.MapError(err))
	}
	if in.Kind == domain.ChannelWebhook {
		if saveErr := s.ws.SaveEndpoint(ctx, out.Endpoint{
			ID: chID.String(), URL: in.Config.URL, Secret: in.Config.Secret, Active: in.Enabled,
		}); saveErr != nil {
			return domain.NotificationChannel{}, fmt.Errorf("notify: save webhook endpoint: %w", saveErr)
		}
	}
	res, err := s.channelFromRow(row)
	if err != nil {
		return domain.NotificationChannel{}, err
	}
	res.Config = res.Config.Redacted()
	return res, nil
}

// UpdateChannel updates an existing channel, merging existing secrets when omitted.
func (s *Service) UpdateChannel(ctx context.Context, chID uuid.UUID, in domain.NotificationChannelInput) (domain.NotificationChannel, error) {
	if err := validateChannelInput(in); err != nil {
		return domain.NotificationChannel{}, err
	}
	existing, err := s.GetChannelWithSecrets(ctx, chID)
	if err != nil {
		return domain.NotificationChannel{}, err
	}
	var cfg domain.ChannelConfig
	if in.KeepConfig {
		cfg = existing.Config
	} else {
		cfg = in.Config.MergeSecrets(existing.Config)
	}
	enabled := in.Enabled
	if in.KeepEnabled {
		enabled = existing.Enabled
	}
	cfgBytes, err := json.Marshal(cfg) // #nosec G117 -- sealed immediately with crypto.Encryptor AES-GCM at rest
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("notify: marshal config: %w", err)
	}
	sealed, err := s.enc.Seal(cfgBytes)
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("notify: seal config: %w", err)
	}
	row, err := s.st.Q().UpdateNotificationChannel(ctx, sqlcgen.UpdateNotificationChannelParams{
		ID:        chID,
		Name:      in.Name,
		Kind:      string(in.Kind),
		Enabled:   enabled,
		ConfigEnc: sealed,
		HasSecret: cfg.HasSecret(),
		UpdatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("notify: update channel: %w", store.MapError(err))
	}
	if err = s.syncWebhookEndpoint(ctx, chID, in.Kind, existing.Kind, cfg, enabled); err != nil {
		return domain.NotificationChannel{}, err
	}
	res, err := s.channelFromRow(row)
	if err != nil {
		return domain.NotificationChannel{}, err
	}
	res.Config = res.Config.Redacted()
	return res, nil
}

func (s *Service) syncWebhookEndpoint(ctx context.Context, chID uuid.UUID, newKind, oldKind domain.ChannelKind, cfg domain.ChannelConfig, enabled bool) error {
	if newKind == domain.ChannelWebhook {
		if err := s.ws.SaveEndpoint(ctx, out.Endpoint{
			ID: chID.String(), URL: cfg.URL, Secret: cfg.Secret, Active: enabled,
		}); err != nil {
			return fmt.Errorf("notify: update webhook endpoint: %w", err)
		}
		return nil
	}
	if oldKind == domain.ChannelWebhook {
		if err := s.ws.DeleteEndpoint(ctx, chID.String()); err != nil {
			return fmt.Errorf("notify: delete old webhook endpoint: %w", err)
		}
	}
	return nil
}

// DeleteChannel removes a channel and any associated webhook endpoint.
func (s *Service) DeleteChannel(ctx context.Context, chID uuid.UUID) error {
	n, err := s.st.Q().DeleteNotificationChannel(ctx, chID)
	if err != nil {
		return fmt.Errorf("notify: delete channel: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("%w: channel %s not found", domain.ErrNotFound, chID)
	}
	if err := s.ws.DeleteEndpoint(ctx, chID.String()); err != nil {
		return fmt.Errorf("notify: delete webhook endpoint: %w", err)
	}
	return nil
}

// TestChannel sends a synchronous test notification to a channel.
func (s *Service) TestChannel(ctx context.Context, chID uuid.UUID) error {
	ch, err := s.GetChannelWithSecrets(ctx, chID)
	if err != nil {
		return err
	}
	p := Payload{
		Event:     "test",
		Timestamp: s.clk.Now(),
		Title:     "SnatchArr Test Notification",
		Detail:    "Testing notification delivery from SnatchArr",
		Level:     "info",
	}
	if err := Deliver(ctx, ch, p, s.ws, s.logger, s.clk); err != nil {
		sanitized := SanitizeError(err, ch.Config)
		return fmt.Errorf("%w: %s", domain.ErrInvalid, sanitized)
	}
	return nil
}

// ListRules returns all configured notification rules.
func (s *Service) ListRules(ctx context.Context) ([]domain.NotificationRule, error) {
	rows, err := s.st.Q().ListNotificationRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("notify: list rules: %w", store.MapError(err))
	}
	return store.NotificationRulesFromRows(rows), nil
}

// GetRule returns a single notification rule by ID.
func (s *Service) GetRule(ctx context.Context, ruleID uuid.UUID) (domain.NotificationRule, error) {
	row, err := s.st.Q().GetNotificationRule(ctx, ruleID)
	if err != nil {
		return domain.NotificationRule{}, fmt.Errorf("notify: get rule: %w", store.MapError(err))
	}
	return store.NotificationRuleFromRow(row), nil
}

// CreateRule registers a new notification rule.
func (s *Service) CreateRule(ctx context.Context, in domain.NotificationRuleInput) (domain.NotificationRule, error) {
	if err := validateRuleInput(in); err != nil {
		return domain.NotificationRule{}, err
	}
	ruleID, err := s.ids.NewUUID()
	if err != nil {
		return domain.NotificationRule{}, fmt.Errorf("notify: new rule id: %w", err)
	}
	thresh := int32(max(in.Threshold, 1)) // #nosec G115 -- threshold bounded 1..1000
	row, err := s.st.Q().CreateNotificationRule(ctx, sqlcgen.CreateNotificationRuleParams{
		ID:         ruleID,
		Name:       in.Name,
		EventKind:  string(in.EventKind),
		Threshold:  thresh,
		Enabled:    in.Enabled,
		ChannelIds: in.ChannelIDs,
		CreatedAt:  s.clk.Now(),
	})
	if err != nil {
		return domain.NotificationRule{}, fmt.Errorf("notify: create rule: %w", store.MapError(err))
	}
	return store.NotificationRuleFromRow(row), nil
}

// UpdateRule updates an existing notification rule.
func (s *Service) UpdateRule(ctx context.Context, ruleID uuid.UUID, in domain.NotificationRuleInput) (domain.NotificationRule, error) {
	if err := validateRuleInput(in); err != nil {
		return domain.NotificationRule{}, err
	}
	thresh := int32(max(in.Threshold, 1)) // #nosec G115 -- threshold bounded 1..1000
	row, err := s.st.Q().UpdateNotificationRule(ctx, sqlcgen.UpdateNotificationRuleParams{
		ID:         ruleID,
		Name:       in.Name,
		EventKind:  string(in.EventKind),
		Threshold:  thresh,
		Enabled:    in.Enabled,
		ChannelIds: in.ChannelIDs,
		UpdatedAt:  s.clk.Now(),
	})
	if err != nil {
		return domain.NotificationRule{}, fmt.Errorf("notify: update rule: %w", store.MapError(err))
	}
	return store.NotificationRuleFromRow(row), nil
}

// DeleteRule removes a notification rule.
func (s *Service) DeleteRule(ctx context.Context, ruleID uuid.UUID) error {
	n, err := s.st.Q().DeleteNotificationRule(ctx, ruleID)
	if err != nil {
		return fmt.Errorf("notify: delete rule: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("%w: rule %s not found", domain.ErrNotFound, ruleID)
	}
	return nil
}

func validateChannelInput(in domain.NotificationChannelInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w: channel name is required", domain.ErrInvalid)
	}
	if len(in.Name) > 100 {
		return fmt.Errorf("%w: channel name exceeds 100 characters", domain.ErrInvalid)
	}
	if !in.Kind.Valid() {
		return fmt.Errorf("%w: invalid channel kind %q", domain.ErrInvalid, in.Kind)
	}
	return nil
}

func validateRuleInput(in domain.NotificationRuleInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w: rule name is required", domain.ErrInvalid)
	}
	if len(in.Name) > 100 {
		return fmt.Errorf("%w: rule name exceeds 100 characters", domain.ErrInvalid)
	}
	if !in.EventKind.Valid() {
		return fmt.Errorf("%w: invalid event kind %q", domain.ErrInvalid, in.EventKind)
	}
	if len(in.ChannelIDs) == 0 {
		return fmt.Errorf("%w: at least one channel is required", domain.ErrInvalid)
	}
	return nil
}

// ErrInvalidChannel marks channel configuration problems.
var ErrInvalidChannel = errors.New("notify: invalid channel")

// ValidateConfig checks that the channel kind is valid and that required configuration
// fields for that kind are populated.
func ValidateConfig(kind domain.ChannelKind, cfg domain.ChannelConfig) error {
	if !kind.Valid() {
		return fmt.Errorf("%w: invalid channel kind %q", domain.ErrBadRequest, kind)
	}
	if kind == domain.ChannelWebhook {
		return validateWebhookConfig(cfg)
	}
	if err := validateKindFields(kind, cfg); err != nil {
		return err
	}
	if _, err := buildChatSender(domain.NotificationChannel{Kind: kind, Config: cfg}); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrBadRequest, err)
	}
	return nil
}

func validateWebhookConfig(cfg domain.ChannelConfig) error {
	if strings.TrimSpace(cfg.URL) == "" {
		return fmt.Errorf("%w: url is required for webhook", domain.ErrBadRequest)
	}
	u, err := url.Parse(cfg.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: url must be a valid http or https URL", domain.ErrBadRequest)
	}
	return nil
}

func validateKindFields(kind domain.ChannelKind, cfg domain.ChannelConfig) error {
	switch kind {
	case domain.ChannelDiscord, domain.ChannelSlack, domain.ChannelTeams:
		return validateURLField(cfg.WebhookURL, string(kind))
	case domain.ChannelApprise:
		return validateURLField(cfg.WebhookURL, "apprise")
	case domain.ChannelTelegram:
		return validateTelegramFields(cfg)
	case domain.ChannelNtfy:
		return validateNtfyFields(cfg)
	case domain.ChannelGotify:
		return validateGotifyFields(cfg)
	case domain.ChannelWebhook:
		return nil
	}
	return nil
}

func validateTelegramFields(cfg domain.ChannelConfig) error {
	if strings.TrimSpace(cfg.BotToken) == "" || strings.TrimSpace(cfg.ChatID) == "" {
		return fmt.Errorf("%w: bot_token and chat_id are required for telegram", domain.ErrBadRequest)
	}
	return nil
}

func validateNtfyFields(cfg domain.ChannelConfig) error {
	if strings.TrimSpace(cfg.ServerURL) == "" && strings.TrimSpace(cfg.Topic) == "" {
		return fmt.Errorf("%w: server_url or topic is required for ntfy", domain.ErrBadRequest)
	}
	return nil
}

func validateGotifyFields(cfg domain.ChannelConfig) error {
	if strings.TrimSpace(cfg.ServerURL) == "" || strings.TrimSpace(cfg.AppToken) == "" {
		return fmt.Errorf("%w: server_url and app_token are required for gotify", domain.ErrBadRequest)
	}
	return nil
}

func validateURLField(raw, name string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%w: webhook_url is required for %s", domain.ErrBadRequest, name)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: webhook_url must be a valid http or https URL", domain.ErrBadRequest)
	}
	return nil
}

// ListUserTargets returns all targets for a user with redacted secrets.
func (s *Service) ListUserTargets(ctx context.Context, userID uuid.UUID) ([]domain.UserNotifyTarget, error) {
	rows, err := s.uts.ListTargets(ctx, userID)
	if err != nil {
		return nil, err
	}
	targets := make([]domain.UserNotifyTarget, 0, len(rows))
	for _, r := range rows {
		t, err := s.userTargetFromRow(r)
		if err != nil {
			return nil, err
		}
		t.Config = t.Config.Redacted()
		targets = append(targets, t)
	}
	return targets, nil
}

// ListEnabledUserTargets returns all enabled targets for a user with secrets for internal dispatch.
func (s *Service) ListEnabledUserTargets(ctx context.Context, userID uuid.UUID) ([]domain.UserNotifyTarget, error) {
	rows, err := s.uts.ListEnabledTargets(ctx, userID)
	if err != nil {
		return nil, err
	}
	targets := make([]domain.UserNotifyTarget, 0, len(rows))
	for _, r := range rows {
		t, err := s.userTargetFromRow(r)
		if err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, nil
}

// GetUserTarget returns a user target with redacted secrets.
func (s *Service) GetUserTarget(ctx context.Context, userID, targetID uuid.UUID) (domain.UserNotifyTarget, error) {
	t, err := s.GetUserTargetWithSecrets(ctx, userID, targetID)
	if err != nil {
		return domain.UserNotifyTarget{}, err
	}
	t.Config = t.Config.Redacted()
	return t, nil
}

// GetUserTargetWithSecrets loads a user target with decrypted secrets for internal dispatch.
func (s *Service) GetUserTargetWithSecrets(ctx context.Context, userID, targetID uuid.UUID) (domain.UserNotifyTarget, error) {
	row, err := s.uts.GetTarget(ctx, targetID, userID)
	if err != nil {
		return domain.UserNotifyTarget{}, err
	}
	return s.userTargetFromRow(row)
}

// CreateUserTarget creates a new personal notification target for a user with encrypted secrets.
func (s *Service) CreateUserTarget(ctx context.Context, userID uuid.UUID, in domain.UserNotifyTargetInput) (domain.UserNotifyTarget, error) {
	if err := ValidateConfig(in.Kind, in.Config); err != nil {
		return domain.UserNotifyTarget{}, err
	}
	targetID, err := s.ids.NewUUID()
	if err != nil {
		return domain.UserNotifyTarget{}, fmt.Errorf("notify: new target id: %w", err)
	}
	cfgBytes, err := json.Marshal(in.Config) // #nosec G117 -- sealed immediately with crypto.Encryptor AES-GCM at rest
	if err != nil {
		return domain.UserNotifyTarget{}, fmt.Errorf("notify: marshal user target config: %w", err)
	}
	sealed, err := s.enc.Seal(cfgBytes)
	if err != nil {
		return domain.UserNotifyTarget{}, fmt.Errorf("notify: seal user target config: %w", err)
	}
	row, err := s.uts.InsertTarget(ctx, targetID, userID, string(in.Kind), sealed, in.Enabled)
	if err != nil {
		return domain.UserNotifyTarget{}, err
	}
	if in.Kind == domain.ChannelWebhook {
		if saveErr := s.ws.SaveEndpoint(ctx, out.Endpoint{
			ID: targetID.String(), URL: in.Config.URL, Secret: in.Config.Secret, Active: in.Enabled,
		}); saveErr != nil {
			return domain.UserNotifyTarget{}, fmt.Errorf("notify: save webhook endpoint: %w", saveErr)
		}
	}
	res, err := s.userTargetFromRow(row)
	if err != nil {
		return domain.UserNotifyTarget{}, err
	}
	res.Config = res.Config.Redacted()
	return res, nil
}

// UpdateUserTarget updates an existing user target, preserving stored secrets when omitted.
func (s *Service) UpdateUserTarget(ctx context.Context, userID, targetID uuid.UUID, in domain.UserNotifyTargetUpdateInput) (domain.UserNotifyTarget, error) {
	if !in.Kind.Valid() {
		return domain.UserNotifyTarget{}, fmt.Errorf("%w: invalid channel kind %q", domain.ErrBadRequest, in.Kind)
	}
	existing, err := s.GetUserTargetWithSecrets(ctx, userID, targetID)
	if err != nil {
		return domain.UserNotifyTarget{}, err
	}
	var cfg domain.ChannelConfig
	if in.Config == nil {
		cfg = existing.Config
	} else {
		if vErr := ValidateConfig(in.Kind, *in.Config); vErr != nil {
			return domain.UserNotifyTarget{}, vErr
		}
		cfg = in.Config.MergeSecrets(existing.Config)
	}
	cfgBytes, err := json.Marshal(cfg) // #nosec G117 -- sealed immediately with crypto.Encryptor AES-GCM at rest
	if err != nil {
		return domain.UserNotifyTarget{}, fmt.Errorf("notify: marshal user target config: %w", err)
	}
	sealed, err := s.enc.Seal(cfgBytes)
	if err != nil {
		return domain.UserNotifyTarget{}, fmt.Errorf("notify: seal user target config: %w", err)
	}
	row, err := s.uts.UpdateTarget(ctx, targetID, userID, string(in.Kind), sealed, in.Enabled)
	if err != nil {
		return domain.UserNotifyTarget{}, err
	}
	if syncErr := s.syncWebhookEndpoint(ctx, targetID, in.Kind, existing.Kind, cfg, in.Enabled); syncErr != nil {
		return domain.UserNotifyTarget{}, syncErr
	}
	res, err := s.userTargetFromRow(row)
	if err != nil {
		return domain.UserNotifyTarget{}, err
	}
	res.Config = res.Config.Redacted()
	return res, nil
}

// DeleteUserTarget removes a personal notification target and any associated webhook endpoint.
func (s *Service) DeleteUserTarget(ctx context.Context, userID, targetID uuid.UUID) error {
	existing, err := s.GetUserTarget(ctx, userID, targetID)
	if err != nil {
		return err
	}
	if err := s.uts.DeleteTarget(ctx, targetID, userID); err != nil {
		return err
	}
	if existing.Kind == domain.ChannelWebhook {
		if err := s.ws.DeleteEndpoint(ctx, targetID.String()); err != nil {
			return fmt.Errorf("notify: delete webhook endpoint: %w", err)
		}
	}
	return nil
}

// TestUserTarget sends a test notification to a personal user target.
func (s *Service) TestUserTarget(ctx context.Context, userID, targetID uuid.UUID) error {
	t, err := s.GetUserTargetWithSecrets(ctx, userID, targetID)
	if err != nil {
		return err
	}
	ch := domain.NotificationChannel{
		ID:      t.ID,
		Name:    fmt.Sprintf("user-%s-%s", t.UserID, t.Kind),
		Kind:    t.Kind,
		Enabled: t.Enabled,
		Config:  t.Config,
	}
	p := Payload{
		Event:     "test",
		Timestamp: s.clk.Now(),
		Title:     "SnatchArr Test Notification",
		Detail:    "Testing notification delivery from SnatchArr",
		Level:     "info",
	}
	if err := Deliver(ctx, ch, p, s.ws, s.logger, s.clk); err != nil {
		sanitized := SanitizeError(err, ch.Config)
		return fmt.Errorf("%w: %s", domain.ErrInvalid, sanitized)
	}
	return nil
}

func (s *Service) userTargetFromRow(r sqlcgen.UserNotifyTarget) (domain.UserNotifyTarget, error) {
	cfgJSON, err := s.enc.Open(r.ConfigEnc)
	if err != nil {
		return domain.UserNotifyTarget{}, fmt.Errorf("notify: open user target config %s: %w", r.ID, err)
	}
	var cfg domain.ChannelConfig
	if err := json.Unmarshal(cfgJSON, &cfg); err != nil {
		return domain.UserNotifyTarget{}, fmt.Errorf("notify: unmarshal user target config: %w", err)
	}
	return domain.UserNotifyTarget{
		ID:        r.ID,
		UserID:    r.UserID,
		Kind:      domain.ChannelKind(r.Kind),
		Enabled:   r.Enabled,
		Config:    cfg,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}, nil
}
