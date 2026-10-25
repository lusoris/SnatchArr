// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"testing"
)

func TestBuildItemParams(t *testing.T) {
	t.Parallel()

	params := buildItemParams("user-42", 10, 50)

	if got := params.Get("userId"); got != "user-42" {
		t.Errorf("userId = %q, want user-42", got)
	}
	if got := params.Get("Recursive"); got != "true" {
		t.Errorf("Recursive = %q, want true", got)
	}
	if got := params.Get("IncludeItemTypes"); got != "Episode,Movie,Series" {
		t.Errorf("IncludeItemTypes = %q, want Episode,Movie,Series", got)
	}
	if got := params.Get("Fields"); got != "ProviderIds" {
		t.Errorf("Fields = %q, want ProviderIds", got)
	}
	if got := params.Get("EnableUserData"); got != "true" {
		t.Errorf("EnableUserData = %q, want true", got)
	}
	if got := params.Get("StartIndex"); got != "10" {
		t.Errorf("StartIndex = %q, want 10", got)
	}
	if got := params.Get("Limit"); got != "50" {
		t.Errorf("Limit = %q, want 50", got)
	}
}

func TestExtractTvdbID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		providers map[string]string
		want      string
	}{
		{
			name:      "exact casing Tvdb",
			providers: map[string]string{"Tvdb": "12345"},
			want:      "12345",
		},
		{
			name:      "lowercase tvdb",
			providers: map[string]string{"tvdb": "67890"},
			want:      "67890",
		},
		{
			name:      "uppercase TVDB",
			providers: map[string]string{"TVDB": "11223"},
			want:      "11223",
		},
		{
			name:      "multiple providers with mixed casing",
			providers: map[string]string{"Imdb": "tt123456", "tvdb": "99999", "Tmdb": "888"},
			want:      "99999",
		},
		{
			name:      "nil map",
			providers: nil,
			want:      "",
		},
		{
			name:      "empty map",
			providers: map[string]string{},
			want:      "",
		},
		{
			name:      "other providers only",
			providers: map[string]string{"Imdb": "tt123", "Tmdb": "456"},
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractTvdbID(tt.providers)
			if got != tt.want {
				t.Errorf("extractTvdbID(%v) = %q, want %q", tt.providers, got, tt.want)
			}
		})
	}
}
