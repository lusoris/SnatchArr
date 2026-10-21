// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/auth/session"

	"github.com/lusoris/SnatchArr/api/internal/store"
)

// RenewSession signs userID in under a fresh session id and deletes the
// request's previous session row, so an id known before the sign-in is
// never authenticated (session fixation, #268). The new id replaces the
// old one in the response cookie. golusoris auth/session gains
// Manager.Renew for this (golusoris/golusoris#931, after v0.17.0); this
// body moves onto it once a golusoris release carries it.
func (s *Service) RenewSession(ctx context.Context, m *session.Manager, w http.ResponseWriter, r *http.Request, userID uuid.UUID) error {
	return s.renewSession(ctx, m, w, r, userID, nil)
}

func (s *Service) renewSession(ctx context.Context, m *session.Manager, w http.ResponseWriter, r *http.Request, userID uuid.UUID, origin *SSOOrigin) error {
	old, err := m.Load(r)
	if err != nil {
		return fmt.Errorf("auth: load session: %w", err)
	}
	if delErr := s.st.Q().DeleteSession(ctx, old.ID); delErr != nil {
		return fmt.Errorf("auth: delete previous session: %w", store.MapError(delErr))
	}
	// Without the cookie, Load hands out an empty session under a new random id.
	anon := r.Clone(ctx)
	anon.Header.Del("Cookie")
	fresh, err := m.Load(anon)
	if err != nil {
		return fmt.Errorf("auth: new session: %w", err)
	}
	fresh.Set(SessionUserKey, userID.String())
	if origin != nil {
		if err := setSSOOrigin(fresh, *origin); err != nil {
			return err
		}
	}
	if err := m.SaveContext(ctx, w, fresh); err != nil {
		return fmt.Errorf("auth: save session: %w", err)
	}
	return nil
}

func setSSOOrigin(fresh *session.Session, origin SSOOrigin) error {
	groups := origin.Groups
	if groups == nil {
		groups = []string{}
	}
	rawGroups, err := json.Marshal(groups)
	if err != nil {
		return fmt.Errorf("auth: encode sso_groups: %w", err)
	}
	fresh.Set(SessionSSOSourceKey, origin.Source)
	fresh.Set(SessionSSOSubjectKey, origin.Subject)
	fresh.Set(SessionSSOGroupsKey, string(rawGroups))
	fresh.Set(SessionSSORoleKey, string(origin.Role))
	return nil
}
