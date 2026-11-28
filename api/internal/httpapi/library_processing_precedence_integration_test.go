// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

func TestLibraryProcessingResolveAllScopesPrecedenceUntilNoMatch(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix
	lpSetModule(t, f.client, base, csrf, true)

	mkInstance := func(name string) string {
		inst, err := f.h.instances.Create(t.Context(), instances.Input{
			Kind: domain.KindSonarr, Name: name, BaseURL: "http://" + name + ".invalid:8989", APIKey: "test-secret-key", Enabled: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return inst.ID.String()
	}
	instID := mkInstance("lp-win")
	otherInstID := mkInstance("lp-decoy")

	const (
		itemID   = "item-1"
		rootPath = "/media/tv"
		reqLang  = "de"
		mediaTyp = "episode"
	)

	// Rows are in documented precedence order (highest first).
	// fields match the resolve request; decoy fields do not match it.
	table := []struct {
		scope string
		langs []string
		match map[string]any
		decoy map[string]any
	}{
		{
			"item_override",
			[]string{"en"},
			map[string]any{"instance_id": instID, "item_id": itemID},
			map[string]any{"instance_id": instID, "item_id": "item-other"},
		},
		{
			"user_language",
			[]string{"de"},
			map[string]any{"user_language": reqLang},
			map[string]any{"user_language": "fr"},
		},
		{
			"library",
			[]string{"fr"},
			map[string]any{"instance_id": instID, "root_path": rootPath},
			map[string]any{"instance_id": instID, "root_path": "/media/other"},
		},
		{
			"instance",
			[]string{"es"},
			map[string]any{"instance_id": instID},
			map[string]any{"instance_id": otherInstID},
		},
		{
			"media_type",
			[]string{"it"},
			map[string]any{"media_type": mediaTyp},
			map[string]any{"media_type": "music"},
		},
		{"global_default", []string{"ja"}, map[string]any{}, nil},
	}

	create := func(scope string, langs []string, fields map[string]any) map[string]any {
		payload := map[string]any{"scope": scope, "rule": lpRuleBody(langs...)}
		maps.Copy(payload, fields)
		return lpCreateRule(t, f.client, base, csrf, payload)
	}

	ids := make([]string, len(table))
	for i, row := range table {
		if row.decoy != nil {
			d := create(row.scope, []string{"pt"}, row.decoy)
			if d["scope"] != row.scope {
				t.Fatalf("decoy scope = %v, want %s", d["scope"], row.scope)
			}
		}
		created := create(row.scope, row.langs, row.match)
		if created["scope"] != row.scope {
			t.Fatalf("created scope = %v, want %s", created["scope"], row.scope)
		}
		ids[i] = created["id"].(string)
	}

	q := url.Values{}
	q.Set("instance_id", instID)
	q.Set("item_id", itemID)
	q.Set("requester_language", reqLang)
	q.Set("root_path", rootPath)
	q.Set("media_type", mediaTyp)
	resolveURL := base + "/library-processing/resolve?" + q.Encode()

	for i, row := range table {
		res := doHTTP(t, f.client, http.MethodGet, resolveURL, "", nil)
		if res.statusCode != http.StatusOK {
			t.Fatalf("resolve before deleting %s: status = %d, want 200; body: %s", row.scope, res.statusCode, string(res.body))
		}
		got := lpDecode(t, res.body)
		if got["rule_id"] != ids[i] {
			t.Errorf("winner %d rule_id = %v, want %s (%s)", i, got["rule_id"], ids[i], row.scope)
		}
		if got["scope"] != row.scope {
			t.Errorf("winner %d scope = %v, want %s", i, got["scope"], row.scope)
		}
		wantLangs := make([]any, len(row.langs))
		for j, l := range row.langs {
			wantLangs[j] = l
		}
		if langs := lpLanguages(t, got); !reflect.DeepEqual(langs, wantLangs) {
			t.Errorf("winner %d (%s) languages = %v, want %v", i, row.scope, langs, wantLangs)
		}

		res = doHTTP(t, f.client, http.MethodDelete, base+"/library-processing/rules/"+ids[i], csrf, nil)
		if res.statusCode != http.StatusNoContent && res.statusCode != http.StatusOK {
			t.Fatalf("delete %s rule status = %d, want 204; body: %s", row.scope, res.statusCode, string(res.body))
		}
	}

	// Every matching rule is gone; nonmatching decoys must not win.
	res := doHTTP(t, f.client, http.MethodGet, resolveURL, "", nil)
	if res.statusCode != http.StatusNotFound {
		t.Fatalf("resolve with no matching rule status = %d, want 404; body: %s", res.statusCode, string(res.body))
	}
}
