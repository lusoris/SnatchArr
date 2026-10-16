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

func TestValidateAPIKeyName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "positive simple", input: "my-key", want: "my-key", wantErr: false},
		{name: "positive trimmed", input: "  my-key  ", want: "my-key", wantErr: false},
		{name: "negative empty", input: "", want: "", wantErr: true},
		{name: "negative blank spaces", input: "    ", want: "", wantErr: true},
		{name: "boundary min 1 char", input: "k", want: "k", wantErr: false},
		{name: "boundary max 100 chars", input: strings.Repeat("a", 100), want: strings.Repeat("a", 100), wantErr: false},
		{name: "negative 101 chars", input: strings.Repeat("a", 101), want: "", wantErr: true},
		{name: "boundary 100 umlauts ok", input: strings.Repeat("ä", 100), want: strings.Repeat("ä", 100), wantErr: false},
		{name: "negative 101 umlauts err", input: strings.Repeat("ä", 101), want: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ValidateAPIKeyName(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateAPIKeyName(%q) err = %v, wantErr %v", tc.input, err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, domain.ErrBadRequest) {
				t.Fatalf("ValidateAPIKeyName(%q) err = %v, want ErrBadRequest", tc.input, err)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("ValidateAPIKeyName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
