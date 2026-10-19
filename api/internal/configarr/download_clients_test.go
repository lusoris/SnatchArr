// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package configarr_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/configarr"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestParseDownloadClients_Table(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "qbit-pass.txt", "  qbit-secret-pass  \n")
	secrets := writeFile(t, dir, "secrets.yml", "sab_key: sabnzbd-secret-key\n")

	validYAML := `
qbittorrent:
  main:
    base_url: http://qbittorrent.media.svc:8080
    username: !env QBIT_USER
    password: !file qbit-pass.txt
    instance: sonarr/main
    max_active: 5
    bandwidth_budget_bps: 10485760
    enabled: true
sabnzbd:
  main:
    base_url: http://sabnzbd.media.svc:8080
    api_key: !secret sab_key
`

	enabledFalseYAML := `
qbittorrent:
  disabled_client:
    base_url: http://qbit.lan:8080
    password: secretpass
    enabled: false
`

	enabledNotBoolYAML := `
qbittorrent:
  maybe_client:
    base_url: http://qbit.lan:8080
    password: secretpass
    enabled: maybe
`

	bothSecretsYAML := `
sabnzbd:
  bad:
    base_url: http://sabnzbd:8080
    api_key: key123
    password: pass123
`

	unknownSectionYAML := `
utorrent:
  main:
    base_url: http://utorrent:8080
    password: pass
`

	unknownFieldYAML := `
qbittorrent:
  main:
    base_url: http://qbit:8080
    password: pass
    unknown_setting: foo
`

	missingSecretYAML := `
qbittorrent:
  missing_secret:
    base_url: http://qbit:8080
`

	missingBaseURLYAML := `
qbittorrent:
  missing_url:
    password: pass
`

	cases := []struct {
		name         string
		content      string
		secretsPath  string
		env          map[string]string
		wantErr      bool
		wantClients  int
		wantWarnings int
		checkFn      func(t *testing.T, res configarr.ParsedClients)
	}{
		{
			name:         "positive_valid_qbit_and_sab_tags",
			content:      validYAML,
			secretsPath:  secrets,
			env:          map[string]string{"QBIT_USER": "admin_qbit"},
			wantErr:      false,
			wantClients:  2,
			wantWarnings: 0,
			checkFn: func(t *testing.T, res configarr.ParsedClients) {
				t.Helper()
				qbit := res.Clients[0]
				if qbit.Key != "qbittorrent/main" || qbit.Kind != domain.ClientQBittorrent || qbit.Name != "main" {
					t.Fatalf("unexpected qbit identity: %+v", qbit)
				}
				if qbit.BaseURL != "http://qbittorrent.media.svc:8080" || qbit.Username != "admin_qbit" || qbit.Secret != "qbit-secret-pass" {
					t.Fatalf("unexpected qbit connection: %+v", qbit)
				}
				if qbit.InstanceKey != "sonarr/main" || qbit.MaxActive != 5 || qbit.BandwidthBudgetBPS != 10485760 || !qbit.Enabled {
					t.Fatalf("unexpected qbit settings: %+v", qbit)
				}
				sab := res.Clients[1]
				if sab.Key != "sabnzbd/main" || sab.Kind != domain.ClientSABnzbd || sab.Secret != "sabnzbd-secret-key" {
					t.Fatalf("unexpected sab: %+v", sab)
				}
			},
		},
		{
			name:         "boundary_enabled_false",
			content:      enabledFalseYAML,
			wantErr:      false,
			wantClients:  1,
			wantWarnings: 0,
			checkFn: func(t *testing.T, res configarr.ParsedClients) {
				t.Helper()
				if res.Clients[0].Enabled {
					t.Fatalf("expected enabled=false, got true")
				}
			},
		},
		{
			name:    "negative_enabled_not_bool",
			content: enabledNotBoolYAML,
			wantErr: true,
		},
		{
			name:    "negative_unknown_section",
			content: unknownSectionYAML,
			wantErr: true,
		},
		{
			name:    "negative_unknown_field",
			content: unknownFieldYAML,
			wantErr: true,
		},
		{
			name:    "negative_both_api_key_and_password",
			content: bothSecretsYAML,
			wantErr: true,
		},
		{
			name:         "negative_missing_secret_warns_and_skips",
			content:      missingSecretYAML,
			wantErr:      false,
			wantClients:  0,
			wantWarnings: 1,
			checkFn: func(t *testing.T, res configarr.ParsedClients) {
				t.Helper()
				if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "base_url or secret missing") {
					t.Fatalf("expected missing secret warning, got %v", res.Warnings)
				}
			},
		},
		{
			name:         "negative_missing_base_url_warns_and_skips",
			content:      missingBaseURLYAML,
			wantErr:      false,
			wantClients:  0,
			wantWarnings: 1,
			checkFn: func(t *testing.T, res configarr.ParsedClients) {
				t.Helper()
				if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "base_url or secret missing") {
					t.Fatalf("expected missing base_url warning, got %v", res.Warnings)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			subDir := t.TempDir()
			if strings.Contains(tc.content, "qbit-pass.txt") {
				writeFile(t, subDir, "qbit-pass.txt", "  qbit-secret-pass  \n")
			}
			path := writeFile(t, subDir, "clients.yml", tc.content)
			res, err := configarr.ParseDownloadClients(path, tc.secretsPath, env(tc.env))
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseDownloadClients() err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr {
				if !errors.Is(err, configarr.ErrParse) {
					t.Fatalf("expected ErrParse, got %v", err)
				}
				return
			}
			if len(res.Clients) != tc.wantClients {
				t.Fatalf("len(Clients) = %d, want %d", len(res.Clients), tc.wantClients)
			}
			if len(res.Warnings) != tc.wantWarnings {
				t.Fatalf("len(Warnings) = %d, want %d", len(res.Warnings), tc.wantWarnings)
			}
			if tc.checkFn != nil {
				tc.checkFn(t, res)
			}
		})
	}
}

func TestParseDownloadClients_EntryBounds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		count   int
		wantErr bool
	}{
		{"boundary_256_entries_pass", 256, false},
		{"negative_257_entries_error", 257, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			b.WriteString("qbittorrent:\n")
			for i := range tc.count {
				fmt.Fprintf(&b, "  c%d:\n    base_url: http://qbit%d:8080\n    password: secret%d\n", i, i, i)
			}
			dir := t.TempDir()
			p := writeFile(t, dir, "clients.yml", b.String())
			res, err := configarr.ParseDownloadClients(p, "", env(nil))
			if (err != nil) != tc.wantErr {
				t.Fatalf("count=%d err=%v, wantErr=%v", tc.count, err, tc.wantErr)
			}
			if !tc.wantErr && len(res.Clients) != tc.count {
				t.Fatalf("count=%d got %d clients", tc.count, len(res.Clients))
			}
		})
	}
}
