// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestPriorityPresetInputName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want error
	}{
		{"", domain.ErrInvalid},
		{"a", nil},
		{strings.Repeat("a", 40), nil},
		{strings.Repeat("a", 41), domain.ErrInvalid},
		{strings.Repeat("ä", 40), nil},
		{"   ", domain.ErrInvalid},
		{"night", nil},
		{"MEDIA_FIRST", domain.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := domain.PriorityPresetInput{Name: tc.name, Search: domain.MediaFirstPreset(), Download: domain.MediaFirstPreset()}
			if err := in.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate(%q) = %v; want %v", tc.name, err, tc.want)
			}
		})
	}
}

func TestPriorityPresetInputMaps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		search   domain.PriorityPreset
		download domain.PriorityPreset
		want     error
	}{
		{"complete", domain.MediaFirstPreset(), domain.MediaFirstPreset(), nil},
		{"missing search", nil, domain.MediaFirstPreset(), domain.ErrInvalid},
		{"missing download", domain.MediaFirstPreset(), nil, domain.ErrInvalid},
		{"unknown kind", domain.PriorityPreset{"unknown": 10}, domain.MediaFirstPreset(), domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := domain.PriorityPresetInput{Name: "night", Search: tc.search, Download: tc.download}
			if err := in.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v; want %v", err, tc.want)
			}
		})
	}
}
