// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package sso

import (
	"slices"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// ResolveRole maps user groups to a domain.Role based on admin and viewer lists.
// Admin membership takes precedence over viewer membership.
// Group matching is exact and case-sensitive.
// If neither role matches, or if groups is empty or nil, it returns ("", false).
func ResolveRole(groups []string, adminGroups, viewerGroups []string) (domain.Role, bool) {
	if len(groups) == 0 {
		return "", false
	}
	for _, g := range groups {
		if g != "" && slices.Contains(adminGroups, g) {
			return domain.RoleAdmin, true
		}
	}
	for _, g := range groups {
		if g != "" && slices.Contains(viewerGroups, g) {
			return domain.RoleViewer, true
		}
	}
	return "", false
}
