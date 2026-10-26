// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package requests

import (
	"fmt"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// VotingMode specifies the voting calculation model used for community requests.
type VotingMode string

// Supported voting modes.
const (
	VotingModeUpvotesOnly VotingMode = "upvotes_only"
	VotingModeNetScore    VotingMode = "net_score"
)

// Visibility defines how community requests and requester identities are displayed to users.
type Visibility string

// Supported visibility modes.
const (
	VisibilityPublicAttributed Visibility = "public_attributed"
	VisibilityPublicAnonymous  Visibility = "public_anonymous"
	VisibilityPrivate          Visibility = "private_to_requester_and_admin"
)

// Settings holds runtime configuration for the community requests module.
type Settings struct {
	VotingMode       VotingMode `json:"voting_mode"`
	AllowViewerVotes bool       `json:"allow_viewer_votes"`
	Visibility       Visibility `json:"visibility"`
	VotingTTLDays    int        `json:"voting_ttl_days"`
}

// DefaultSettings returns operator-decided default settings for community requests.
// Defaults are governed by decision ledgers snatcharr-oq-0036-2, snatcharr-oq-0036-4,
// snatcharr-oq-0036-11, snatcharr-oq-0036-13, and snatcharr-oq-0036-defaults-20261026.
func DefaultSettings() Settings {
	return Settings{
		VotingMode:       VotingModeUpvotesOnly,
		AllowViewerVotes: true,
		Visibility:       VisibilityPublicAttributed,
		VotingTTLDays:    0,
	}
}

// Validate checks that settings fields contain supported values within valid bounds.
func (s Settings) Validate() error {
	switch s.VotingMode {
	case VotingModeUpvotesOnly, VotingModeNetScore:
	default:
		return fmt.Errorf("%w: unknown voting_mode %q", domain.ErrInvalid, s.VotingMode)
	}

	switch s.Visibility {
	case VisibilityPublicAttributed, VisibilityPublicAnonymous, VisibilityPrivate:
	default:
		return fmt.Errorf("%w: unknown visibility %q", domain.ErrInvalid, s.Visibility)
	}

	if s.VotingTTLDays < 0 {
		return fmt.Errorf("%w: voting_ttl_days must not be negative", domain.ErrInvalid)
	}

	return nil
}

// MayVote reports whether a user with the given role is permitted to vote on requests.
// Administrators and requesters are always permitted to vote. Viewers may vote only
// when AllowViewerVotes is enabled. Unknown or invalid roles are rejected.
func (s Settings) MayVote(role domain.Role) bool {
	switch role {
	case domain.RoleAdmin, domain.RoleRequester:
		return true
	case domain.RoleViewer:
		return s.AllowViewerVotes
	default:
		return false
	}
}
