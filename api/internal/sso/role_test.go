// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package sso_test

import (
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/sso"
)

func TestResolveRole(t *testing.T) {
	t.Parallel()

	adminList := []string{"snatcharr-admins", "ops"}
	viewerList := []string{"snatcharr-viewers", "guests"}

	tests := []struct {
		name     string
		groups   []string
		admin    []string
		viewer   []string
		wantRole domain.Role
		wantOK   bool
	}{
		{
			name:     "positive admin only",
			groups:   []string{"snatcharr-admins"},
			admin:    adminList,
			viewer:   viewerList,
			wantRole: domain.RoleAdmin,
			wantOK:   true,
		},
		{
			name:     "positive viewer only",
			groups:   []string{"snatcharr-viewers"},
			admin:    adminList,
			viewer:   viewerList,
			wantRole: domain.RoleViewer,
			wantOK:   true,
		},
		{
			name:     "positive admin wins over viewer",
			groups:   []string{"snatcharr-viewers", "snatcharr-admins"},
			admin:    adminList,
			viewer:   viewerList,
			wantRole: domain.RoleAdmin,
			wantOK:   true,
		},
		{
			name:     "positive second admin group",
			groups:   []string{"other", "ops"},
			admin:    adminList,
			viewer:   viewerList,
			wantRole: domain.RoleAdmin,
			wantOK:   true,
		},
		{
			name:     "positive second viewer group",
			groups:   []string{"other", "guests"},
			admin:    adminList,
			viewer:   viewerList,
			wantRole: domain.RoleViewer,
			wantOK:   true,
		},
		{
			name:     "negative no matching group",
			groups:   []string{"unknown", "staff"},
			admin:    adminList,
			viewer:   viewerList,
			wantRole: "",
			wantOK:   false,
		},
		{
			name:     "negative nil groups",
			groups:   nil,
			admin:    adminList,
			viewer:   viewerList,
			wantRole: "",
			wantOK:   false,
		},
		{
			name:     "negative empty groups",
			groups:   []string{},
			admin:    adminList,
			viewer:   viewerList,
			wantRole: "",
			wantOK:   false,
		},
		{
			name:     "boundary case sensitivity admin mismatch",
			groups:   []string{"Snatcharr-Admins"},
			admin:    adminList,
			viewer:   viewerList,
			wantRole: "",
			wantOK:   false,
		},
		{
			name:     "boundary case sensitivity viewer mismatch",
			groups:   []string{"SNATCHARR-VIEWERS"},
			admin:    adminList,
			viewer:   viewerList,
			wantRole: "",
			wantOK:   false,
		},
		{
			name:     "boundary empty segments in groups ignored",
			groups:   []string{"", "  ", "ops"},
			admin:    adminList,
			viewer:   viewerList,
			wantRole: domain.RoleAdmin,
			wantOK:   true,
		},
		{
			name:     "boundary only empty strings in groups",
			groups:   []string{"", " "},
			admin:    adminList,
			viewer:   viewerList,
			wantRole: "",
			wantOK:   false,
		},
		{
			name:     "negative nil configured lists",
			groups:   []string{"ops"},
			admin:    nil,
			viewer:   nil,
			wantRole: "",
			wantOK:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			role, ok := sso.ResolveRole(tc.groups, tc.admin, tc.viewer)
			if ok != tc.wantOK {
				t.Fatalf("ResolveRole() ok = %v, want %v", ok, tc.wantOK)
			}
			if role != tc.wantRole {
				t.Fatalf("ResolveRole() role = %q, want %q", role, tc.wantRole)
			}
		})
	}
}
