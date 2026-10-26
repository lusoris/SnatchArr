// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain_test

import (
	"errors"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestValidateProwlarrHourlyCap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		cap     int
		wantErr error
	}{
		{"positive default 15", 15, nil},
		{"positive custom 50", 50, nil},
		{"boundary min 1", 1, nil},
		{"boundary max 500", 500, nil},
		{"negative zero rejected", 0, domain.ErrInvalid},
		{"negative below zero rejected", -1, domain.ErrInvalid},
		{"negative 501 rejected", 501, domain.ErrInvalid},
		{"negative 1000 rejected", 1000, domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := domain.ValidateProwlarrHourlyCap(tc.cap)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("ValidateProwlarrHourlyCap(%d) err = %v, want nil", tc.cap, err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateProwlarrHourlyCap(%d) err = %v, want %v", tc.cap, err, tc.wantErr)
			}
		})
	}
}

func TestValidateProwlarrLinkInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		link    string
		base    string
		key     string
		cap     int
		wantErr error
	}{
		{"positive valid link", "prowlarr", "http://127.0.0.1:9696", "12345678", 15, nil},
		{"negative bad cap 0", "prowlarr", "http://127.0.0.1:9696", "12345678", 0, domain.ErrInvalid},
		{"negative bad cap 501", "prowlarr", "http://127.0.0.1:9696", "12345678", 501, domain.ErrInvalid},
		{"negative bad name", "", "http://127.0.0.1:9696", "12345678", 15, domain.ErrInvalid},
		{"negative bad url", "prowlarr", "ftp://prowlarr", "12345678", 15, domain.ErrInvalid},
		{"negative bad key", "prowlarr", "http://127.0.0.1:9696", "short", 15, domain.ErrInvalid},
		{"boundary min cap 1", "prowlarr", "http://127.0.0.1:9696", "12345678", 1, nil},
		{"boundary max cap 500", "prowlarr", "http://127.0.0.1:9696", "12345678", 500, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := domain.ValidateProwlarrLinkInput(tc.link, tc.base, tc.key, tc.cap)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("ValidateProwlarrLinkInput err = %v, want nil", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateProwlarrLinkInput err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
