// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain_test

import (
	"errors"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestParseDownloadClientKind(t *testing.T) {
	t.Parallel()
	for _, k := range domain.AllDownloadClientKinds {
		got, err := domain.ParseDownloadClientKind(string(k))
		if err != nil || got != k {
			t.Fatalf("ParseDownloadClientKind(%q) = %q, %v", k, got, err)
		}
	}
	if _, err := domain.ParseDownloadClientKind("utorrent"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
}

func TestDownloadClientProtocol(t *testing.T) {
	t.Parallel()
	if domain.ClientSABnzbd.Protocol() != domain.ProtocolUsenet || domain.ClientNZBGet.Protocol() != domain.ProtocolUsenet {
		t.Fatal("usenet clients must report the usenet protocol")
	}
	if domain.ClientQBittorrent.Protocol() != domain.ProtocolTorrent || domain.ClientRTorrent.Protocol() != domain.ProtocolTorrent {
		t.Fatal("torrent clients must report the torrent protocol")
	}
}

func TestValidateDownloadClientInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		kind      domain.DownloadClientKind
		clientNm  string
		baseURL   string
		maxActive int
		budget    int64
		wantErr   bool
	}{
		{"ok", domain.ClientQBittorrent, "qbit", "http://qbit:8080", 5, 10_000_000, false},
		{"no limits", domain.ClientSABnzbd, "sab", "https://sab.example", 0, 0, false},
		{"bad kind", "utorrent", "x", "http://x", 0, 0, true},
		{"empty name", domain.ClientDeluge, "  ", "http://x", 0, 0, true},
		{"bad url", domain.ClientDeluge, "d", "ftp://x", 0, 0, true},
		{"negative active", domain.ClientNZBGet, "n", "http://x", -1, 0, true},
		{"too many active", domain.ClientNZBGet, "n", "http://x", 1001, 0, true},
		{"negative budget", domain.ClientNZBGet, "n", "http://x", 0, -5, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := domain.ValidateDownloadClientInput(tc.kind, tc.clientNm, tc.baseURL, tc.maxActive, tc.budget)
			if (err != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v got %v", tc.wantErr, err)
			}
			if err != nil && !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}
