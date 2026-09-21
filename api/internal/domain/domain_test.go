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

func TestParseAppKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		want    domain.AppKind
		wantErr bool
	}{
		{"positive sonarr", "sonarr", domain.KindSonarr, false},
		{"positive whisparr v3", "whisparr_v3", domain.KindWhisparrV3, false},
		{"negative unknown", "plex", "", true},
		{"boundary empty", "", "", true},
		{"boundary case-sensitive", "Sonarr", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseAppKind(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"positive plain", "http://sonarr:8989", "http://sonarr:8989", false},
		{"positive strips trailing slash and query", "HTTPS://Sonarr.Local:8989/base/?x=1#f", "https://sonarr.local:8989/base", false},
		{"positive adds scheme", "sonarr:8989", "http://sonarr:8989", false},
		{"positive whitespace", "  http://a  ", "http://a", false},
		{"negative empty", "", "", true},
		{"negative bad scheme", "ftp://sonarr", "", true},
		{"boundary only scheme", "http://", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := domain.NormalizeBaseURL(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateInstanceInput(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("n", 65)
	tests := []struct {
		name    string
		kind    domain.AppKind
		inst    string
		url     string
		key     string
		wantErr bool
	}{
		{"positive", domain.KindRadarr, "Movies", "http://radarr:7878", "0123456789abcdef", false},
		{"negative kind", "plex", "x", "http://a", "0123456789abcdef", true},
		{"negative empty name", domain.KindRadarr, "  ", "http://a", "0123456789abcdef", true},
		{"boundary 64-char name ok", domain.KindRadarr, long[:64], "http://a", "0123456789abcdef", false},
		{"boundary 65-char name", domain.KindRadarr, long, "http://a", "0123456789abcdef", true},
		{"boundary 8-char key ok", domain.KindRadarr, "x", "http://a", "12345678", false},
		{"boundary 7-char key", domain.KindRadarr, "x", "http://a", "1234567", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := domain.ValidateInstanceInput(tc.kind, tc.inst, tc.url, tc.key)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateUsernameAndPassword(t *testing.T) {
	t.Parallel()
	if err := domain.ValidateUsername("ada.lovelace-1"); err != nil {
		t.Fatalf("positive username: %v", err)
	}
	if err := domain.ValidateUsername("ab"); err == nil {
		t.Fatal("boundary 2-char username should fail")
	}
	if err := domain.ValidateUsername("has space"); err == nil {
		t.Fatal("negative username with space should fail")
	}
	if err := domain.ValidatePassword(strings.Repeat("x", domain.MinPasswordLen)); err != nil {
		t.Fatalf("boundary min password: %v", err)
	}
	if err := domain.ValidatePassword(strings.Repeat("x", domain.MinPasswordLen-1)); err == nil {
		t.Fatal("boundary below-min password should fail")
	}
	if err := domain.ValidatePassword(strings.Repeat("x", domain.MaxPasswordLen+1)); err == nil {
		t.Fatal("boundary above-max password should fail")
	}
}
