// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package sso

import (
	"slices"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// ResolveRole maps user groups to a domain.Role based on admin, requester, and viewer lists.
// Admin membership takes precedence over requester, which takes precedence over viewer.
// Group matching is exact and case-sensitive; empty group strings are ignored.
// If no role matches, or if groups is empty or nil, it returns ("", false).
func ResolveRole(groups, adminGroups, requesterGroups, viewerGroups []string) (domain.Role, bool) {
	switch {
	case memberOfAny(groups, adminGroups):
		return domain.RoleAdmin, true
	case memberOfAny(groups, requesterGroups):
		return domain.RoleRequester, true
	case memberOfAny(groups, viewerGroups):
		return domain.RoleViewer, true
	default:
		return "", false
	}
}

// memberOfAny reports whether a non-empty group of groups appears in configured.
func memberOfAny(groups, configured []string) bool {
	for _, g := range groups {
		if g != "" && slices.Contains(configured, g) {
			return true
		}
	}
	return false
}
