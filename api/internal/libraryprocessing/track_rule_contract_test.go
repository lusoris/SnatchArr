// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package libraryprocessing_test

import (
	"fmt"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

// distinctLanguages returns n distinct placeholder strings. The schema
// assertion below covers cardinality and uniqueness only, not code format.
func distinctLanguages(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("l%02d", i)
	}
	return out
}

func TestTrackRuleValidateLanguages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		languages []string
		wantErr   bool
	}{
		{name: "empty", languages: []string{}, wantErr: true},
		{name: "one", languages: distinctLanguages(1), wantErr: false},
		{name: "exactly limit", languages: distinctLanguages(libraryprocessing.MaxRuleLanguages), wantErr: false},
		{name: "one over limit", languages: distinctLanguages(libraryprocessing.MaxRuleLanguages + 1), wantErr: true},
		{name: "duplicate", languages: []string{"l00", "l01", "l00"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rule := &oas.TrackRule{Languages: tt.languages}
			err := rule.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
