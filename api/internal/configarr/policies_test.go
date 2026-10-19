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

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/configarr"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

func TestParsePolicies_PositiveDefaultsAndInstanceMerge(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := `
defaults:
  snatch:
    missing_per_cycle: 2
    hourly_cap: 20
  queue:
    enabled: true
    rules:
      failed:
        mode: observe
        threshold: 2
instances:
  sonarr/series:
    snatch:
      hourly_cap: 30
    queue:
      rules:
        failed:
          threshold: 4
        stalled:
          mode: act
          threshold: 6
`
	path := writeFile(t, dir, "policies.yml", content)
	res, err := configarr.ParsePolicies(path, "", env(nil))
	if err != nil {
		t.Fatalf("ParsePolicies() error = %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}

	// 1. Defaults verification
	defSnatchCap, ok := res.Defaults.Snatch["hourly_cap"].(int)
	if !ok || defSnatchCap != 20 {
		t.Fatalf("defaults snatch hourly_cap = %v, want 20", res.Defaults.Snatch["hourly_cap"])
	}
	defQueueEnabled, ok := res.Defaults.Queue["enabled"].(bool)
	if !ok || !defQueueEnabled {
		t.Fatalf("defaults queue enabled = %v, want true", res.Defaults.Queue["enabled"])
	}

	// 2. Merged instance verification
	inst, ok := res.Instances["sonarr/series"]
	if !ok {
		t.Fatal("sonarr/series not found in res.Instances")
	}

	// Snatch: missing_per_cycle inherited from defaults (2), hourly_cap overridden by instance (30)
	missingPerCycle, ok := inst.Snatch["missing_per_cycle"].(int)
	if !ok || missingPerCycle != 2 {
		t.Fatalf("sonarr/series missing_per_cycle = %v, want 2 (from defaults)", inst.Snatch["missing_per_cycle"])
	}
	hourlyCap, ok := inst.Snatch["hourly_cap"].(int)
	if !ok || hourlyCap != 30 {
		t.Fatalf("sonarr/series hourly_cap = %v, want 30 (from instance)", inst.Snatch["hourly_cap"])
	}

	// Queue: enabled inherited from defaults (true)
	qEnabled, ok := inst.Queue["enabled"].(bool)
	if !ok || !qEnabled {
		t.Fatalf("sonarr/series queue enabled = %v, want true (from defaults)", inst.Queue["enabled"])
	}

	// Queue rules: failed threshold overridden (4), mode inherited (observe); stalled added (act, 6)
	rules, ok := inst.Queue["rules"].(map[string]any)
	if !ok {
		t.Fatalf("sonarr/series rules not a map: %T", inst.Queue["rules"])
	}
	failedRule, ok := rules["failed"].(map[string]any)
	if !ok {
		t.Fatalf("rules.failed not a map: %T", rules["failed"])
	}
	if failedRule["mode"] != "observe" {
		t.Fatalf("rules.failed.mode = %v, want observe", failedRule["mode"])
	}
	if failedRule["threshold"] != 4 {
		t.Fatalf("rules.failed.threshold = %v, want 4", failedRule["threshold"])
	}
	stalledRule, ok := rules["stalled"].(map[string]any)
	if !ok {
		t.Fatalf("rules.stalled not a map: %T", rules["stalled"])
	}
	if stalledRule["mode"] != "act" || stalledRule["threshold"] != 6 {
		t.Fatalf("rules.stalled = %+v, want act, 6", stalledRule)
	}

	// 3. ForInstance fallback to defaults for unlisted instances
	unlisted := res.ForInstance("radarr/movies")
	if unlisted.Snatch["missing_per_cycle"] != 2 || unlisted.Snatch["hourly_cap"] != 20 {
		t.Fatalf("ForInstance unlisted snatch = %+v", unlisted.Snatch)
	}
	if unlisted.Queue["enabled"] != true {
		t.Fatalf("ForInstance unlisted queue = %+v", unlisted.Queue)
	}
}

func TestParsePolicies_Negative(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	t.Run("negative_unknown_key_top_level", func(t *testing.T) {
		t.Parallel()
		content := `
unknown_top_level: true
instances:
  sonarr/series:
    snatch:
      missing_per_cycle: 1
`
		p := writeFile(t, dir, "unknown_top_key.yml", content)
		res, err := configarr.ParsePolicies(p, "", env(nil))
		if err != nil {
			t.Fatalf("ParsePolicies() error = %v", err)
		}
		if len(res.Warnings) == 0 {
			t.Fatal("expected warning for unknown top-level key, got none")
		}
		if !strings.Contains(res.Warnings[0], `unknown key "unknown_top_level"`) {
			t.Fatalf("warning = %q, want unknown key", res.Warnings[0])
		}
	})

	t.Run("negative_unknown_instance_section", func(t *testing.T) {
		t.Parallel()
		content := `
instances:
  sonarr/series:
    bogus_section: true
`
		p := writeFile(t, dir, "unknown_inst_sec.yml", content)
		res, err := configarr.ParsePolicies(p, "", env(nil))
		if err != nil {
			t.Fatalf("ParsePolicies() error = %v", err)
		}
		if len(res.Warnings) == 0 {
			t.Fatal("expected warning for unknown instance section")
		}
		if !strings.Contains(res.Warnings[0], `unknown field "bogus_section"`) || !strings.Contains(res.Warnings[0], "sonarr/series") {
			t.Fatalf("warning = %q", res.Warnings[0])
		}
	})

	t.Run("negative_wrong_type_defaults_not_mapping", func(t *testing.T) {
		t.Parallel()
		content := `defaults: [1, 2, 3]`
		p := writeFile(t, dir, "defaults_list.yml", content)
		_, err := configarr.ParsePolicies(p, "", env(nil))
		if !errors.Is(err, configarr.ErrParse) {
			t.Fatalf("expected ErrParse for defaults list, got %v", err)
		}
	})

	t.Run("negative_wrong_type_instances_not_mapping", func(t *testing.T) {
		t.Parallel()
		content := `instances: "not-a-map"`
		p := writeFile(t, dir, "instances_str.yml", content)
		_, err := configarr.ParsePolicies(p, "", env(nil))
		if !errors.Is(err, configarr.ErrParse) {
			t.Fatalf("expected ErrParse for instances string, got %v", err)
		}
	})

	t.Run("negative_syntax_error", func(t *testing.T) {
		t.Parallel()
		content := `{ broken yaml: [`
		p := writeFile(t, dir, "syntax_err.yml", content)
		_, err := configarr.ParsePolicies(p, "", env(nil))
		if !errors.Is(err, configarr.ErrParse) {
			t.Fatalf("expected ErrParse for YAML syntax error, got %v", err)
		}
	})
}

func TestParsePolicies_Boundary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	t.Run("boundary_empty_file", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, dir, "empty.yml", "\n   # empty comment only\n")
		res, err := configarr.ParsePolicies(p, "", env(nil))
		if err != nil {
			t.Fatalf("empty file err = %v, want nil", err)
		}
		if len(res.Instances) != 0 || len(res.Warnings) != 0 {
			t.Fatalf("empty file res = %+v, want 0 instances 0 warnings", res)
		}
	})

	t.Run("boundary_defaults_only", func(t *testing.T) {
		t.Parallel()
		content := `
defaults:
  queue:
    enabled: true
`
		p := writeFile(t, dir, "defaults_only.yml", content)
		res, err := configarr.ParsePolicies(p, "", env(nil))
		if err != nil {
			t.Fatalf("defaults only err = %v", err)
		}
		if res.Defaults.Queue["enabled"] != true {
			t.Fatalf("Defaults queue enabled = %v, want true", res.Defaults.Queue["enabled"])
		}
		if len(res.Instances) != 0 {
			t.Fatalf("len(Instances) = %d, want 0", len(res.Instances))
		}
	})

	t.Run("boundary_instance_with_empty_mapping", func(t *testing.T) {
		t.Parallel()
		content := `
instances:
  sonarr/series: {}
`
		p := writeFile(t, dir, "empty_mapping.yml", content)
		res, err := configarr.ParsePolicies(p, "", env(nil))
		if err != nil {
			t.Fatalf("empty mapping err = %v", err)
		}
		if _, ok := res.Instances["sonarr/series"]; !ok {
			t.Fatal("expected sonarr/series in res.Instances")
		}
	})

	t.Run("boundary_file_at_size_bound", func(t *testing.T) {
		t.Parallel()
		// 4 * 1024 * 1024 = 4194304 bytes
		prefix := "defaults:\n  queue:\n    enabled: true\n# "
		paddingLen := (4 * 1024 * 1024) - len(prefix)
		exactContent := prefix + strings.Repeat("x", paddingLen)
		if len(exactContent) != 4*1024*1024 {
			t.Fatalf("len = %d, want 4194304", len(exactContent))
		}
		pExact := writeFile(t, dir, "exact_bound.yml", exactContent)
		res, err := configarr.ParsePolicies(pExact, "", env(nil))
		if err != nil {
			t.Fatalf("exact size bound error = %v, want nil", err)
		}
		if res.Defaults.Queue["enabled"] != true {
			t.Fatal("failed to parse exact bound content")
		}

		// 1 byte over the limit
		pOver := writeFile(t, dir, "over_bound.yml", exactContent+"y")
		_, overErr := configarr.ParsePolicies(pOver, "", env(nil))
		if !errors.Is(overErr, configarr.ErrParse) {
			t.Fatalf("over size bound error = %v, want ErrParse", overErr)
		}
	})
}

func TestParsePolicies_TagResolution(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "cap.txt", "35\n")
	secrets := writeFile(t, dir, "secrets.yml", "CYCLE_INTERVAL: 120\n")
	content := `
instances:
  sonarr/series:
    snatch:
      missing_per_cycle: !env MISSING_PER_CYCLE
      hourly_cap: !file cap.txt
      cycle_interval_s: !secret CYCLE_INTERVAL
`
	p := writeFile(t, dir, "tags.yml", content)
	res, err := configarr.ParsePolicies(p, secrets, env(map[string]string{"MISSING_PER_CYCLE": "3"}))
	if err != nil {
		t.Fatalf("ParsePolicies() error = %v", err)
	}
	inst := res.Instances["sonarr/series"]
	if inst.Snatch["missing_per_cycle"] != 3 {
		t.Fatalf("missing_per_cycle = %v, want 3", inst.Snatch["missing_per_cycle"])
	}
	if inst.Snatch["hourly_cap"] != 35 {
		t.Fatalf("hourly_cap = %v, want 35", inst.Snatch["hourly_cap"])
	}
	if inst.Snatch["cycle_interval_s"] != 120 {
		t.Fatalf("cycle_interval_s = %v, want 120", inst.Snatch["cycle_interval_s"])
	}
}

func setupValidationFixture(t *testing.T) (fixture, string, string, uuid.UUID, uuid.UUID) {
	t.Helper()
	dir := t.TempDir()
	configPath := writeConfig(t, dir, `
sonarr:
  series:
    base_url: http://sonarr:8989
    api_key: sonarr-key-series
radarr:
  movies:
    base_url: http://radarr:7878
    api_key: radarr-key-movies
`)
	policiesPath := filepath.Join(dir, "policies.yml")
	f := newFixture(t, func(o *config.Options) {
		o.Configarr.Config = configPath
		o.Configarr.Policies = policiesPath
	})
	ctx := t.Context()
	if err := os.WriteFile(policiesPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write policies.yml: %v", err)
	}
	if _, err := f.svc.Import(ctx, configPath, ""); err != nil {
		t.Fatalf("initial Import() = %v", err)
	}
	idSonarr, ok := f.instSvc.IDByConfigarrKey(ctx, "sonarr/series")
	if !ok {
		t.Fatal("sonarr/series not found")
	}
	idRadarr, ok := f.instSvc.IDByConfigarrKey(ctx, "radarr/movies")
	if !ok {
		t.Fatal("radarr/movies not found")
	}
	return f, configPath, policiesPath, idSonarr, idRadarr
}

func TestPolicies_Validation(t *testing.T) {
	t.Parallel()

	t.Run("unknown_snatch_field", func(t *testing.T) {
		t.Parallel()
		f, configPath, policiesPath, idSonarr, idRadarr := setupValidationFixture(t)
		ctx := t.Context()

		if err := os.WriteFile(policiesPath, []byte(`
instances:
  sonarr/series:
    snatch:
      bogus_field: 123
      missing_per_cycle: 5
  radarr/movies:
    snatch:
      hourly_cap: 45
`), 0o600); err != nil {
			t.Fatal(err)
		}

		res, err := f.svc.Import(ctx, configPath, "")
		if err != nil {
			t.Fatalf("Import() err = %v", err)
		}
		var foundWarning bool
		for _, w := range res.Warnings {
			if strings.Contains(w, "policies.yml") && strings.Contains(w, "sonarr/series") && strings.Contains(w, `unknown field "bogus_field"`) {
				foundWarning = true
				break
			}
		}
		if !foundWarning {
			t.Fatalf("expected warning naming path bogus_field, got: %v", res.Warnings)
		}

		polSonarr, err := f.polSvc.Get(ctx, idSonarr)
		if err != nil {
			t.Fatal(err)
		}
		if polSonarr.MissingPerCycle == 5 {
			t.Fatalf("sonarr/series snatch section was applied, want skipped")
		}

		polRadarr, err := f.polSvc.Get(ctx, idRadarr)
		if err != nil {
			t.Fatal(err)
		}
		if polRadarr.HourlyCap != 45 {
			t.Fatalf("radarr/movies hourly_cap = %d, want 45", polRadarr.HourlyCap)
		}
	})

	t.Run("unknown_queue_rule", func(t *testing.T) {
		t.Parallel()
		f, configPath, policiesPath, idSonarr, idRadarr := setupValidationFixture(t)
		ctx := t.Context()

		if err := os.WriteFile(policiesPath, []byte(`
instances:
  sonarr/series:
    queue:
      rules:
        bogus_rule:
          mode: observe
        failed:
          threshold: 9
  radarr/movies:
    queue:
      rules:
        failed:
          threshold: 5
`), 0o600); err != nil {
			t.Fatal(err)
		}

		res, err := f.svc.Import(ctx, configPath, "")
		if err != nil {
			t.Fatalf("Import() err = %v", err)
		}
		var foundWarning bool
		for _, w := range res.Warnings {
			if strings.Contains(w, "policies.yml") && strings.Contains(w, "sonarr/series") && strings.Contains(w, `unknown field "rules.bogus_rule"`) {
				foundWarning = true
				break
			}
		}
		if !foundWarning {
			t.Fatalf("expected warning naming path rules.bogus_rule, got: %v", res.Warnings)
		}

		qSonarr, err := f.qStore.Settings(ctx, idSonarr)
		if err != nil {
			t.Fatal(err)
		}
		if rule := queue.ResolveRule(qSonarr, queue.RuleFailed); rule.Threshold == 9 {
			t.Fatalf("sonarr/series queue section was applied, want skipped")
		}

		qRadarr, err := f.qStore.Settings(ctx, idRadarr)
		if err != nil {
			t.Fatal(err)
		}
		if rule := queue.ResolveRule(qRadarr, queue.RuleFailed); rule.Threshold != 5 {
			t.Fatalf("radarr/movies failed threshold = %d, want 5", rule.Threshold)
		}
	})

	t.Run("unknown_rule_field", func(t *testing.T) {
		t.Parallel()
		f, configPath, policiesPath, idSonarr, idRadarr := setupValidationFixture(t)
		ctx := t.Context()

		if err := os.WriteFile(policiesPath, []byte(`
instances:
  sonarr/series:
    queue:
      rules:
        failed:
          bogus: 123
          threshold: 8
  radarr/movies:
    queue:
      rules:
        failed:
          threshold: 5
`), 0o600); err != nil {
			t.Fatal(err)
		}

		res, err := f.svc.Import(ctx, configPath, "")
		if err != nil {
			t.Fatalf("Import() err = %v", err)
		}
		var foundWarning bool
		for _, w := range res.Warnings {
			if strings.Contains(w, "policies.yml") && strings.Contains(w, "sonarr/series") && strings.Contains(w, `unknown field "rules.failed.bogus"`) {
				foundWarning = true
				break
			}
		}
		if !foundWarning {
			t.Fatalf("expected warning naming path rules.failed.bogus, got: %v", res.Warnings)
		}

		qSonarr, err := f.qStore.Settings(ctx, idSonarr)
		if err != nil {
			t.Fatal(err)
		}
		if rule := queue.ResolveRule(qSonarr, queue.RuleFailed); rule.Threshold == 8 {
			t.Fatalf("sonarr/series queue section was applied, want skipped")
		}

		qRadarr, err := f.qStore.Settings(ctx, idRadarr)
		if err != nil {
			t.Fatal(err)
		}
		if rule := queue.ResolveRule(qRadarr, queue.RuleFailed); rule.Threshold != 5 {
			t.Fatalf("radarr/movies failed threshold = %d, want 5", rule.Threshold)
		}
	})

	t.Run("wrong_type_hourly_cap", func(t *testing.T) {
		t.Parallel()
		f, configPath, policiesPath, idSonarr, idRadarr := setupValidationFixture(t)
		ctx := t.Context()

		if err := os.WriteFile(policiesPath, []byte(`
instances:
  sonarr/series:
    snatch:
      hourly_cap: "x"
      missing_per_cycle: 7
  radarr/movies:
    snatch:
      hourly_cap: 45
`), 0o600); err != nil {
			t.Fatal(err)
		}

		res, err := f.svc.Import(ctx, configPath, "")
		if err != nil {
			t.Fatalf("Import() err = %v", err)
		}
		var foundWarning bool
		for _, w := range res.Warnings {
			if strings.Contains(w, "sonarr/series") && strings.Contains(w, "hourly_cap") {
				foundWarning = true
				break
			}
		}
		if !foundWarning {
			t.Fatalf("expected warning naming hourly_cap, got: %v", res.Warnings)
		}

		polSonarr, err := f.polSvc.Get(ctx, idSonarr)
		if err != nil {
			t.Fatal(err)
		}
		if polSonarr.MissingPerCycle == 7 {
			t.Fatalf("sonarr/series snatch section was applied, want skipped")
		}

		polRadarr, err := f.polSvc.Get(ctx, idRadarr)
		if err != nil {
			t.Fatal(err)
		}
		if polRadarr.HourlyCap != 45 {
			t.Fatalf("radarr/movies hourly_cap = %d, want 45", polRadarr.HourlyCap)
		}
	})

	t.Run("boundary_optional_unset_min_custom_format_score", func(t *testing.T) {
		t.Parallel()
		f, configPath, policiesPath, idSonarr, _ := setupValidationFixture(t)
		ctx := t.Context()

		initQ, qErr := f.qStore.Settings(ctx, idSonarr)
		if qErr != nil {
			t.Fatal(qErr)
		}
		if initQ.MinCustomFormatScore != nil {
			t.Fatalf("initial MinCustomFormatScore = %v, want nil", initQ.MinCustomFormatScore)
		}

		if err := os.WriteFile(policiesPath, []byte(`
instances:
  sonarr/series:
    queue:
      min_custom_format_score: 50
`), 0o600); err != nil {
			t.Fatal(err)
		}

		res, err := f.svc.Import(ctx, configPath, "")
		if err != nil {
			t.Fatalf("Import() err = %v", err)
		}
		for _, w := range res.Warnings {
			if strings.Contains(w, "min_custom_format_score") {
				t.Fatalf("unexpected warning for valid optional field: %s", w)
			}
		}

		afterQ, aErr := f.qStore.Settings(ctx, idSonarr)
		if aErr != nil {
			t.Fatal(aErr)
		}
		if afterQ.MinCustomFormatScore == nil || *afterQ.MinCustomFormatScore != 50 {
			t.Fatalf("after MinCustomFormatScore = %v, want 50", afterQ.MinCustomFormatScore)
		}
	})
}

func TestPolicies_ReasonRulesAndMatchFloors(t *testing.T) {
	t.Parallel()
	f, configPath, policiesPath, idSonarr, _ := setupValidationFixture(t)
	ctx := t.Context()

	if err := os.WriteFile(policiesPath, []byte(`
instances:
  sonarr/series:
    queue:
      rules:
        import_fixable:
          mode: act
        import_broken:
          threshold: 2
        import_not_wanted:
          mode: observe
      match_fixable_min_percent: 70
      track_broken_below_percent: 30
`), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil {
		t.Fatalf("Import() err = %v", err)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "policies.yml") {
			t.Fatalf("unexpected policies warning: %s", w)
		}
	}
	q, err := f.qStore.Settings(ctx, idSonarr)
	if err != nil {
		t.Fatal(err)
	}
	if rule := queue.ResolveRule(q, queue.RuleImportFixable); rule.Mode != queue.ModeAct {
		t.Fatalf("import_fixable mode = %q, want act", rule.Mode)
	}
	if rule := queue.ResolveRule(q, queue.RuleImportBroken); rule.Threshold != 2 {
		t.Fatalf("import_broken threshold = %d, want 2", rule.Threshold)
	}
	if q.MatchFixableMinPercent != 70 || q.TrackBrokenBelowPercent != 30 {
		t.Fatalf("floors = %v/%v, want 70/30", q.MatchFixableMinPercent, q.TrackBrokenBelowPercent)
	}
	if q.MatchBrokenBelowPercent != queue.DefaultMatchBrokenBelowPercent {
		t.Fatalf("match_broken_below_percent = %v, want default %v", q.MatchBrokenBelowPercent, queue.DefaultMatchBrokenBelowPercent)
	}
}
