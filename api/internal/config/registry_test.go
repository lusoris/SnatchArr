// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type structFrame struct {
	typ    reflect.Type
	prefix string
}

func collectOptionKeys() []string {
	var keys []string
	stack := []structFrame{{typ: reflect.TypeFor[Options](), prefix: "snatcharr"}}
	const maxIterations = 1000
	for iter := 0; len(stack) > 0 && iter < maxIterations; iter++ {
		curr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		t := curr.typ
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			continue
		}

		for field := range t.Fields() {
			if !field.IsExported() {
				continue
			}
			tag := field.Tag.Get("koanf")
			if tag == "" || tag == "-" {
				continue
			}
			tag = strings.Split(tag, ",")[0]
			key := curr.prefix + "." + tag

			ft := field.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				stack = append(stack, structFrame{typ: ft, prefix: key})
			} else {
				keys = append(keys, key)
			}
		}
	}
	slices.Sort(keys)
	return keys
}

func findDocsConfig(t *testing.T) string {
	t.Helper()
	dir := "."
	for range 5 {
		candidate := filepath.Join(dir, "docs", "configuration.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		dir = filepath.Join("..", dir)
	}
	t.Fatal("registry_test: could not find docs/configuration.md")
	return ""
}

func extractDocsEnvs(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- path within repository
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return DocsEnvs(data)
}

func TestOptionsKeysInRegistry(t *testing.T) {
	t.Parallel()
	keys := collectOptionKeys()
	reg := Registry()

	tests := []struct {
		name    string
		runTest func(t *testing.T)
	}{
		{
			name: "boundary_option_keys_count",
			runTest: func(t *testing.T) {
				t.Helper()
				const expectedCount = 47
				if len(keys) != expectedCount {
					t.Fatalf("expected %d Option keys, got %d", expectedCount, len(keys))
				}
			},
		},
		{
			name: "all_options_keys_in_registry",
			runTest: func(t *testing.T) {
				t.Helper()
				notFound := missing(keys, reg)
				if len(notFound) > 0 {
					t.Fatalf("keys under Options missing from Registry(): %v", notFound)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.runTest(t)
		})
	}
}

func TestDocsConfigurationParity(t *testing.T) {
	t.Parallel()
	path := findDocsConfig(t)
	docsEnvs := extractDocsEnvs(t, path)
	reg := Registry()

	tests := []struct {
		name    string
		runTest func(t *testing.T)
	}{
		{
			name: "boundary_registry_count",
			runTest: func(t *testing.T) {
				t.Helper()
				const expectedCount = 126
				if len(reg) != expectedCount {
					t.Fatalf("expected %d registry entries, got %d", expectedCount, len(reg))
				}
			},
		},
		{
			name: "all_registry_envs_in_docs",
			runTest: func(t *testing.T) {
				t.Helper()
				for _, s := range reg {
					if _, ok := docsEnvs[s.Env]; !ok {
						t.Errorf("registry env %s (%s) not found in docs/configuration.md table", s.Env, s.Key)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.runTest(t)
		})
	}
}

func TestNoDuplicateKeyOrEnv(t *testing.T) {
	t.Parallel()
	reg := Registry()

	tests := []struct {
		name string
		val  func(s Setting) string
	}{
		{name: "duplicate_key", val: func(s Setting) string { return s.Key }},
		{name: "duplicate_env", val: func(s Setting) string { return s.Env }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			seen := make(map[string]struct{}, len(reg))
			for _, s := range reg {
				v := tt.val(s)
				if _, ok := seen[v]; ok {
					t.Errorf("duplicate found: %s", v)
				}
				seen[v] = struct{}{}
			}
		})
	}
}

func TestMissingHelperNegativeCase(t *testing.T) {
	t.Parallel()
	reg := Registry()

	tests := []struct {
		name string
		keys []string
		want []string
	}{
		{
			name: "planted_missing",
			keys: []string{"snatcharr.planted.missing"},
			want: []string{"snatcharr.planted.missing"},
		},
		{
			name: "empty_keys_boundary",
			keys: []string{},
			want: nil,
		},
		{
			name: "mixed_present_and_missing",
			keys: []string{"snatcharr.profile", "snatcharr.planted.missing"},
			want: []string{"snatcharr.planted.missing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := missing(tt.keys, reg)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("missing(%v) = %v, want %v", tt.keys, got, tt.want)
			}
		})
	}
}

func TestWorkerOnly(t *testing.T) {
	t.Parallel()
	if len(workerOnly) == 0 {
		t.Fatal("workerOnly must not be empty")
	}
	reg := Registry()
	regEnvs := make(map[string]struct{}, len(reg))
	for _, s := range reg {
		regEnvs[s.Env] = struct{}{}
	}
	for _, wo := range workerOnly {
		if _, ok := regEnvs[wo]; ok {
			t.Errorf("workerOnly var %s unexpectedly present in Registry()", wo)
		}
	}
}

func TestParityTableCurrent(t *testing.T) {
	t.Parallel()
	docsPath := findDocsConfig(t)
	docs := extractDocsEnvs(t, docsPath)
	tsvPath := filepath.Join(filepath.Dir(docsPath), "settings-parity.tsv")
	committed, err := os.ReadFile(filepath.Clean(tsvPath)) // #nosec G304 -- path within repository
	if err != nil {
		t.Fatalf("read %s: %v", tsvPath, err)
	}
	planted := append(Registry(), Setting{Key: "snatcharr.planted", Env: "APP_SNATCHARR_PLANTED", Class: ClassRuntime, Apply: ApplyRestart})

	tests := []struct {
		name  string
		reg   []Setting
		docs  map[string]struct{}
		fresh bool
		row   string
	}{
		{name: "positive_committed_table_matches_registry", reg: Registry(), docs: docs, fresh: true},
		{name: "negative_planted_setting_makes_table_stale", reg: planted, docs: docs, row: "APP_SNATCHARR_PLANTED\tsnatcharr.planted\truntime\trestart\tno\n"},
		{name: "boundary_empty_docs_marks_every_row_undocumented", reg: Registry()[:1], docs: map[string]struct{}{}, row: "\tno\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := string(ParityTSV(tt.reg, tt.docs))
			if (got == string(committed)) != tt.fresh {
				t.Fatalf("table fresh = %v, want %v; run make settings-parity", !tt.fresh, tt.fresh)
			}
			if tt.row != "" && !strings.HasSuffix(got, tt.row) {
				t.Fatalf("last row = %q, want suffix %q", got[strings.LastIndex(got[:len(got)-1], "\n")+1:], tt.row)
			}
		})
	}
}

func TestDocsEnvs(t *testing.T) {
	t.Parallel()
	md := []byte("| Variable | Default |\n| :--- | :--- |\n| `APP_A` | x |\ntext `APP_B` |\n| `` | y |\n|`APP_C`|z|\n")
	got := DocsEnvs(md)
	for _, want := range []string{"APP_A", "APP_C"} {
		if _, ok := got[want]; !ok {
			t.Errorf("DocsEnvs missed %s", want)
		}
	}
	if len(got) != 2 {
		t.Errorf("DocsEnvs = %v, want exactly APP_A and APP_C", got)
	}
}

func sampleSettingValue(s Setting) string {
	if s.Default != "" {
		return s.Default
	}
	switch s.Type {
	case "bool":
		return "true"
	case "duration":
		return "10s"
	case "int":
		return "1"
	case "float64":
		return "1.0"
	default:
		return "sample"
	}
}

func unhandledRegistryKeys(reg []Setting) []string {
	var unhandled []string
	for _, s := range reg {
		if s.Class == ClassRuntime || (s.Class == ClassSecret && !IsEnvOnlySecret(s.Key, s.Env)) {
			o := Default()
			err := ApplySetting(&o, s.Key, sampleSettingValue(s))
			if err != nil && strings.Contains(err.Error(), "unknown option") {
				unhandled = append(unhandled, s.Key)
			}
		}
	}
	return unhandled
}

func TestRuntimeSettingsAcceptedByApplySetting(t *testing.T) {
	t.Parallel()
	reg := Registry()

	tests := []struct {
		name    string
		reg     []Setting
		wantBad []string
	}{
		{
			name:    "positive_all_runtime_and_writable_secrets_accepted",
			reg:     reg,
			wantBad: nil,
		},
		{
			name: "negative_planted_unhandled_runtime_key",
			reg: []Setting{
				{Key: "snatcharr.planted.unknown", Class: ClassRuntime, Type: "string"},
			},
			wantBad: []string{"snatcharr.planted.unknown"},
		},
		{
			name:    "boundary_bootstrap_and_env_secrets_ignored",
			reg:     []Setting{{Key: "http.addr", Class: ClassBootstrap, Type: "string"}},
			wantBad: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := unhandledRegistryKeys(tt.reg)
			if !slices.Equal(got, tt.wantBad) {
				t.Fatalf("unhandledRegistryKeys() = %v, want %v", got, tt.wantBad)
			}
		})
	}
}
