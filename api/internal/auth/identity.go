// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// LinkIdentity links an external identity (jellyfin or proxy) to an existing user account.
// Refuses domain.ErrConflict when the identity already belongs to any other account.
// Linking the same user or linking an identity that is the user's primary external_id returns nil.
func (s *Service) LinkIdentity(ctx context.Context, userID uuid.UUID, provider, subject string) error {
	provider = strings.TrimSpace(provider)
	subject = strings.TrimSpace(subject)
	if !domain.ValidIdentityProvider(provider) || subject == "" {
		return fmt.Errorf("%w: invalid identity provider %q or empty subject", domain.ErrInvalid, provider)
	}
	extID := externalIDForProvider(provider, subject)
	if isOwn, err := s.checkExternalIDOwnership(ctx, userID, extID); err != nil {
		return err
	} else if isOwn {
		return nil
	}
	if isOwn, err := s.checkUserExistingProviderLink(ctx, userID, provider, subject); err != nil {
		return err
	} else if isOwn {
		return nil
	}
	if isOwn, err := s.checkIdentityLinkOwnership(ctx, userID, provider, subject); err != nil {
		return err
	} else if isOwn {
		return nil
	}
	return s.insertIdentity(ctx, userID, provider, subject)
}

func (s *Service) checkExternalIDOwnership(ctx context.Context, userID uuid.UUID, extID string) (bool, error) {
	extUser, err := s.st.Q().GetUserByExternalID(ctx, &extID)
	if err == nil {
		if extUser.ID != userID {
			return false, fmt.Errorf("%w: identity belongs to another account", domain.ErrConflict)
		}
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("auth: check external id: %w", store.MapError(err))
}

func (s *Service) checkUserExistingProviderLink(ctx context.Context, userID uuid.UUID, provider, subject string) (bool, error) {
	rows, err := s.st.Q().ListUserIdentities(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("auth: check user provider link: %w", store.MapError(err))
	}
	for _, r := range rows {
		if r.Provider == provider {
			if r.Subject == subject {
				return true, nil
			}
			return false, fmt.Errorf("%w: account already linked to another %s identity; unlink first", domain.ErrConflict, provider)
		}
	}
	return false, nil
}

func (s *Service) checkIdentityLinkOwnership(ctx context.Context, userID uuid.UUID, provider, subject string) (bool, error) {
	existing, err := s.st.Q().GetUserIdentity(ctx, sqlcgen.GetUserIdentityParams{
		Provider: provider,
		Subject:  subject,
	})
	if err == nil {
		if existing.UserID != userID {
			return false, fmt.Errorf("%w: identity linked to another account", domain.ErrConflict)
		}
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("auth: check user identity: %w", store.MapError(err))
}

func (s *Service) insertIdentity(ctx context.Context, userID uuid.UUID, provider, subject string) error {
	now := s.clk.Now()
	_, err := s.st.Q().InsertUserIdentity(ctx, sqlcgen.InsertUserIdentityParams{
		Provider:  provider,
		Subject:   subject,
		UserID:    userID,
		CreatedAt: now,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if isOwn, checkErr := s.checkIdentityLinkOwnership(ctx, userID, provider, subject); checkErr != nil {
				return checkErr
			} else if isOwn {
				return nil
			}
		}
		return fmt.Errorf("auth: insert user identity: %w", store.MapError(err))
	}
	return nil
}

func externalIDForProvider(provider, subject string) string {
	if provider == domain.IdentityProviderJellyfin {
		return "jellyfin:" + subject
	}
	return subject
}

// LinkedUser returns the user associated with a linked provider and subject, if any.
func (s *Service) LinkedUser(ctx context.Context, provider, subject string) (domain.User, bool, error) {
	provider = strings.TrimSpace(provider)
	subject = strings.TrimSpace(subject)
	if provider == "" || subject == "" {
		return domain.User{}, false, nil
	}
	row, err := s.st.Q().GetUserIdentity(ctx, sqlcgen.GetUserIdentityParams{
		Provider: provider,
		Subject:  subject,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, false, nil
		}
		return domain.User{}, false, fmt.Errorf("auth: get user identity: %w", store.MapError(err))
	}
	u, err := s.User(ctx, row.UserID)
	if err != nil {
		return domain.User{}, false, err
	}
	return u, true, nil
}

// ListIdentities returns the linked external identities for userID ordered by provider.
func (s *Service) ListIdentities(ctx context.Context, userID uuid.UUID) ([]domain.UserIdentity, error) {
	rows, err := s.st.Q().ListUserIdentities(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("auth: list user identities: %w", store.MapError(err))
	}
	items := make([]domain.UserIdentity, 0, len(rows))
	for _, r := range rows {
		items = append(items, domain.UserIdentity{
			Provider:  r.Provider,
			Subject:   r.Subject,
			UserID:    r.UserID,
			CreatedAt: r.CreatedAt,
		})
	}
	return items, nil
}

// UnlinkIdentity deletes a linked identity. Returns domain.ErrNotFound when no row was deleted.
func (s *Service) UnlinkIdentity(ctx context.Context, userID uuid.UUID, provider string) error {
	provider = strings.TrimSpace(provider)
	n, err := s.st.Q().DeleteUserIdentity(ctx, sqlcgen.DeleteUserIdentityParams{
		UserID:   userID,
		Provider: provider,
	})
	if err != nil {
		return fmt.Errorf("auth: delete user identity: %w", store.MapError(err))
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
