// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// ListNotificationChannels implements listNotificationChannels operation.
func (h *Handlers) ListNotificationChannels(ctx context.Context) ([]oas.NotificationChannel, error) {
	channels, err := h.notify.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.NotificationChannel, 0, len(channels))
	for _, ch := range channels {
		out = append(out, notificationChannelToOAS(ch))
	}
	return out, nil
}

// GetNotificationChannel implements getNotificationChannel operation.
func (h *Handlers) GetNotificationChannel(ctx context.Context, params oas.GetNotificationChannelParams) (*oas.NotificationChannel, error) {
	ch, err := h.notify.GetChannel(ctx, params.ChannelId)
	if err != nil {
		return nil, err
	}
	res := notificationChannelToOAS(ch)
	return &res, nil
}

// CreateNotificationChannel implements createNotificationChannel operation.
func (h *Handlers) CreateNotificationChannel(ctx context.Context, req *oas.NotificationChannelInput) (*oas.NotificationChannel, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	in := domain.NotificationChannelInput{
		Name:    req.Name,
		Kind:    domain.ChannelKind(req.Kind),
		Enabled: req.Enabled.Value,
		Config:  notificationConfigFromOAS(req.Config),
	}
	if !req.Enabled.IsSet() {
		in.Enabled = true
	}
	ch, err := h.notify.CreateChannel(ctx, in)
	if err != nil {
		return nil, err
	}
	res := notificationChannelToOAS(ch)
	return &res, nil
}

// UpdateNotificationChannel implements updateNotificationChannel operation.
func (h *Handlers) UpdateNotificationChannel(ctx context.Context, req *oas.NotificationChannelUpdate, params oas.UpdateNotificationChannelParams) (*oas.NotificationChannel, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	in := domain.NotificationChannelInput{
		Name:    req.Name,
		Kind:    domain.ChannelKind(req.Kind),
		Enabled: req.Enabled.Value,
		Config:  notificationConfigFromOAS(req.Config.Value),
	}
	if !req.Enabled.IsSet() {
		in.Enabled = true
	}
	ch, err := h.notify.UpdateChannel(ctx, params.ChannelId, in)
	if err != nil {
		return nil, err
	}
	res := notificationChannelToOAS(ch)
	return &res, nil
}

// DeleteNotificationChannel implements deleteNotificationChannel operation.
func (h *Handlers) DeleteNotificationChannel(ctx context.Context, params oas.DeleteNotificationChannelParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.notify.DeleteChannel(ctx, params.ChannelId)
}

// TestNotificationChannel implements testNotificationChannel operation.
func (h *Handlers) TestNotificationChannel(ctx context.Context, params oas.TestNotificationChannelParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.notify.TestChannel(ctx, params.ChannelId)
}

// ListNotificationRules implements listNotificationRules operation.
func (h *Handlers) ListNotificationRules(ctx context.Context) ([]oas.NotificationRule, error) {
	rules, err := h.notify.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.NotificationRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, notificationRuleToOAS(r))
	}
	return out, nil
}

// GetNotificationRule implements getNotificationRule operation.
func (h *Handlers) GetNotificationRule(ctx context.Context, params oas.GetNotificationRuleParams) (*oas.NotificationRule, error) {
	r, err := h.notify.GetRule(ctx, params.RuleId)
	if err != nil {
		return nil, err
	}
	res := notificationRuleToOAS(r)
	return &res, nil
}

// CreateNotificationRule implements createNotificationRule operation.
func (h *Handlers) CreateNotificationRule(ctx context.Context, req *oas.NotificationRuleInput) (*oas.NotificationRule, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	thresh := req.Threshold
	if thresh <= 0 {
		thresh = 1
	}
	enabled := req.Enabled.Value
	if !req.Enabled.IsSet() {
		enabled = true
	}
	in := domain.NotificationRuleInput{
		Name:       req.Name,
		EventKind:  domain.RuleEventKind(req.EventKind),
		Threshold:  thresh,
		Enabled:    enabled,
		ChannelIDs: req.ChannelIds,
	}
	r, err := h.notify.CreateRule(ctx, in)
	if err != nil {
		return nil, err
	}
	res := notificationRuleToOAS(r)
	return &res, nil
}

// UpdateNotificationRule implements updateNotificationRule operation.
func (h *Handlers) UpdateNotificationRule(ctx context.Context, req *oas.NotificationRuleUpdate, params oas.UpdateNotificationRuleParams) (*oas.NotificationRule, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	thresh := req.Threshold
	if thresh <= 0 {
		thresh = 1
	}
	enabled := req.Enabled.Value
	if !req.Enabled.IsSet() {
		enabled = true
	}
	in := domain.NotificationRuleInput{
		Name:       req.Name,
		EventKind:  domain.RuleEventKind(req.EventKind),
		Threshold:  thresh,
		Enabled:    enabled,
		ChannelIDs: req.ChannelIds,
	}
	r, err := h.notify.UpdateRule(ctx, params.RuleId, in)
	if err != nil {
		return nil, err
	}
	res := notificationRuleToOAS(r)
	return &res, nil
}

// DeleteNotificationRule implements deleteNotificationRule operation.
func (h *Handlers) DeleteNotificationRule(ctx context.Context, params oas.DeleteNotificationRuleParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.notify.DeleteRule(ctx, params.RuleId)
}
