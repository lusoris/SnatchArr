// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain_test

import (
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestRole_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role domain.Role
		want bool
	}{
		{name: "positive admin", role: domain.RoleAdmin, want: true},
		{name: "positive requester", role: domain.RoleRequester, want: true},
		{name: "positive viewer", role: domain.RoleViewer, want: true},
		{name: "negative empty string", role: "", want: false},
		{name: "negative unknown role", role: "operator", want: false},
		{name: "negative case-sensitive Admin", role: "Admin", want: false},
		{name: "negative case-sensitive Requester", role: "Requester", want: false},
		{name: "negative case-sensitive Viewer", role: "Viewer", want: false},
		{name: "boundary leading space", role: " requester", want: false},
		{name: "boundary trailing space", role: "requester ", want: false},
		{name: "boundary prefix only", role: "req", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.role.Valid()
			if got != tc.want {
				t.Fatalf("Role(%q).Valid() = %v, want %v", tc.role, got, tc.want)
			}
		})
	}
}

func TestRole_CanRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role domain.Role
		want bool
	}{
		{name: "positive admin", role: domain.RoleAdmin, want: true},
		{name: "positive requester", role: domain.RoleRequester, want: true},
		{name: "negative viewer", role: domain.RoleViewer, want: false},
		{name: "negative empty", role: "", want: false},
		{name: "negative unknown", role: "guest", want: false},
		{name: "boundary case-sensitive Requester", role: "Requester", want: false},
		{name: "boundary padded requester", role: " requester ", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.role.CanRequest()
			if got != tc.want {
				t.Fatalf("Role(%q).CanRequest() = %v, want %v", tc.role, got, tc.want)
			}
		})
	}
}
