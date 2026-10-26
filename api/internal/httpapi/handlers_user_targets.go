// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// ListMyNotifyTargets implements listMyNotifyTargets operation.
func (h *Handlers) ListMyNotifyTargets(ctx context.Context) ([]oas.UserNotifyTarget, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	targets, err := h.notify.ListUserTargets(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	out := make([]oas.UserNotifyTarget, 0, len(targets))
	for _, t := range targets {
		out = append(out, userNotifyTargetToOAS(t))
	}
	return out, nil
}

// GetMyNotifyTarget implements getMyNotifyTarget operation.
func (h *Handlers) GetMyNotifyTarget(ctx context.Context, params oas.GetMyNotifyTargetParams) (*oas.UserNotifyTarget, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	t, err := h.notify.GetUserTarget(ctx, u.ID, params.TargetId)
	if err != nil {
		return nil, err
	}
	res := userNotifyTargetToOAS(t)
	return &res, nil
}

// CreateMyNotifyTarget implements createMyNotifyTarget operation.
func (h *Handlers) CreateMyNotifyTarget(ctx context.Context, req *oas.UserNotifyTargetInput) (*oas.UserNotifyTarget, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	enabled := true
	if req.Enabled.IsSet() {
		enabled = req.Enabled.Value
	}
	in := domain.UserNotifyTargetInput{
		Kind:    domain.ChannelKind(req.Kind),
		Enabled: enabled,
		Config:  notificationConfigFromOAS(req.Config),
	}
	t, err := h.notify.CreateUserTarget(ctx, u.ID, in)
	if err != nil {
		return nil, err
	}
	res := userNotifyTargetToOAS(t)
	return &res, nil
}

// UpdateMyNotifyTarget implements updateMyNotifyTarget operation.
func (h *Handlers) UpdateMyNotifyTarget(ctx context.Context, req *oas.UserNotifyTargetUpdate, params oas.UpdateMyNotifyTargetParams) (*oas.UserNotifyTarget, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	enabled := true
	if req.Enabled.IsSet() {
		enabled = req.Enabled.Value
	}
	var cfg *domain.ChannelConfig
	if req.Config.IsSet() {
		c := notificationConfigFromOAS(req.Config.Value)
		cfg = &c
	}
	in := domain.UserNotifyTargetUpdateInput{
		Kind:    domain.ChannelKind(req.Kind),
		Enabled: enabled,
		Config:  cfg,
	}
	t, err := h.notify.UpdateUserTarget(ctx, u.ID, params.TargetId, in)
	if err != nil {
		return nil, err
	}
	res := userNotifyTargetToOAS(t)
	return &res, nil
}

// DeleteMyNotifyTarget implements deleteMyNotifyTarget operation.
func (h *Handlers) DeleteMyNotifyTarget(ctx context.Context, params oas.DeleteMyNotifyTargetParams) error {
	u, ok := UserFrom(ctx)
	if !ok {
		return domain.ErrUnauthorized
	}
	return h.notify.DeleteUserTarget(ctx, u.ID, params.TargetId)
}

// TestMyNotifyTarget implements testMyNotifyTarget operation.
func (h *Handlers) TestMyNotifyTarget(ctx context.Context, params oas.TestMyNotifyTargetParams) error {
	u, ok := UserFrom(ctx)
	if !ok {
		return domain.ErrUnauthorized
	}
	return h.notify.TestUserTarget(ctx, u.ID, params.TargetId)
}
