// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package requests_test

import (
	"errors"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/requests"
)

func TestDefaultSettings(t *testing.T) {
	t.Parallel()

	got := requests.DefaultSettings()

	if got.VotingMode != requests.VotingModeUpvotesOnly {
		t.Fatalf("DefaultSettings().VotingMode = %q, want %q", got.VotingMode, requests.VotingModeUpvotesOnly)
	}
	if !got.AllowViewerVotes {
		t.Fatalf("DefaultSettings().AllowViewerVotes = %v, want true", got.AllowViewerVotes)
	}
	if got.Visibility != requests.VisibilityPublicAttributed {
		t.Fatalf("DefaultSettings().Visibility = %q, want %q", got.Visibility, requests.VisibilityPublicAttributed)
	}
	if got.VotingTTLDays != 0 {
		t.Fatalf("DefaultSettings().VotingTTLDays = %d, want 0", got.VotingTTLDays)
	}
}

func TestSettings_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*requests.Settings)
		wantErr bool
	}{
		{
			name:    "positive default settings",
			mutate:  nil,
			wantErr: false,
		},
		{
			name: "boundary ttl zero",
			mutate: func(s *requests.Settings) {
				s.VotingTTLDays = 0
			},
			wantErr: false,
		},
		{
			name: "boundary ttl one day",
			mutate: func(s *requests.Settings) {
				s.VotingTTLDays = 1
			},
			wantErr: false,
		},
		{
			name: "positive net score mode",
			mutate: func(s *requests.Settings) {
				s.VotingMode = requests.VotingModeNetScore
			},
			wantErr: false,
		},
		{
			name: "positive public anonymous visibility",
			mutate: func(s *requests.Settings) {
				s.Visibility = requests.VisibilityPublicAnonymous
			},
			wantErr: false,
		},
		{
			name: "positive private visibility",
			mutate: func(s *requests.Settings) {
				s.Visibility = requests.VisibilityPrivate
			},
			wantErr: false,
		},
		{
			name: "boundary negative ttl minus one",
			mutate: func(s *requests.Settings) {
				s.VotingTTLDays = -1
			},
			wantErr: true,
		},
		{
			name: "negative empty mode",
			mutate: func(s *requests.Settings) {
				s.VotingMode = ""
			},
			wantErr: true,
		},
		{
			name: "negative unknown mode",
			mutate: func(s *requests.Settings) {
				s.VotingMode = "super_vote"
			},
			wantErr: true,
		},
		{
			name: "negative unknown visibility x",
			mutate: func(s *requests.Settings) {
				s.Visibility = "x"
			},
			wantErr: true,
		},
		{
			name: "negative empty visibility",
			mutate: func(s *requests.Settings) {
				s.Visibility = ""
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := requests.DefaultSettings()
			if tc.mutate != nil {
				tc.mutate(&s)
			}
			err := s.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error, got nil")
				}
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("Validate() error %v does not wrap domain.ErrInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestSettings_MayVote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		allowViewerVotes bool
		role             domain.Role
		want             bool
	}{
		{
			name:             "positive admin with viewer votes enabled",
			allowViewerVotes: true,
			role:             domain.RoleAdmin,
			want:             true,
		},
		{
			name:             "positive admin with viewer votes disabled",
			allowViewerVotes: false,
			role:             domain.RoleAdmin,
			want:             true,
		},
		{
			name:             "positive requester with viewer votes enabled",
			allowViewerVotes: true,
			role:             domain.RoleRequester,
			want:             true,
		},
		{
			name:             "positive requester with viewer votes disabled",
			allowViewerVotes: false,
			role:             domain.RoleRequester,
			want:             true,
		},
		{
			name:             "positive viewer with viewer votes enabled",
			allowViewerVotes: true,
			role:             domain.RoleViewer,
			want:             true,
		},
		{
			name:             "negative viewer with viewer votes disabled",
			allowViewerVotes: false,
			role:             domain.RoleViewer,
			want:             false,
		},
		{
			name:             "negative unknown role with viewer votes enabled",
			allowViewerVotes: true,
			role:             domain.Role("guest"),
			want:             false,
		},
		{
			name:             "negative unknown role with viewer votes disabled",
			allowViewerVotes: false,
			role:             domain.Role("guest"),
			want:             false,
		},
		{
			name:             "boundary empty role",
			allowViewerVotes: true,
			role:             domain.Role(""),
			want:             false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := requests.Settings{AllowViewerVotes: tc.allowViewerVotes}
			got := s.MayVote(tc.role)
			if got != tc.want {
				t.Fatalf("MayVote(%q) with allow_viewer_votes=%v: got %v, want %v", tc.role, tc.allowViewerVotes, got, tc.want)
			}
		})
	}
}
