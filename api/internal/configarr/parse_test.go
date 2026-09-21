// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package configarr_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/configarr"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func env(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

func TestParseResolvesTagsAndFlags(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "radarr.key", "  file-key-1234  \n")
	secrets := writeFile(t, dir, "secrets.yml", "SONARR_API_KEY: sonarr-secret-key\n")
	config := writeFile(t, dir, "config.yml", `
trashGuideUrl: https://github.com/TRaSH-Guides/Guides
lidarrEnabled: false
sonarr:
  series:
    base_url: http://sonarr:8989
    api_key: !secret SONARR_API_KEY
    quality_definition:
      type: series
  anime:
    base_url: !env ANIME_URL
    api_key: !env ANIME_KEY
    enabled: false
radarr:
  movies:
    base_url: https://radarr.lan/
    api_key: !file radarr.key
  incomplete:
    base_url: http://nowhere
lidarr:
  music:
    base_url: http://lidarr:8686
    api_key: abcdefgh
whisparr:
  adult:
    base_url: http://whisparr:6969
    api_key: whisparr-key-1
`)
	got, err := configarr.ParseWithEnv(config, secrets, env(map[string]string{"ANIME_URL": "http://anime:8989", "ANIME_KEY": "anime-key-1"}))
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(got.Instances))
	for _, d := range got.Instances {
		keys = append(keys, d.Key)
	}
	if strings.Join(keys, ",") != "sonarr/series,sonarr/anime,radarr/movies,whisparr/adult" {
		t.Fatalf("keys %v", keys)
	}
	by := map[string]configarr.Definition{}
	for _, d := range got.Instances {
		by[d.Key] = d
	}
	if by["sonarr/series"].APIKey != "sonarr-secret-key" || !by["sonarr/series"].Enabled {
		t.Fatalf("secret tag: %+v", by["sonarr/series"])
	}
	if by["sonarr/anime"].BaseURL != "http://anime:8989" || by["sonarr/anime"].Enabled {
		t.Fatalf("env tag / enabled flag: %+v", by["sonarr/anime"])
	}
	if by["radarr/movies"].APIKey != "file-key-1234" {
		t.Fatalf("file tag: %+v", by["radarr/movies"])
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "radarr/incomplete") {
		t.Fatalf("warnings: %v", got.Warnings)
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := map[string]string{
		"missing secret":  "sonarr:\n  a:\n    base_url: http://x\n    api_key: !secret NOPE\n",
		"missing env":     "sonarr:\n  a:\n    base_url: http://x\n    api_key: !env NOPE_ENV\n",
		"unknown tag":     "sonarr:\n  a:\n    base_url: http://x\n    api_key: !vault key\n",
		"not a mapping":   "sonarr: [a, b]\n",
		"instance scalar": "sonarr:\n  a: http://x\n",
		"missing file":    "sonarr:\n  a:\n    base_url: http://x\n    api_key: !file does-not-exist\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := writeFile(t, dir, strings.ReplaceAll(name, " ", "-")+".yml", content)
			_, err := configarr.ParseWithEnv(p, "", env(nil))
			if !errors.Is(err, configarr.ErrParse) {
				t.Fatalf("expected ErrParse, got %v", err)
			}
		})
	}
	if _, err := configarr.ParseWithEnv(filepath.Join(dir, "absent.yml"), "", env(nil)); !errors.Is(err, configarr.ErrParse) {
		t.Fatalf("absent file: %v", err)
	}
}
