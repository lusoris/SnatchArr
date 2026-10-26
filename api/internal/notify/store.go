// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package notify

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/webhooks/out"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// WebhookStore implements webhooks/out.Store backed by Postgres and crypto.Encryptor.
type WebhookStore struct {
	st  *store.Store
	enc *crypto.Encryptor
}

// NewWebhookStore builds a WebhookStore.
func NewWebhookStore(st *store.Store, enc *crypto.Encryptor) *WebhookStore {
	return &WebhookStore{st: st, enc: enc}
}

// SaveEndpoint registers or updates an outbound webhook destination.
func (s *WebhookStore) SaveEndpoint(ctx context.Context, e out.Endpoint) error {
	secretEnc, err := s.enc.Seal([]byte(e.Secret))
	if err != nil {
		return fmt.Errorf("webhook store: seal secret: %w", err)
	}
	events := e.Events
	if events == nil {
		events = []string{}
	}
	_, err = s.st.Q().SaveWebhookEndpoint(ctx, sqlcgen.SaveWebhookEndpointParams{
		ID:        e.ID,
		Url:       e.URL,
		SecretEnc: secretEnc,
		Events:    events,
		Active:    e.Active,
	})
	if err != nil {
		return fmt.Errorf("webhook store: save endpoint: %w", store.MapError(err))
	}
	return nil
}

// FindEndpoint loads an endpoint by ID, decrypting its signing secret.
func (s *WebhookStore) FindEndpoint(ctx context.Context, id string) (out.Endpoint, error) {
	row, err := s.st.Q().FindWebhookEndpoint(ctx, id)
	if err != nil {
		return out.Endpoint{}, fmt.Errorf("webhook store: find endpoint: %w", store.MapError(err))
	}
	secret, err := s.enc.Open(row.SecretEnc)
	if err != nil {
		return out.Endpoint{}, fmt.Errorf("webhook store: open secret: %w", err)
	}
	return out.Endpoint{
		ID:     row.ID,
		URL:    row.Url,
		Secret: string(secret),
		Events: row.Events,
		Active: row.Active,
	}, nil
}

// ListEndpoints returns all active endpoints subscribed to event.
func (s *WebhookStore) ListEndpoints(ctx context.Context, event string) ([]out.Endpoint, error) {
	rows, err := s.st.Q().ListWebhookEndpoints(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("webhook store: list endpoints: %w", store.MapError(err))
	}
	endpoints := make([]out.Endpoint, 0, len(rows))
	for _, row := range rows {
		secret, openErr := s.enc.Open(row.SecretEnc)
		if openErr != nil {
			return nil, fmt.Errorf("webhook store: open secret for %s: %w", row.ID, openErr)
		}
		endpoints = append(endpoints, out.Endpoint{
			ID:     row.ID,
			URL:    row.Url,
			Secret: string(secret),
			Events: row.Events,
			Active: row.Active,
		})
	}
	return endpoints, nil
}

// DeleteEndpoint removes an endpoint subscription.
func (s *WebhookStore) DeleteEndpoint(ctx context.Context, id string) error {
	_, err := s.st.Q().DeleteWebhookEndpoint(ctx, id)
	if err != nil {
		return fmt.Errorf("webhook store: delete endpoint: %w", store.MapError(err))
	}
	return nil
}

// SaveDelivery records or updates a webhook delivery attempt.
func (s *WebhookStore) SaveDelivery(ctx context.Context, d out.Delivery) error {
	attempts := int32(d.Attempts)     // #nosec G115 -- bounded by max attempts <= 100
	statusCode := int32(d.StatusCode) // #nosec G115 -- HTTP status 0..999
	_, err := s.st.Q().SaveWebhookDelivery(ctx, sqlcgen.SaveWebhookDeliveryParams{
		ID:         d.ID,
		EndpointID: d.EndpointID,
		Event:      d.Event,
		Payload:    d.Payload,
		Attempts:   attempts,
		Status:     string(d.Status),
		StatusCode: statusCode,
		Error:      d.Error,
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("webhook store: save delivery: %w", store.MapError(err))
	}
	return nil
}

// FindDelivery looks up a delivery record.
func (s *WebhookStore) FindDelivery(ctx context.Context, id string) (out.Delivery, error) {
	row, err := s.st.Q().FindWebhookDelivery(ctx, id)
	if err != nil {
		return out.Delivery{}, fmt.Errorf("webhook store: find delivery: %w", store.MapError(err))
	}
	return out.Delivery{
		ID:         row.ID,
		EndpointID: row.EndpointID,
		Event:      row.Event,
		Payload:    row.Payload,
		Attempts:   int(row.Attempts),
		Status:     out.Status(row.Status),
		StatusCode: int(row.StatusCode),
		Error:      row.Error,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}, nil
}

// ListDeadLetters returns all deliveries that failed all retry attempts.
func (s *WebhookStore) ListDeadLetters(ctx context.Context) ([]out.Delivery, error) {
	rows, err := s.st.Q().ListDeadLetterDeliveries(ctx)
	if err != nil {
		return nil, fmt.Errorf("webhook store: list dead letters: %w", store.MapError(err))
	}
	outDeliveries := make([]out.Delivery, 0, len(rows))
	for _, row := range rows {
		outDeliveries = append(outDeliveries, out.Delivery{
			ID:         row.ID,
			EndpointID: row.EndpointID,
			Event:      row.Event,
			Payload:    row.Payload,
			Attempts:   int(row.Attempts),
			Status:     out.Status(row.Status),
			StatusCode: int(row.StatusCode),
			Error:      row.Error,
			CreatedAt:  row.CreatedAt,
			UpdatedAt:  row.UpdatedAt,
		})
	}
	return outDeliveries, nil
}

// UserTargetStore manages persistent user notification targets, ensuring at-rest encryption.
type UserTargetStore struct {
	st  *store.Store
	enc *crypto.Encryptor
	clk clock.Clock
}

// NewUserTargetStore creates a UserTargetStore.
func NewUserTargetStore(st *store.Store, enc *crypto.Encryptor, clk clock.Clock) *UserTargetStore {
	return &UserTargetStore{st: st, enc: enc, clk: clk}
}

// InsertTarget persists a target record, refusing unsealed bytes.
func (s *UserTargetStore) InsertTarget(ctx context.Context, id, userID uuid.UUID, kind string, configEnc []byte, enabled bool) (sqlcgen.UserNotifyTarget, error) {
	if _, err := s.enc.Open(configEnc); err != nil {
		return sqlcgen.UserNotifyTarget{}, fmt.Errorf("%w: store write refused unsealed config bytes", domain.ErrInvalid)
	}
	now := s.clk.Now()
	row, err := s.st.Q().CreateUserNotifyTarget(ctx, sqlcgen.CreateUserNotifyTargetParams{
		ID:        id,
		UserID:    userID,
		Kind:      kind,
		ConfigEnc: configEnc,
		Enabled:   enabled,
		CreatedAt: now,
	})
	if err != nil {
		return sqlcgen.UserNotifyTarget{}, fmt.Errorf("user target store: create target: %w", store.MapError(err))
	}
	return row, nil
}

// UpdateTarget updates an existing target, refusing unsealed bytes.
func (s *UserTargetStore) UpdateTarget(ctx context.Context, id, userID uuid.UUID, kind string, configEnc []byte, enabled bool) (sqlcgen.UserNotifyTarget, error) {
	if _, err := s.enc.Open(configEnc); err != nil {
		return sqlcgen.UserNotifyTarget{}, fmt.Errorf("%w: store write refused unsealed config bytes", domain.ErrInvalid)
	}
	now := s.clk.Now()
	row, err := s.st.Q().UpdateUserNotifyTarget(ctx, sqlcgen.UpdateUserNotifyTargetParams{
		ID:        id,
		UserID:    userID,
		Kind:      kind,
		ConfigEnc: configEnc,
		Enabled:   enabled,
		UpdatedAt: now,
	})
	if err != nil {
		return sqlcgen.UserNotifyTarget{}, fmt.Errorf("user target store: update target: %w", store.MapError(err))
	}
	return row, nil
}

// GetTarget retrieves a user target.
func (s *UserTargetStore) GetTarget(ctx context.Context, id, userID uuid.UUID) (sqlcgen.UserNotifyTarget, error) {
	row, err := s.st.Q().GetUserNotifyTarget(ctx, sqlcgen.GetUserNotifyTargetParams{
		ID:     id,
		UserID: userID,
	})
	if err != nil {
		return sqlcgen.UserNotifyTarget{}, fmt.Errorf("user target store: get target: %w", store.MapError(err))
	}
	return row, nil
}

// ListTargets lists all targets for a user.
func (s *UserTargetStore) ListTargets(ctx context.Context, userID uuid.UUID) ([]sqlcgen.UserNotifyTarget, error) {
	rows, err := s.st.Q().ListUserNotifyTargets(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("user target store: list targets: %w", store.MapError(err))
	}
	return rows, nil
}

// ListEnabledTargets lists all enabled targets for a user.
func (s *UserTargetStore) ListEnabledTargets(ctx context.Context, userID uuid.UUID) ([]sqlcgen.UserNotifyTarget, error) {
	rows, err := s.st.Q().ListEnabledUserNotifyTargets(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("user target store: list enabled targets: %w", store.MapError(err))
	}
	return rows, nil
}

// DeleteTarget deletes a user target.
func (s *UserTargetStore) DeleteTarget(ctx context.Context, id, userID uuid.UUID) error {
	n, err := s.st.Q().DeleteUserNotifyTarget(ctx, sqlcgen.DeleteUserNotifyTargetParams{
		ID:     id,
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("user target store: delete target: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("%w: user target %s not found", domain.ErrNotFound, id)
	}
	return nil
}
