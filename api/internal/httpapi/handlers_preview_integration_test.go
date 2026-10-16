// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

type previewCase struct {
	name        string
	useUnknown  bool
	kind        string
	preEnqueue  bool
	wantStatus  int
	wantProblem bool
}

// TestPreviewRun_HTTPStatusCodes verifies 202 with non-zero seed, 404, 422, and 409 over HTTP.
func TestPreviewRun_HTTPStatusCodes(t *testing.T) {
	t.Parallel()
	cases := []previewCase{
		{name: "positive preview accepted with seed", kind: "missing", wantStatus: http.StatusAccepted},
		{name: "negative unknown instance 404", useUnknown: true, kind: "missing", wantStatus: http.StatusNotFound, wantProblem: true},
		{name: "boundary invalid kind 422", kind: "not-a-valid-kind", wantStatus: http.StatusUnprocessableEntity, wantProblem: true},
		{name: "negative duplicate run 409", kind: "missing", preEnqueue: true, wantStatus: http.StatusConflict, wantProblem: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runPreviewCase(t, tc)
		})
	}
}

func runPreviewCase(t *testing.T, tc previewCase) {
	t.Helper()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrfToken := getAdminCsrfToken(t, f)
	instID := seedTestInstance(t, f.integrationFixture)

	targetID := instID
	if tc.useUnknown {
		targetID = uuid.New()
	}
	if tc.preEnqueue {
		if _, _, err := f.h.runs.Enqueue(t.Context(), instID, domain.SnatchMissing); err != nil {
			t.Fatalf("pre-enqueue run: %v", err)
		}
	}

	url := fmt.Sprintf("%s%s/instances/%s/preview?kind=%s", f.srv.URL, PathPrefix, targetID, tc.kind)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Csrf-Token", csrfToken)

	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("POST preview: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != tc.wantStatus {
		t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
	}
	if tc.wantProblem {
		assertProblem(t, resp, tc.wantStatus)
		return
	}
	verifyPreviewAccepted(t, f, resp, instID)
}

func verifyPreviewAccepted(t *testing.T, f *routerFixture, resp *http.Response, instID uuid.UUID) {
	t.Helper()
	var res oas.PreviewResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode PreviewResult: %v", err)
	}
	if res.RunID == uuid.Nil || res.Seed == 0 {
		t.Fatalf("invalid PreviewResult: %+v", res)
	}
	run, err := f.h.runs.Get(t.Context(), res.RunID)
	if err != nil {
		t.Fatalf("runs.Get(%s): %v", res.RunID, err)
	}
	if !run.DryRun || run.Seed != res.Seed || run.InstanceID != instID {
		t.Fatalf("stored run mismatch: dryRun=%v, seed=%d, instance=%s", run.DryRun, run.Seed, run.InstanceID)
	}
}

// TestGetRun_ReturnsPreviewedTargets verifies GET /runs/{runId} returns previewed targets.
func TestGetRun_ReturnsPreviewedTargets(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	instID := seedTestInstance(t, f.integrationFixture)
	ctx := t.Context()

	run, _, err := f.h.runs.EnqueueWithOptions(ctx, snatch.EnqueueOptions{
		InstanceID: instID,
		Kind:       domain.SnatchMissing,
		DryRun:     true,
		Seed:       42,
	})
	if err != nil {
		t.Fatalf("EnqueueWithOptions: %v", err)
	}

	err = f.h.rec.Record(ctx, domain.Event{
		RunID:      &run.ID,
		InstanceID: instID,
		Type:       "search_previewed",
		EntityType: "movie",
		EntityID:   101,
		Title:      "Ghost In The Shell",
		Detail:     "1",
	})
	if err != nil {
		t.Fatalf("Record preview event: %v", err)
	}

	getAndAssertRun(t, f, run.ID)
}

func getAndAssertRun(t *testing.T, f *routerFixture, runID uuid.UUID) {
	t.Helper()
	url := fmt.Sprintf("%s%s/runs/%s", f.srv.URL, PathPrefix, runID)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("GET run: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET run status = %d, want 200", resp.StatusCode)
	}
	var got oas.Run
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode Run: %v", err)
	}
	if !got.DryRun.Value || got.Seed.Value != 42 {
		t.Errorf("got dryRun=%v, seed=%d; want true, 42", got.DryRun.Value, got.Seed.Value)
	}
	if len(got.PreviewedTargets) != 1 {
		t.Fatalf("previewed targets len = %d, want 1", len(got.PreviewedTargets))
	}
	target := got.PreviewedTargets[0]
	if target.EntityType != "movie" || target.ID != 101 || target.Title != "Ghost In The Shell" || target.Cost != 1 {
		t.Errorf("preview target mismatch: %+v", target)
	}
}

// TestPolicy_DryRunRoundTripHTTP verifies policy dry_run round-trips over the HTTP API.
func TestPolicy_DryRunRoundTripHTTP(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrfToken := getAdminCsrfToken(t, f)
	instID := seedTestInstance(t, f.integrationFixture)

	policyURL := fmt.Sprintf("%s%s/instances/%s/policy", f.srv.URL, PathPrefix, instID)
	pol := fetchPolicyHTTP(t, f, policyURL)
	if pol.DryRun.Value {
		t.Fatal("initial policy dry_run = true, want false")
	}

	pol.DryRun = oas.NewOptBool(true)
	updatePolicyHTTP(t, f, policyURL, csrfToken, pol)

	after := fetchPolicyHTTP(t, f, policyURL)
	if !after.DryRun.Value {
		t.Errorf("policy dry_run after update = false, want true")
	}
}

func fetchPolicyHTTP(t *testing.T, f *routerFixture, url string) oas.SnatchPolicy {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("GET policy: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET policy status = %d, want 200", resp.StatusCode)
	}
	var pol oas.SnatchPolicy
	if err := json.NewDecoder(resp.Body).Decode(&pol); err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	return pol
}

func updatePolicyHTTP(t *testing.T, f *routerFixture, url, csrfToken string, pol oas.SnatchPolicy) {
	t.Helper()
	body, err := json.Marshal(pol)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Csrf-Token", csrfToken)
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("PUT policy: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT policy status = %d, want 200; body: %s", resp.StatusCode, string(raw))
	}
}
