// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package cleanuparrclient_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/cleanuparrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// liveEnv names the file tools/e2e/cleanuparr_live.sh writes: the real Cleanuparr it started
// and configured.
const liveEnv = "SNATCHARR_CLEANUPARR_LIVE"

// liveCleanuparr is one running Cleanuparr. Version is the release tag; SonarrName and
// SonarrURL are the one Sonarr instance the script configured, DryRun the general setting.
type liveCleanuparr struct {
	URL        string `json:"url"`
	APIKey     string `json:"api_key"`
	Version    string `json:"version"`
	SonarrName string `json:"sonarr_name"`
	SonarrURL  string `json:"sonarr_url"`
	DryRun     bool   `json:"dry_run"`
}

// TestLiveCleanuparr reads a real Cleanuparr through the client production uses. It runs in
// `make cleanuparr-live`, which starts and configures Cleanuparr and fails when this test
// skipped.
func TestLiveCleanuparr(t *testing.T) {
	t.Parallel()
	path := os.Getenv(liveEnv)
	if path == "" {
		t.Skip(liveEnv + " is not set; run make cleanuparr-live")
	}
	live := readLiveCleanuparr(t, path)
	c := cleanuparrclient.New(10*time.Second, "snatcharr-live-test")
	t.Run("status", func(t *testing.T) {
		t.Parallel()
		checkLiveStatus(t, c, live)
	})
	t.Run("strikes", func(t *testing.T) {
		t.Parallel()
		checkLiveStrikes(t, c, live)
	})
	t.Run("configuration", func(t *testing.T) {
		t.Parallel()
		checkLiveConfiguration(t, c, live)
	})
}

func readLiveCleanuparr(t *testing.T, path string) liveCleanuparr {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- the path is the test's own input file
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var live liveCleanuparr
	if err = json.Unmarshal(raw, &live); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return live
}

func checkLiveStatus(t *testing.T, c *cleanuparrclient.HTTP, live liveCleanuparr) {
	t.Helper()
	st, err := c.Status(t.Context(), live.URL, live.APIKey)
	if err != nil {
		t.Fatalf("status with the right key: %v", err)
	}
	if !strings.HasPrefix(st.Version, live.Version+".") && st.Version != live.Version {
		t.Errorf("version = %q; want release %s", st.Version, live.Version)
	}
	if st.StartedAt == nil {
		t.Error("start time not read")
	}
	if n := st.MediaManagers["Sonarr"]; n != 1 {
		t.Errorf("Sonarr instance count = %d; want 1 (media managers %v)", n, st.MediaManagers)
	}
	if _, err = c.Status(t.Context(), live.URL, live.APIKey+"-wrong"); err == nil {
		t.Error("a wrong API key was accepted")
	}
}

func checkLiveStrikes(t *testing.T, c *cleanuparrclient.HTTP, live liveCleanuparr) {
	t.Helper()
	recent, err := c.RecentStrikes(t.Context(), live.URL, live.APIKey, cleanuparrclient.MaxRecent)
	if err != nil {
		t.Fatalf("recent strikes: %v", err)
	}
	if len(recent) != 0 {
		t.Errorf("a new Cleanuparr reports %d recent strikes; want 0", len(recent))
	}
	sum, err := c.Summary(t.Context(), live.URL, live.APIKey)
	if err != nil {
		t.Fatalf("strike summary: %v", err)
	}
	if sum != (cleanuparrclient.StrikeSummary{}) {
		t.Errorf("a new Cleanuparr reports strike summary %+v; want zero", sum)
	}
}

func checkLiveConfiguration(t *testing.T, c *cleanuparrclient.HTTP, live liveCleanuparr) {
	t.Helper()
	dry, err := c.GeneralDryRun(t.Context(), live.URL, live.APIKey)
	if err != nil {
		t.Fatalf("general configuration: %v", err)
	}
	if dry != live.DryRun {
		t.Errorf("dry run = %t; Cleanuparr was left with dry run = %t", dry, live.DryRun)
	}
	sonarr := checkLiveSonarr(t, c, live)
	radarr, err := c.ArrInstances(t.Context(), live.URL, live.APIKey, "radarr")
	if err != nil || len(radarr) != 0 {
		t.Errorf("radarr instances = %v, %v; want none", radarr, err)
	}
	seeker, err := c.SeekerConfig(t.Context(), live.URL, live.APIKey)
	if err != nil {
		t.Fatalf("seeker configuration: %v", err)
	}
	want := cleanuparrclient.SeekerInstance{
		ArrInstanceID: sonarr.ID, InstanceName: live.SonarrName, InstanceType: "Sonarr", ArrInstanceEnabled: true,
	}
	if len(seeker.Instances) != 1 || seeker.Instances[0] != want {
		t.Errorf("seeker instances = %+v; want [%+v]", seeker.Instances, want)
	}
}

// checkLiveSonarr reads the one Sonarr instance; its URL matches the configured one once both
// are normalised (Cleanuparr stores it with a trailing slash).
func checkLiveSonarr(t *testing.T, c *cleanuparrclient.HTTP, live liveCleanuparr) cleanuparrclient.ArrInstanceRef {
	t.Helper()
	refs, err := c.ArrInstances(t.Context(), live.URL, live.APIKey, "sonarr")
	if err != nil {
		t.Fatalf("sonarr instances: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("sonarr instances = %+v; want the one configured", refs)
	}
	got, gotErr := domain.NormalizeBaseURL(refs[0].URL)
	want, wantErr := domain.NormalizeBaseURL(live.SonarrURL)
	if gotErr != nil || wantErr != nil || got != want {
		t.Errorf("sonarr URL %q (%v) does not match the configured %q (%v)", got, gotErr, want, wantErr)
	}
	if refs[0].Name != live.SonarrName || !refs[0].Enabled || refs[0].ID == "" {
		t.Errorf("sonarr instance = %+v; want enabled %q with an id", refs[0], live.SonarrName)
	}
	return refs[0]
}
