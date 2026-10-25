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
	requesterList := []string{"snatcharr-requesters", "family"}
	viewerList := []string{"snatcharr-viewers", "guests"}

	tests := []struct {
		name      string
		groups    []string
		admin     []string
		requester []string
		viewer    []string
		wantRole  domain.Role
		wantOK    bool
	}{
		{
			name:      "positive admin only",
			groups:    []string{"snatcharr-admins"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  domain.RoleAdmin,
			wantOK:    true,
		},
		{
			name:      "positive requester only",
			groups:    []string{"snatcharr-requesters"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  domain.RoleRequester,
			wantOK:    true,
		},
		{
			name:      "positive viewer only",
			groups:    []string{"snatcharr-viewers"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  domain.RoleViewer,
			wantOK:    true,
		},
		{
			name:      "positive admin wins over requester",
			groups:    []string{"snatcharr-requesters", "snatcharr-admins"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  domain.RoleAdmin,
			wantOK:    true,
		},
		{
			name:      "positive admin wins over requester and viewer",
			groups:    []string{"snatcharr-viewers", "family", "ops"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  domain.RoleAdmin,
			wantOK:    true,
		},
		{
			name:      "positive requester wins over viewer",
			groups:    []string{"snatcharr-viewers", "family"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  domain.RoleRequester,
			wantOK:    true,
		},
		{
			name:      "positive second requester group",
			groups:    []string{"other", "family"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  domain.RoleRequester,
			wantOK:    true,
		},
		{
			name:      "positive second viewer group",
			groups:    []string{"other", "guests"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  domain.RoleViewer,
			wantOK:    true,
		},
		{
			name:      "negative no matching group",
			groups:    []string{"unknown", "staff"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  "",
			wantOK:    false,
		},
		{
			name:      "negative nil groups",
			groups:    nil,
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  "",
			wantOK:    false,
		},
		{
			name:      "negative empty groups",
			groups:    []string{},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  "",
			wantOK:    false,
		},
		{
			name:      "boundary case sensitivity admin mismatch",
			groups:    []string{"Snatcharr-Admins"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  "",
			wantOK:    false,
		},
		{
			name:      "boundary case sensitivity requester mismatch",
			groups:    []string{"Requesters", "Snatcharr-Requesters"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  "",
			wantOK:    false,
		},
		{
			name:      "boundary case sensitivity viewer mismatch",
			groups:    []string{"SNATCHARR-VIEWERS"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  "",
			wantOK:    false,
		},
		{
			name:      "boundary empty segments in groups ignored",
			groups:    []string{"", "  ", "ops"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  domain.RoleAdmin,
			wantOK:    true,
		},
		{
			name:      "boundary empty string group in requester ignored",
			groups:    []string{"", "family"},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  domain.RoleRequester,
			wantOK:    true,
		},
		{
			name:      "boundary only empty strings in groups",
			groups:    []string{"", " "},
			admin:     adminList,
			requester: requesterList,
			viewer:    viewerList,
			wantRole:  "",
			wantOK:    false,
		},
		{
			name:      "negative nil configured lists",
			groups:    []string{"ops", "family"},
			admin:     nil,
			requester: nil,
			viewer:    nil,
			wantRole:  "",
			wantOK:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			role, ok := sso.ResolveRole(tc.groups, tc.admin, tc.requester, tc.viewer)
			if ok != tc.wantOK {
				t.Fatalf("ResolveRole() ok = %v, want %v", ok, tc.wantOK)
			}
			if role != tc.wantRole {
				t.Fatalf("ResolveRole() role = %q, want %q", role, tc.wantRole)
			}
		})
	}
}
