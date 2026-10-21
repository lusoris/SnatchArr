// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/auth/session"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
)

// Session data keys for SSO session tracking.
const (
	SessionSSOSourceKey  = "sso_source"
	SessionSSOSubjectKey = "sso_subject"
	SessionSSOGroupsKey  = "sso_groups"
	SessionSSORoleKey    = "sso_role"
)

// SSOOrigin records the single sign-on origin and group state of a session.
type SSOOrigin struct {
	Source  string
	Subject string
	Groups  []string
	Role    domain.Role
}

// RenewSSOSession signs userID in under a fresh session id, deletes the previous session row,
// and records SSOOrigin attributes on the new session.
func (s *Service) RenewSSOSession(ctx context.Context, m *session.Manager, w http.ResponseWriter, r *http.Request, userID uuid.UUID, origin SSOOrigin) error {
	return s.renewSession(ctx, m, w, r, userID, &origin)
}

// RevokeSession deletes the session row identified by sessID.
func (s *Service) RevokeSession(ctx context.Context, sessID string) error {
	if err := s.st.Q().DeleteSession(ctx, sessID); err != nil {
		return fmt.Errorf("auth: revoke session: %w", store.MapError(err))
	}
	return nil
}

// SSOOriginFrom extracts SSOOrigin from sess.
// It returns ok=false when no sso_source is present, or an error if present but malformed.
func SSOOriginFrom(sess *session.Session) (SSOOrigin, bool, error) {
	if sess == nil {
		return SSOOrigin{}, false, nil
	}
	rawSource := sess.Get(SessionSSOSourceKey)
	if rawSource == nil {
		return SSOOrigin{}, false, nil
	}
	source, ok := rawSource.(string)
	if !ok || source == "" {
		return SSOOrigin{}, false, errors.New("auth: malformed sso_source")
	}
	subject, ok := sess.Get(SessionSSOSubjectKey).(string)
	if !ok {
		return SSOOrigin{}, false, errors.New("auth: malformed sso_subject")
	}
	groups, err := decodeGroups(sess.Get(SessionSSOGroupsKey))
	if err != nil {
		return SSOOrigin{}, false, err
	}
	role, err := validateRole(sess.Get(SessionSSORoleKey))
	if err != nil {
		return SSOOrigin{}, false, err
	}
	return SSOOrigin{
		Source:  source,
		Subject: subject,
		Groups:  groups,
		Role:    role,
	}, true, nil
}

func decodeGroups(raw any) ([]string, error) {
	groupsJSON, ok := raw.(string)
	if !ok {
		return nil, errors.New("auth: malformed sso_groups")
	}
	var groups []string
	if err := json.Unmarshal([]byte(groupsJSON), &groups); err != nil {
		return nil, fmt.Errorf("auth: decode sso_groups: %w", err)
	}
	return groups, nil
}

func validateRole(raw any) (domain.Role, error) {
	roleStr, ok := raw.(string)
	if !ok {
		return "", errors.New("auth: malformed sso_role")
	}
	role := domain.Role(roleStr)
	if role != domain.RoleAdmin && role != domain.RoleViewer {
		return "", errors.New("auth: invalid sso_role")
	}
	return role, nil
}
