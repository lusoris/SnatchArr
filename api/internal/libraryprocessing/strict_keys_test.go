// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package libraryprocessing

import (
	"errors"
	"slices"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestDecodeRuleJSONStrictKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "canonical lowercase", raw: `{"track":{"languages":["en","de"]}}`, want: []string{"en", "de"}},
		{name: "canonical with surrounding whitespace", raw: " \n{\"track\":{\"languages\":[\"en\"]}}\n ", want: []string{"en"}},
		{name: "escaped canonical keys", raw: `{"\u0074rack":{"\u006canguages":["en"]}}`, want: []string{"en"}},
		{name: "only outer key case wrong", raw: `{"Track":{"languages":["en"]}}`, wantErr: true},
		{name: "only inner key case wrong", raw: `{"track":{"Languages":["en"]}}`, wantErr: true},
		{name: "mixed spelling together", raw: `{"TRACK":{"LANGUAGES":["en"]}}`, wantErr: true},
		{name: "mixed canonical outer wrong inner", raw: `{"track":{"languages":["en"],"LANGUAGES":["de"]}}`, wantErr: true},
		{name: "unknown nested field", raw: `{"track":{"languages":["en"],"extra":true}}`, wantErr: true},
		{name: "unknown outer field", raw: `{"track":{"languages":["en"]},"extra":true}`, wantErr: true},
		{name: "null document", raw: `null`, wantErr: true},
		{name: "missing track", raw: `{}`, wantErr: true},
		{name: "null track", raw: `{"track":null}`, wantErr: true},
		{name: "missing languages", raw: `{"track":{}}`, wantErr: true},
		{name: "null languages", raw: `{"track":{"languages":null}}`, wantErr: true},
		{name: "trailing object", raw: `{"track":{"languages":["en"]}} {}`, wantErr: true},
		{name: "trailing garbage", raw: `{"track":{"languages":["en"]}}x`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := DecodeRuleJSON([]byte(tt.raw))
			if tt.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("error = %v, want domain.ErrInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got.Track.Languages, tt.want) {
				t.Fatalf("languages = %v, want %v", got.Track.Languages, tt.want)
			}
		})
	}
}
