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
		enc:    enc,
		clk:    clk,
		ids:    ids,
		logger: logger,
	}
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
	cfg := in.Config.MergeSecrets(existing.Config)
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
		Enabled:   in.Enabled,
		ConfigEnc: sealed,
		HasSecret: cfg.HasSecret(),
		UpdatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.NotificationChannel{}, fmt.Errorf("notify: update channel: %w", store.MapError(err))
	}
	if err = s.syncWebhookEndpoint(ctx, chID, in.Kind, existing.Kind, cfg, in.Enabled); err != nil {
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
