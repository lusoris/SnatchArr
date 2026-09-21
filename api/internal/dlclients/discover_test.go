// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients_test

import (
	"testing"

	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func provider(impl string, fields map[string]any) arr.ProviderResource {
	enabled := true
	r := arr.ProviderResource{ID: 7, Name: "client", Implementation: impl, ImplementationName: impl, Enable: &enabled}
	for k, v := range fields {
		r.Fields = append(r.Fields, arr.ProviderField{Name: k, Value: v})
	}
	return r
}

func TestFromProvider(t *testing.T) {
	t.Parallel()
	inst := uuid.New()
	cases := []struct {
		name     string
		res      arr.ProviderResource
		kind     domain.DownloadClientKind
		baseURL  string
		username string
		secret   string
		redacted bool
	}{
		{
			"qbittorrent",
			provider("QBittorrent", map[string]any{"host": "qbit.lan", "port": float64(8080), "useSsl": false, "urlBase": "", "username": "admin", "password": "s3cret"}),
			domain.ClientQBittorrent, "http://qbit.lan:8080", "admin", "s3cret", false,
		},
		{
			"sabnzbd uses the api key over https with a url base",
			provider("Sabnzbd", map[string]any{"host": "sab.lan", "port": float64(9090), "useSsl": true, "urlBase": "/sabnzbd/", "apiKey": "abc123", "username": "u", "password": "p"}),
			domain.ClientSABnzbd, "https://sab.lan:9090/sabnzbd", "u", "abc123", false,
		},
		{
			"rtorrent defaults to RPC2",
			provider("RTorrent", map[string]any{"host": "rt.lan", "port": float64(5000), "username": "", "password": ""}),
			domain.ClientRTorrent, "http://rt.lan:5000/RPC2", "", "", false,
		},
		{
			"transmission keeps its web path",
			provider("Transmission", map[string]any{"host": "tr.lan", "port": float64(9091), "urlBase": "/transmission/", "username": "tr", "password": "pw"}),
			domain.ClientTransmission, "http://tr.lan:9091/transmission", "tr", "pw", false,
		},
		{
			"redacted secret is dropped and flagged",
			provider("Nzbget", map[string]any{"host": "nzb.lan", "port": float64(6789), "username": "nzbget", "password": "********"}),
			domain.ClientNZBGet, "http://nzb.lan:6789", "nzbget", "", true,
		},
		{
			"port given as a string",
			provider("Deluge", map[string]any{"host": "deluge.lan", "port": "8112", "password": "deluge"}),
			domain.ClientDeluge, "http://deluge.lan:8112", "", "deluge", false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, ok := dlclients.FromProvider(inst, tc.res)
			if !ok {
				t.Fatal("expected a supported client")
			}
			in := d.Input
			if in.Kind != tc.kind || in.BaseURL != tc.baseURL || in.Username != tc.username || in.Secret != tc.secret {
				t.Fatalf("got %+v", in)
			}
			if d.RedactedSecret != tc.redacted || d.RemoteID != 7 || !in.Enabled || in.InstanceID == nil || *in.InstanceID != inst {
				t.Fatalf("got %+v", d)
			}
		})
	}
}

func TestFromProviderRejectsUnsupportedImplementations(t *testing.T) {
	t.Parallel()
	for _, impl := range []string{"TorrentBlackhole", "UsenetBlackhole", "Aria2", "Flood", "DownloadStation", ""} {
		if _, ok := dlclients.FromProvider(uuid.New(), provider(impl, nil)); ok {
			t.Fatalf("%q must be unsupported", impl)
		}
	}
}

func TestFromProviderHonoursDisabledFlag(t *testing.T) {
	t.Parallel()
	r := provider("QBittorrent", map[string]any{"host": "h", "port": float64(1)})
	disabled := false
	r.Enable = &disabled
	d, ok := dlclients.FromProvider(uuid.New(), r)
	if !ok || d.Input.Enabled {
		t.Fatalf("got %+v ok=%v", d, ok)
	}
}
