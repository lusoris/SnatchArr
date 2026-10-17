// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package logring

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// Planted secrets in every shape the redaction claims to cover; none may reach the ring.
const (
	plantedHex     = "0123456789abcdef0123456789abcdef"
	plantedPass    = "hunter2-planted"
	plantedBearer  = "eyJhbGciOiJIUzI1NiJ9.planted.sig"
	plantedToken   = "tok-planted-77"
	plantedWebhook = "https://hooks.example/planted-path"
)

func TestRedactionHidesPlantedSecrets(t *testing.T) {
	t.Parallel()
	r := New(MinSize)
	l := slog.New(r.Wrap(debugHandler(), SourceAPI))
	l.Info("GET http://sonarr:8989/api/v3/series?apikey="+plantedHex+"&page=2",
		"api_key", plantedHex,
		"sonarr_api_key", "short-but-secret",
		"password", plantedPass,
		"client_secret", plantedToken,
		"webhook_url", plantedWebhook,
		"headers", "Authorization: Bearer "+plantedBearer+", X-Api-Key: "+plantedToken,
		"url", "https://admin:"+plantedPass+"@radarr.local/api",
		"err", errors.New("dial: token="+plantedToken+" refused"),
		slog.Group("arr", "key", plantedHex),
	)
	recs, _ := r.List(Query{Limit: 1})
	var sb strings.Builder
	sb.WriteString(recs[0].Message)
	for _, a := range recs[0].Attrs {
		sb.WriteString("\n" + a.Key + "=" + a.Value)
	}
	stored := sb.String()
	for _, secret := range []string{plantedHex, "short-but-secret", plantedPass, plantedBearer, plantedToken, plantedWebhook} {
		if strings.Contains(stored, secret) {
			t.Errorf("stored record leaks %q:\n%s", secret, stored)
		}
	}
	for _, kept := range []string{"page=2", "sonarr:8989", "radarr.local/api", "dial:", "refused"} {
		if !strings.Contains(stored, kept) {
			t.Errorf("redaction removed %q, which is no secret:\n%s", kept, stored)
		}
	}
}

func TestRedactAttr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, key, value, want string
	}{
		{"positive secret key", "api_key", "abc", Redacted},
		{"positive dotted secret key ignores case", "arr.X-Api-Key", "abc", Redacted},
		{"positive suffix", "prowlarr_token", "abc", Redacted},
		{"positive hex in a plain value", "note", "key " + plantedHex, "key " + Redacted},
		{"negative trace id keeps its hex", "trace_id", plantedHex, plantedHex},
		{"negative plain value", "instance", "sonarr-main", "sonarr-main"},
		{"negative look-alike key", "monkey", "banana", "banana"},
		{"boundary empty secret stays empty", "password", "", ""},
		{"boundary 31 hex is kept", "note", plantedHex[:31], plantedHex[:31]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := redactAttr(tc.key, tc.value); got != tc.want {
				t.Fatalf("redactAttr(%q, %q) = %q, want %q", tc.key, tc.value, got, tc.want)
			}
		})
	}
}
