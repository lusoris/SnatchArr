// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

func namedPresetRequest(t *testing.T, f *routerFixture, method, path, csrf string, body any, status int) []byte {
	t.Helper()
	var text string
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		text = string(raw)
	}
	resp, raw := doPriorityRequest(t, f, method, path, csrf, text)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != status {
		t.Fatalf("%s %s = %d; want %d: %s", method, path, resp.StatusCode, status, raw)
	}
	if status >= 400 {
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/problem+json") {
			t.Fatalf("error Content-Type = %q", resp.Header.Get("Content-Type"))
		}
		var problem oas.Problem
		if err := json.Unmarshal(raw, &problem); err != nil || problem.Status != status {
			t.Fatalf("problem = %+v, %v", problem, err)
		}
	}
	return raw
}

func namedPresetBody(name string) oas.PriorityPresetInput {
	return oas.PriorityPresetInput{
		Name: name, Search: priorityPresetToOAS(domain.MediaFirstPreset()), Download: priorityPresetToOAS(domain.MediaFirstPreset()),
	}
}

func createNamedPreset(t *testing.T, f *routerFixture, csrf string, body oas.PriorityPresetInput) oas.PriorityPreset {
	t.Helper()
	raw := namedPresetRequest(t, f, http.MethodPost, "/priority-presets", csrf, body, http.StatusCreated)
	var out oas.PriorityPreset
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.ID.Set || out.Builtin || out.Name != strings.TrimSpace(body.Name) {
		t.Fatalf("created preset = %+v", out)
	}
	return out
}

func assertNamedPresetWriteValidation(t *testing.T, f *routerFixture, csrf string, in oas.PriorityPresetInput, status int) {
	t.Helper()
	raw := namedPresetRequest(t, f, http.MethodPost, "/priority-presets", csrf, in, status)
	var target oas.PriorityPreset
	updateStatus := status
	if status == http.StatusCreated {
		if err := json.Unmarshal(raw, &target); err != nil {
			t.Fatal(err)
		}
		updateStatus = http.StatusOK
	} else {
		target = createNamedPreset(t, f, csrf, namedPresetBody("valid-target"))
	}
	namedPresetRequest(t, f, http.MethodPut, "/priority-presets/"+target.ID.Value.String(), csrf, in, updateStatus)
}

func TestNamedPriorityPresetValidationHTTP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
	}{
		{"", http.StatusUnprocessableEntity},
		{"a", http.StatusCreated},
		{strings.Repeat("a", 40), http.StatusCreated},
		{strings.Repeat("a", 41), http.StatusUnprocessableEntity},
		{strings.Repeat("ä", 40), http.StatusCreated},
		{"   ", http.StatusUnprocessableEntity},
		{"MEDIA_FIRST", http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newRouterFixture(t)
			_ = completeInitialSetup(t, f)
			csrf := getAdminCsrfToken(t, f)
			assertNamedPresetWriteValidation(t, f, csrf, namedPresetBody(tc.name), tc.status)
		})
	}
}

func TestNamedPriorityPresetConflictAndCapHTTP(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	night := createNamedPreset(t, f, csrf, namedPresetBody("night"))
	namedPresetRequest(t, f, http.MethodPost, "/priority-presets", csrf, namedPresetBody("NIGHT"), http.StatusConflict)
	other := createNamedPreset(t, f, csrf, namedPresetBody("other"))
	namedPresetRequest(t, f, http.MethodPut, "/priority-presets/"+other.ID.Value.String(), csrf, namedPresetBody("Night"), http.StatusConflict)
	for i := 2; i < domain.MaxPriorityPresets; i++ {
		createNamedPreset(t, f, csrf, namedPresetBody(fmt.Sprintf("preset-%02d", i)))
	}
	namedPresetRequest(t, f, http.MethodPost, "/priority-presets", csrf, namedPresetBody("twenty-first"), http.StatusUnprocessableEntity)
	var listed []oas.PriorityPreset
	raw := namedPresetRequest(t, f, http.MethodGet, "/priority-presets", "", nil, http.StatusOK)
	if err := json.Unmarshal(raw, &listed); err != nil || len(listed) != 21 || !listed[0].Builtin || listed[0].ID.Set {
		t.Fatalf("listed = %+v, %v; want builtin + 20 saved presets", listed, err)
	}
	// Editing at the cap is allowed; freeing a slot permits another create.
	path := "/priority-presets/" + night.ID.Value.String()
	namedPresetRequest(t, f, http.MethodPut, path, csrf, namedPresetBody("evening"), http.StatusOK)
	namedPresetRequest(t, f, http.MethodDelete, path, csrf, nil, http.StatusNoContent)
	createNamedPreset(t, f, csrf, namedPresetBody("replacement"))
	namedPresetRequest(t, f, http.MethodDelete, path, csrf, nil, http.StatusNotFound)
}

func TestNamedPriorityPresetMapsHTTP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*oas.PriorityPresetInput)
		status int
	}{
		{"missing search kind", func(in *oas.PriorityPresetInput) { delete(in.Search, "lidarr") }, 422},
		{"missing download kind", func(in *oas.PriorityPresetInput) { delete(in.Download, "lidarr") }, 422},
		{"unknown search kind", func(in *oas.PriorityPresetInput) { in.Search["bazarr"] = 10 }, 422},
		{"unknown download kind", func(in *oas.PriorityPresetInput) { in.Download["stash"] = 10 }, 422},
		{"rank zero", func(in *oas.PriorityPresetInput) { in.Search["sonarr"] = 0 }, 422},
		{"rank 101", func(in *oas.PriorityPresetInput) { in.Download["sonarr"] = 101 }, 422},
		{"rank boundaries", func(in *oas.PriorityPresetInput) { in.Search["sonarr"] = 1; in.Download["sonarr"] = 100 }, 201},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newRouterFixture(t)
			_ = completeInitialSetup(t, f)
			csrf := getAdminCsrfToken(t, f)
			in := namedPresetBody("night")
			tc.mutate(&in)
			assertNamedPresetWriteValidation(t, f, csrf, in, tc.status)
		})
	}
}

func TestNamedPriorityPresetReadOnlyAndAuthorizationHTTP(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		namedPresetRequest(t, f, method, "/priority-presets/media_first", csrf, namedPresetBody("edited"), http.StatusForbidden)
	}
	viewer, viewerCSRF := setupViewerClient(t, f)
	f.client = viewer
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/priority-presets"},
		{http.MethodPut, "/priority-presets/" + uuid.NewString()},
		{http.MethodDelete, "/priority-presets/" + uuid.NewString()},
		{http.MethodPost, "/priority-presets/media_first/apply"},
	} {
		namedPresetRequest(t, f, tc.method, tc.path, viewerCSRF, namedPresetBody("night"), http.StatusForbidden)
	}
}

func assertActivePriorityMaps(t *testing.T, f *routerFixture, want oas.PriorityPresetInput) {
	t.Helper()
	raw := namedPresetRequest(t, f, http.MethodGet, "/settings", "", nil, http.StatusOK)
	var got oas.Settings
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(got.SearchPriorityPreset, want.Search) || !maps.Equal(got.DownloadPriorityPreset, want.Download) {
		t.Fatalf("active search/download = %v/%v; want %v/%v", got.SearchPriorityPreset, got.DownloadPriorityPreset, want.Search, want.Download)
	}
}

func TestNamedPriorityPresetApplyCopiesHTTP(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	in := namedPresetBody("night")
	in.Search["lidarr"], in.Search["sonarr"] = 1, 100
	in.Download["readarr"], in.Download["sonarr"] = 1, 80
	night := createNamedPreset(t, f, csrf, in)
	path := "/priority-presets/" + night.ID.Value.String()
	namedPresetRequest(t, f, http.MethodPost, path+"/apply", csrf, nil, http.StatusOK)
	assertActivePriorityMaps(t, f, in)
	assertNamedPresetLeaseOrder(t, f)
	// Changing and deleting the saved snapshot must not change the applied maps.
	namedPresetRequest(t, f, http.MethodPut, path, csrf, namedPresetBody("renamed"), http.StatusOK)
	assertActivePriorityMaps(t, f, in)
	namedPresetRequest(t, f, http.MethodDelete, path, csrf, nil, http.StatusNoContent)
	assertActivePriorityMaps(t, f, in)
	namedPresetRequest(t, f, http.MethodPost, "/priority-presets/media_first/apply", csrf, nil, http.StatusOK)
	assertActivePriorityMaps(t, f, namedPresetBody("media_first"))
}

func assertNamedPresetLeaseOrder(t *testing.T, f *routerFixture) {
	t.Helper()
	var lidarrID uuid.UUID
	for _, kind := range []domain.AppKind{domain.KindSonarr, domain.KindLidarr} {
		inst, err := f.h.instances.Create(t.Context(), instances.Input{
			Kind: kind, Name: string(kind), BaseURL: "http://" + string(kind) + ".invalid:8989", APIKey: "test-secret-key", Enabled: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if kind == domain.KindLidarr {
			lidarrID = inst.ID
		}
		if _, _, err := f.h.runs.Enqueue(t.Context(), inst.ID, domain.SnatchMissing); err != nil {
			t.Fatal(err)
		}
	}
	leased, err := f.h.runs.Lease(t.Context(), "priority-preset-test")
	if err != nil || leased.InstanceID != lidarrID {
		t.Fatalf("lease = %+v, %v; want lidarr %v", leased, err, lidarrID)
	}
}
