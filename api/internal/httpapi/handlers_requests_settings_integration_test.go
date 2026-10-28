// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/requests"
)

func TestRequestsSettingsDirectHandler(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)

	admin := domain.User{ID: uuid.New(), Username: "admin-req", Role: domain.RoleAdmin}
	viewer := domain.User{ID: uuid.New(), Username: "viewer-req", Role: domain.RoleViewer}
	seedTestUser(t, f, admin)
	seedTestUser(t, f, viewer)

	adminCtx := withUser(t.Context(), admin)
	viewerCtx := withUser(t.Context(), viewer)

	// 1. Positive: Get default settings when none stored
	def, err := f.h.GetRequestsSettings(viewerCtx)
	if err != nil {
		t.Fatalf("GetRequestsSettings() default err = %v", err)
	}
	if def.VotingMode != oas.RequestsVotingModeUpvotesOnly {
		t.Fatalf("default VotingMode = %q, want %q", def.VotingMode, oas.RequestsVotingModeUpvotesOnly)
	}
	if !def.AllowViewerVotes {
		t.Fatalf("default AllowViewerVotes = %v, want true", def.AllowViewerVotes)
	}
	if def.Visibility != oas.RequestsVisibilityPublicAttributed {
		t.Fatalf("default Visibility = %q, want %q", def.Visibility, oas.RequestsVisibilityPublicAttributed)
	}
	if def.VotingTTLDays != 0 {
		t.Fatalf("default VotingTTLDays = %d, want 0", def.VotingTTLDays)
	}

	// 2. Negative: Viewer PUT 403 Forbidden
	_, err = f.h.UpdateRequestsSettings(viewerCtx, &oas.RequestsSettings{
		VotingMode:       oas.RequestsVotingModeUpvotesOnly,
		AllowViewerVotes: true,
		Visibility:       oas.RequestsVisibilityPublicAttributed,
		VotingTTLDays:    7,
	})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer UpdateRequestsSettings() err = %v, want ErrForbidden", err)
	}

	// 3. Negative: Invalid voting_mode returns 4xx (ErrInvalid)
	_, err = f.h.UpdateRequestsSettings(adminCtx, &oas.RequestsSettings{
		VotingMode:       "invalid_mode",
		AllowViewerVotes: true,
		Visibility:       oas.RequestsVisibilityPublicAttributed,
		VotingTTLDays:    0,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid voting_mode err = %v, want ErrInvalid", err)
	}

	// 4. Negative: voting_ttl_days out of bounds (negative) returns 4xx (ErrInvalid)
	_, err = f.h.UpdateRequestsSettings(adminCtx, &oas.RequestsSettings{
		VotingMode:       oas.RequestsVotingModeUpvotesOnly,
		AllowViewerVotes: true,
		Visibility:       oas.RequestsVisibilityPublicAttributed,
		VotingTTLDays:    -1,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("negative voting_ttl_days err = %v, want ErrInvalid", err)
	}

	// 5. Boundary: TTL at min allowed by Validate (0)
	minUpdated, err := f.h.UpdateRequestsSettings(adminCtx, &oas.RequestsSettings{
		VotingMode:       oas.RequestsVotingModeUpvotesOnly,
		AllowViewerVotes: true,
		Visibility:       oas.RequestsVisibilityPublicAttributed,
		VotingTTLDays:    0,
	})
	if err != nil {
		t.Fatalf("UpdateRequestsSettings() with min ttl 0 err = %v", err)
	}
	if minUpdated.VotingTTLDays != 0 {
		t.Fatalf("minUpdated.VotingTTLDays = %d, want 0", minUpdated.VotingTTLDays)
	}

	// 6. Boundary: TTL at max allowed by Validate (math.MaxInt32 / large positive)
	maxUpdated, err := f.h.UpdateRequestsSettings(adminCtx, &oas.RequestsSettings{
		VotingMode:       oas.RequestsVotingModeUpvotesOnly,
		AllowViewerVotes: true,
		Visibility:       oas.RequestsVisibilityPublicAttributed,
		VotingTTLDays:    math.MaxInt32,
	})
	if err != nil {
		t.Fatalf("UpdateRequestsSettings() with max ttl err = %v", err)
	}
	if maxUpdated.VotingTTLDays != math.MaxInt32 {
		t.Fatalf("maxUpdated.VotingTTLDays = %d, want %d", maxUpdated.VotingTTLDays, math.MaxInt32)
	}

	// 7. Positive: Put + Get round trip
	changed := &oas.RequestsSettings{
		VotingMode:       oas.RequestsVotingModeNetScore,
		AllowViewerVotes: false,
		Visibility:       oas.RequestsVisibilityPrivateToRequesterAndAdmin,
		VotingTTLDays:    14,
	}
	updated, err := f.h.UpdateRequestsSettings(adminCtx, changed)
	if err != nil {
		t.Fatalf("UpdateRequestsSettings() err = %v", err)
	}
	if updated.VotingMode != changed.VotingMode ||
		updated.AllowViewerVotes != changed.AllowViewerVotes ||
		updated.Visibility != changed.Visibility ||
		updated.VotingTTLDays != changed.VotingTTLDays {
		t.Fatalf("updated = %+v, want %+v", updated, changed)
	}

	fetched, err := f.h.GetRequestsSettings(viewerCtx)
	if err != nil {
		t.Fatalf("GetRequestsSettings() after update err = %v", err)
	}
	if fetched.VotingMode != changed.VotingMode ||
		fetched.AllowViewerVotes != changed.AllowViewerVotes ||
		fetched.Visibility != changed.Visibility ||
		fetched.VotingTTLDays != changed.VotingTTLDays {
		t.Fatalf("fetched = %+v, want %+v", fetched, changed)
	}
}

func TestRequestsSettingsHTTPEndpoints(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	// 1. Positive: GET default values
	getRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/requests/settings", "", nil)
	if getRes.statusCode != http.StatusOK {
		t.Fatalf("GET /requests/settings status = %d, want 200; body: %s", getRes.statusCode, string(getRes.body))
	}
	var gotDef requests.Settings
	if err := json.Unmarshal(getRes.body, &gotDef); err != nil {
		t.Fatalf("unmarshal default: %v", err)
	}
	if gotDef != requests.DefaultSettings() {
		t.Fatalf("got default = %+v, want %+v", gotDef, requests.DefaultSettings())
	}

	// 2. Negative: Unauthenticated PUT -> 401
	anonRes := doHTTP(t, &http.Client{}, http.MethodPut, f.srv.URL+PathPrefix+"/requests/settings", "", map[string]any{
		"voting_mode":        "net_score",
		"allow_viewer_votes": false,
		"visibility":         "public_anonymous",
		"voting_ttl_days":    7,
	})
	assertProblemStatusAndType(t, anonRes, http.StatusUnauthorized)

	// 3. Negative: Viewer PUT -> 403
	viewerClient, viewerCsrf := loginViewerSession(t, f)
	viewerPutRes := doHTTP(t, viewerClient, http.MethodPut, f.srv.URL+PathPrefix+"/requests/settings", viewerCsrf, map[string]any{
		"voting_mode":        "net_score",
		"allow_viewer_votes": false,
		"visibility":         "public_anonymous",
		"voting_ttl_days":    7,
	})
	assertProblemStatusAndType(t, viewerPutRes, http.StatusForbidden)

	// 4. Negative: Invalid voting_mode -> 4xx problem+json
	badModeRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/requests/settings", adminCsrf, map[string]any{
		"voting_mode":        "super_score",
		"allow_viewer_votes": true,
		"visibility":         "public_attributed",
		"voting_ttl_days":    0,
	})
	assertProblemStatusAndType(t, badModeRes, http.StatusUnprocessableEntity)

	// 5. Negative: Out of bounds negative voting_ttl_days -> 4xx problem+json
	badTTLRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/requests/settings", adminCsrf, map[string]any{
		"voting_mode":        "upvotes_only",
		"allow_viewer_votes": true,
		"visibility":         "public_attributed",
		"voting_ttl_days":    -5,
	})
	assertProblemStatusAndType(t, badTTLRes, http.StatusUnprocessableEntity)

	// 6. Positive: Round trip PUT valid settings -> 200, GET returns them
	putPayload := map[string]any{
		"voting_mode":        "net_score",
		"allow_viewer_votes": false,
		"visibility":         "public_anonymous",
		"voting_ttl_days":    30,
	}
	putRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/requests/settings", adminCsrf, putPayload)
	if putRes.statusCode != http.StatusOK {
		t.Fatalf("PUT /requests/settings status = %d, want 200; body: %s", putRes.statusCode, string(putRes.body))
	}
	var updated requests.Settings
	if err := json.Unmarshal(putRes.body, &updated); err != nil {
		t.Fatalf("unmarshal updated: %v", err)
	}
	if updated.VotingMode != requests.VotingModeNetScore ||
		updated.AllowViewerVotes != false ||
		updated.Visibility != requests.VisibilityPublicAnonymous ||
		updated.VotingTTLDays != 30 {
		t.Fatalf("unexpected updated settings: %+v", updated)
	}

	afterGetRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/requests/settings", "", nil)
	if afterGetRes.statusCode != http.StatusOK {
		t.Fatalf("GET /requests/settings after PUT status = %d", afterGetRes.statusCode)
	}
	var reFetched requests.Settings
	if err := json.Unmarshal(afterGetRes.body, &reFetched); err != nil {
		t.Fatalf("unmarshal reFetched: %v", err)
	}
	if reFetched != updated {
		t.Fatalf("reFetched = %+v, want %+v", reFetched, updated)
	}
}
