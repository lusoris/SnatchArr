// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"errors"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestApplySettingAllRegistryDefaults(t *testing.T) {
	t.Parallel()
	o := Default()
	for _, item := range Registry() {
		if !stringsHasPrefix(item.Key, "snatcharr.") {
			continue
		}
		if item.Default == "" {
			continue
		}
		if err := ApplySetting(&o, item.Key, item.Default); err != nil {
			t.Errorf("ApplySetting(%q, %q): unexpected error %v", item.Key, item.Default, err)
		}
	}
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestApplySettingTableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		key     string
		val     string
		wantErr bool
		errIs   error
		check   func(t *testing.T, o Options)
	}{
		{
			name: "positive boolean",
			key:  "snatcharr.oidc.enabled",
			val:  "true",
			check: func(t *testing.T, o Options) {
				t.Helper()
				if !o.OIDC.Enabled {
					t.Errorf("expected oidc.enabled to be true")
				}
			},
		},
		{
			name: "positive duration",
			key:  "snatcharr.session.ttl",
			val:  "48h",
			check: func(t *testing.T, o Options) {
				t.Helper()
				if o.Session.TTL != 48*time.Hour {
					t.Errorf("expected session.ttl to be 48h, got %v", o.Session.TTL)
				}
			},
		},
		{
			name: "positive list",
			key:  "snatcharr.proxy.trusted",
			val:  "127.0.0.1, 10.0.0.0/8",
			check: func(t *testing.T, o Options) {
				t.Helper()
				if len(o.Proxy.Trusted) != 2 || o.Proxy.Trusted[0] != "127.0.0.1" {
					t.Errorf("unexpected proxy.trusted: %v", o.Proxy.Trusted)
				}
			},
		},
		{
			name:    "negative unknown key",
			key:     "snatcharr.invalid.unknown",
			val:     "something",
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name:    "negative invalid bool",
			key:     "snatcharr.web.dev",
			val:     "not-a-bool",
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name:    "negative invalid duration",
			key:     "snatcharr.session.ttl",
			val:     "invalid-duration",
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name:    "negative invalid int",
			key:     "snatcharr.logs.ring_size",
			val:     "not-an-int",
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name:    "negative invalid float",
			key:     "snatcharr.prowlarr.limit_threshold",
			val:     "not-a-float",
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name: "boundary empty string",
			key:  "snatcharr.jellyfin.api_key",
			val:  "test-jellyfin-api-token",
			check: func(t *testing.T, o Options) {
				t.Helper()
				if o.Jellyfin.APIKey != "test-jellyfin-api-token" {
					t.Errorf("unexpected api key: %s", o.Jellyfin.APIKey)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := Default()
			err := ApplySetting(&o, tc.key, tc.val)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errIs != nil && !errors.Is(err, tc.errIs) {
					t.Fatalf("expected error %v, got %v", tc.errIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, o)
			}
		})
	}
}
