// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

// configarrDeleteCase seeds one instance and states what DELETE /instances/{id} must answer.
type configarrDeleteCase struct {
	name    string
	enabled bool
	orphan  bool
	want    int
}

// TestRouterConfigarrDeleteRule drives DELETE /instances/{id} over HTTP for instances that the
// Configarr configuration declares (enabled or not) and for one it no longer declares.
func TestRouterConfigarrDeleteRule(t *testing.T) {
	t.Parallel()
	cases := []configarrDeleteCase{
		{name: "declared enabled", enabled: true, want: http.StatusForbidden},
		{name: "declared enabled false", enabled: false, want: http.StatusForbidden},
		{name: "orphaned", enabled: true, orphan: true, want: http.StatusNoContent},
		{name: "orphaned after enabled false", enabled: false, orphan: true, want: http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newRouterFixture(t)
			admin := completeInitialSetup(t, f)
			rawKey, _, err := f.keys.Issue(t.Context(), admin.ID.String(), []string{"admin"})
			if err != nil {
				t.Fatalf("keys.Issue: %v", err)
			}
			instID := seedConfigarrInstance(t, f, tc)
			resp := deleteInstanceWithKey(t, f, rawKey, instID)
			defer func() { _ = resp.Body.Close() }()
			if tc.want == http.StatusNoContent {
				assertDeleted(t, f, resp, instID)
				return
			}
			assertProblem(t, resp, tc.want)
			if _, getErr := f.h.instances.Get(t.Context(), instID); getErr != nil {
				t.Fatalf("instance after refused delete: %v, want it kept", getErr)
			}
		})
	}
}

func seedConfigarrInstance(t *testing.T, f *routerFixture, tc configarrDeleteCase) uuid.UUID {
	t.Helper()
	out, err := f.h.instances.UpsertConfigarr(t.Context(), "sonarr.series", instances.Input{
		Kind:    domain.KindSonarr,
		Name:    "series",
		BaseURL: "http://sonarr-configarr.invalid:8989",
		APIKey:  "secret-test-key",
		Enabled: tc.enabled,
	})
	if err != nil {
		t.Fatalf("UpsertConfigarr: %v", err)
	}
	if tc.orphan {
		n, disErr := f.h.instances.DisableConfigarrNotIn(t.Context(), nil)
		if disErr != nil || n != 1 {
			t.Fatalf("DisableConfigarrNotIn = %d, %v; want 1", n, disErr)
		}
	}
	return out.Instance.ID
}

func deleteInstanceWithKey(t *testing.T, f *routerFixture, rawKey string, instID uuid.UUID) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodDelete,
		f.srv.URL+PathPrefix+"/instances/"+instID.String(), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("deleteInstance: %v", err)
	}
	return resp
}

func assertDeleted(t *testing.T, f *routerFixture, resp *http.Response, instID uuid.UUID) {
	t.Helper()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("deleteInstance status = %d, want 204", resp.StatusCode)
	}
	if _, err := f.h.instances.Get(t.Context(), instID); !instances.IsNotFound(err) {
		t.Fatalf("instance after delete: %v, want not found", err)
	}
}
