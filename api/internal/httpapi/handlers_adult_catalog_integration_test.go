// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func seedCatalogData(t *testing.T, f *routerFixture) (perfA, perfB, perfC, studioA, studioB, studioC uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()

	perfA = uuid.MustParse("018f0000-0000-7000-8000-0000000000a1")
	perfB = uuid.MustParse("018f0000-0000-7000-8000-0000000000a2")
	perfC = uuid.MustParse("018f0000-0000-7000-8000-0000000000a3")

	studioA = uuid.MustParse("018f0000-0000-7000-8000-0000000000b1")
	studioB = uuid.MustParse("018f0000-0000-7000-8000-0000000000b2")
	studioC = uuid.MustParse("018f0000-0000-7000-8000-0000000000b3")

	dis := "Series 1"
	canonURLA := "https://example.com/perf-a"
	canonURLB := "https://example.com/perf-b"
	canonURLStudio := "https://example.com/studio-a"

	// Upsert performers
	_, err := f.st.Q().UpsertAdultPerformer(ctx, sqlcgen.UpsertAdultPerformerParams{
		ID:             perfA,
		CanonicalName:  "Performer Alpha",
		Disambiguation: &dis,
		Urls:           []string{"https://profile.example/a"},
		IsFavorite:     true,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		t.Fatalf("upsert perfA: %v", err)
	}

	_, err = f.st.Q().UpsertAdultPerformer(ctx, sqlcgen.UpsertAdultPerformerParams{
		ID:            perfB,
		CanonicalName: "Performer Beta",
		Urls:          []string{},
		IsFavorite:    false,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("upsert perfB: %v", err)
	}

	_, err = f.st.Q().UpsertAdultPerformer(ctx, sqlcgen.UpsertAdultPerformerParams{
		ID:            perfC,
		CanonicalName: "Performer Gamma",
		Urls:          []string{"https://profile.example/c"},
		IsFavorite:    true,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("upsert perfC: %v", err)
	}

	// External refs for Performer A (2 refs)
	_, err = f.st.Q().UpsertAdultExternalRef(ctx, sqlcgen.UpsertAdultExternalRefParams{
		ID:           uuid.New(),
		EntityType:   "performer",
		EntityID:     perfA,
		SourceKind:   "stashbox",
		ForeignID:    "ext-perf-a1",
		CanonicalUrl: &canonURLA,
		ExtraUrls:    []string{},
		IsFavorite:   true,
		FetchedAt:    now,
		LastSeenAt:   now,
	})
	if err != nil {
		t.Fatalf("upsert ref perfA 1: %v", err)
	}

	_, err = f.st.Q().UpsertAdultExternalRef(ctx, sqlcgen.UpsertAdultExternalRefParams{
		ID:           uuid.New(),
		EntityType:   "performer",
		EntityID:     perfA,
		SourceKind:   "tpdb_rest",
		ForeignID:    "ext-perf-a2",
		CanonicalUrl: nil,
		ExtraUrls:    []string{"https://tpdb.example/a"},
		IsFavorite:   false,
		FetchedAt:    now,
		LastSeenAt:   now,
	})
	if err != nil {
		t.Fatalf("upsert ref perfA 2: %v", err)
	}

	// External ref for Performer B (1 ref)
	_, err = f.st.Q().UpsertAdultExternalRef(ctx, sqlcgen.UpsertAdultExternalRefParams{
		ID:           uuid.New(),
		EntityType:   "performer",
		EntityID:     perfB,
		SourceKind:   "stashbox",
		ForeignID:    "ext-perf-b",
		CanonicalUrl: &canonURLB,
		ExtraUrls:    []string{},
		IsFavorite:   false,
		FetchedAt:    now,
		LastSeenAt:   now,
	})
	if err != nil {
		t.Fatalf("upsert ref perfB: %v", err)
	}

	// Upsert studios
	_, err = f.st.Q().UpsertAdultStudio(ctx, sqlcgen.UpsertAdultStudioParams{
		ID:            studioA,
		CanonicalName: "Studio Alpha",
		Urls:          []string{"https://studio.example/a"},
		IsFavorite:    true,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("upsert studioA: %v", err)
	}

	_, err = f.st.Q().UpsertAdultStudio(ctx, sqlcgen.UpsertAdultStudioParams{
		ID:            studioB,
		CanonicalName: "Studio Beta",
		Urls:          []string{},
		IsFavorite:    false,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("upsert studioB: %v", err)
	}

	_, err = f.st.Q().UpsertAdultStudio(ctx, sqlcgen.UpsertAdultStudioParams{
		ID:            studioC,
		CanonicalName: "Studio Gamma",
		Urls:          []string{"https://studio.example/c"},
		IsFavorite:    true,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("upsert studioC: %v", err)
	}

	// External refs for Studio A (2 refs)
	_, err = f.st.Q().UpsertAdultExternalRef(ctx, sqlcgen.UpsertAdultExternalRefParams{
		ID:           uuid.New(),
		EntityType:   "studio",
		EntityID:     studioA,
		SourceKind:   "stashbox",
		ForeignID:    "ext-studio-a1",
		CanonicalUrl: &canonURLStudio,
		ExtraUrls:    []string{},
		IsFavorite:   true,
		FetchedAt:    now,
		LastSeenAt:   now,
	})
	if err != nil {
		t.Fatalf("upsert ref studioA 1: %v", err)
	}

	_, err = f.st.Q().UpsertAdultExternalRef(ctx, sqlcgen.UpsertAdultExternalRefParams{
		ID:           uuid.New(),
		EntityType:   "studio",
		EntityID:     studioA,
		SourceKind:   "whisparr_v3",
		ForeignID:    "ext-studio-a2",
		CanonicalUrl: nil,
		ExtraUrls:    []string{},
		IsFavorite:   false,
		FetchedAt:    now,
		LastSeenAt:   now,
	})
	if err != nil {
		t.Fatalf("upsert ref studioA 2: %v", err)
	}

	// External ref for Studio B (1 ref)
	_, err = f.st.Q().UpsertAdultExternalRef(ctx, sqlcgen.UpsertAdultExternalRefParams{
		ID:           uuid.New(),
		EntityType:   "studio",
		EntityID:     studioB,
		SourceKind:   "stashbox",
		ForeignID:    "ext-studio-b",
		CanonicalUrl: nil,
		ExtraUrls:    []string{},
		IsFavorite:   false,
		FetchedAt:    now,
		LastSeenAt:   now,
	})
	if err != nil {
		t.Fatalf("upsert ref studioB: %v", err)
	}

	return perfA, perfB, perfC, studioA, studioB, studioC
}

func TestAdultCatalog_Boundary_EmptyTable(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)
	enableAdultModuleForTest(t, f, adminCsrf)

	// Performers empty list
	resPerf := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/performers", "", nil)
	if resPerf.statusCode != http.StatusOK {
		t.Fatalf("performers status = %d, want 200; body: %s", resPerf.statusCode, string(resPerf.body))
	}
	var perfPage map[string]any
	if err := json.Unmarshal(resPerf.body, &perfPage); err != nil {
		t.Fatalf("unmarshal perfPage: %v", err)
	}
	itemsPerf, ok := perfPage["items"].([]any)
	if !ok || len(itemsPerf) != 0 {
		t.Errorf("expected empty performers items array, got: %+v", perfPage["items"])
	}
	if perfPage["next_cursor"] != nil {
		t.Errorf("expected no next_cursor on empty table, got: %v", perfPage["next_cursor"])
	}

	// Studios empty list
	resStudio := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/studios", "", nil)
	if resStudio.statusCode != http.StatusOK {
		t.Fatalf("studios status = %d, want 200; body: %s", resStudio.statusCode, string(resStudio.body))
	}
	var studioPage map[string]any
	if err := json.Unmarshal(resStudio.body, &studioPage); err != nil {
		t.Fatalf("unmarshal studioPage: %v", err)
	}
	itemsStudio, ok := studioPage["items"].([]any)
	if !ok || len(itemsStudio) != 0 {
		t.Errorf("expected empty studios items array, got: %+v", studioPage["items"])
	}
	if studioPage["next_cursor"] != nil {
		t.Errorf("expected no next_cursor on empty table, got: %v", studioPage["next_cursor"])
	}
}

func TestAdultCatalog_Negative_ModuleOff_And_UnknownID(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	randomID := uuid.New().String()

	// 1. Module disabled (default): every route returns 404 Problem Details
	assertProblemStatusAndType(t, doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/performers", "", nil), http.StatusNotFound)
	assertProblemStatusAndType(t, doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/performers/"+randomID, "", nil), http.StatusNotFound)
	assertProblemStatusAndType(t, doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/studios", "", nil), http.StatusNotFound)
	assertProblemStatusAndType(t, doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/studios/"+randomID, "", nil), http.StatusNotFound)

	// 2. Enable module: unknown entity UUID returns 404 Problem Details
	enableAdultModuleForTest(t, f, adminCsrf)
	assertProblemStatusAndType(t, doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/performers/"+randomID, "", nil), http.StatusNotFound)
	assertProblemStatusAndType(t, doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/studios/"+randomID, "", nil), http.StatusNotFound)
}

func TestAdultCatalog_Negative_LimitValidation(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)
	enableAdultModuleForTest(t, f, adminCsrf)

	// limit 0 -> 400 or 422 problem details
	assertProblemClientRejected(t, doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/performers?limit=0", "", nil))
	assertProblemClientRejected(t, doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/studios?limit=0", "", nil))

	// limit 201 -> 400 or 422 problem details
	assertProblemClientRejected(t, doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/performers?limit=201", "", nil))
	assertProblemClientRejected(t, doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/studios?limit=201", "", nil))
}

func TestAdultCatalog_Positive_PerformersListGet_Filter_Cursor_BoundaryLimit1(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)
	enableAdultModuleForTest(t, f, adminCsrf)
	perfA, perfB, perfC, _, _, _ := seedCatalogData(t, f)

	// 1. List all performers
	resList := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/performers", "", nil)
	if resList.statusCode != http.StatusOK {
		t.Fatalf("list performers status = %d, want 200", resList.statusCode)
	}
	var pageAll map[string]any
	if err := json.Unmarshal(resList.body, &pageAll); err != nil {
		t.Fatalf("unmarshal pageAll: %v", err)
	}
	itemsAll := pageAll["items"].([]any)
	if len(itemsAll) != 3 {
		t.Fatalf("expected 3 performers, got %d", len(itemsAll))
	}
	firstPerf := itemsAll[0].(map[string]any)
	if firstPerf["name"] != "Performer Alpha" || int(firstPerf["ref_count"].(float64)) != 2 {
		t.Errorf("unexpected first performer: %+v", firstPerf)
	}

	// 2. Favorite filter = true
	resFav := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/performers?favorite=true", "", nil)
	if resFav.statusCode != http.StatusOK {
		t.Fatalf("favorite true status = %d, want 200", resFav.statusCode)
	}
	var pageFav map[string]any
	if err := json.Unmarshal(resFav.body, &pageFav); err != nil {
		t.Fatalf("unmarshal pageFav: %v", err)
	}
	itemsFav := pageFav["items"].([]any)
	if len(itemsFav) != 2 {
		t.Fatalf("expected 2 favorite performers (Alpha, Gamma), got %d", len(itemsFav))
	}

	// Favorite filter = false
	resNonFav := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/performers?favorite=false", "", nil)
	var pageNonFav map[string]any
	_ = json.Unmarshal(resNonFav.body, &pageNonFav)
	itemsNonFav := pageNonFav["items"].([]any)
	if len(itemsNonFav) != 1 || itemsNonFav[0].(map[string]any)["name"] != "Performer Beta" {
		t.Errorf("expected 1 non-favorite performer Beta, got %+v", itemsNonFav)
	}

	// 3. Keyset cursor pagination with boundary limit 1
	resP1 := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/performers?limit=1", "", nil)
	if resP1.statusCode != http.StatusOK {
		t.Fatalf("limit 1 status = %d, want 200", resP1.statusCode)
	}
	var pageP1 map[string]any
	_ = json.Unmarshal(resP1.body, &pageP1)
	itemsP1 := pageP1["items"].([]any)
	if len(itemsP1) != 1 || itemsP1[0].(map[string]any)["id"] != perfA.String() {
		t.Fatalf("expected page 1 to have Performer Alpha, got %+v", itemsP1)
	}
	cursor1, _ := pageP1["next_cursor"].(string)
	if cursor1 == "" {
		t.Fatalf("expected non-empty next_cursor for page 1")
	}

	// Page 2 using cursor
	p2URL := f.srv.URL + PathPrefix + "/adult/performers?limit=1&cursor=" + url.QueryEscape(cursor1)
	resP2 := doHTTP(t, f.client, http.MethodGet, p2URL, "", nil)
	if resP2.statusCode != http.StatusOK {
		t.Fatalf("page 2 status = %d, want 200", resP2.statusCode)
	}
	var pageP2 map[string]any
	_ = json.Unmarshal(resP2.body, &pageP2)
	itemsP2 := pageP2["items"].([]any)
	if len(itemsP2) != 1 || itemsP2[0].(map[string]any)["id"] != perfB.String() {
		t.Fatalf("expected page 2 to have Performer Beta, got %+v", itemsP2)
	}
	cursor2, _ := pageP2["next_cursor"].(string)
	if cursor2 == "" {
		t.Fatalf("expected non-empty next_cursor for page 2")
	}

	// Page 3 using cursor
	p3URL := f.srv.URL + PathPrefix + "/adult/performers?limit=1&cursor=" + url.QueryEscape(cursor2)
	resP3 := doHTTP(t, f.client, http.MethodGet, p3URL, "", nil)
	var pageP3 map[string]any
	_ = json.Unmarshal(resP3.body, &pageP3)
	itemsP3 := pageP3["items"].([]any)
	if len(itemsP3) != 1 || itemsP3[0].(map[string]any)["id"] != perfC.String() {
		t.Fatalf("expected page 3 to have Performer Gamma, got %+v", itemsP3)
	}
	if pageP3["next_cursor"] != nil {
		t.Errorf("expected last page to have no next_cursor, got %v", pageP3["next_cursor"])
	}

	// 4. Detail GET /adult/performers/{performerId} with external_refs
	resDetail := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/performers/"+perfA.String(), "", nil)
	if resDetail.statusCode != http.StatusOK {
		t.Fatalf("get performer A status = %d, want 200", resDetail.statusCode)
	}
	var detail map[string]any
	if err := json.Unmarshal(resDetail.body, &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail["name"] != "Performer Alpha" || detail["disambiguation"] != "Series 1" {
		t.Errorf("unexpected detail: %+v", detail)
	}
	refs, ok := detail["external_refs"].([]any)
	if !ok || len(refs) != 2 {
		t.Fatalf("expected 2 external_refs, got %d (%+v)", len(refs), detail["external_refs"])
	}
	ref1 := refs[0].(map[string]any)
	if ref1["source_kind"] != "stashbox" || ref1["foreign_id"] != "ext-perf-a1" || ref1["is_favorite"] != true {
		t.Errorf("unexpected ref 1: %+v", ref1)
	}
	if ref1["url"] != "https://example.com/perf-a" {
		t.Errorf("unexpected ref 1 url: %v", ref1["url"])
	}
}

func TestAdultCatalog_Positive_StudiosListGet_Filter_Cursor_BoundaryLimit1(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)
	enableAdultModuleForTest(t, f, adminCsrf)
	_, _, _, studioA, studioB, studioC := seedCatalogData(t, f)

	// 1. List all studios
	resList := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/studios", "", nil)
	if resList.statusCode != http.StatusOK {
		t.Fatalf("list studios status = %d, want 200", resList.statusCode)
	}
	var pageAll map[string]any
	_ = json.Unmarshal(resList.body, &pageAll)
	itemsAll := pageAll["items"].([]any)
	if len(itemsAll) != 3 {
		t.Fatalf("expected 3 studios, got %d", len(itemsAll))
	}
	firstStudio := itemsAll[0].(map[string]any)
	if firstStudio["name"] != "Studio Alpha" || int(firstStudio["ref_count"].(float64)) != 2 {
		t.Errorf("unexpected first studio: %+v", firstStudio)
	}

	// 2. Favorite filter
	resFav := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/studios?favorite=true", "", nil)
	var pageFav map[string]any
	_ = json.Unmarshal(resFav.body, &pageFav)
	itemsFav := pageFav["items"].([]any)
	if len(itemsFav) != 2 {
		t.Fatalf("expected 2 favorite studios, got %d", len(itemsFav))
	}

	// 3. Cursor pagination with boundary limit 1
	resP1 := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/studios?limit=1", "", nil)
	var pageP1 map[string]any
	_ = json.Unmarshal(resP1.body, &pageP1)
	itemsP1 := pageP1["items"].([]any)
	if len(itemsP1) != 1 || itemsP1[0].(map[string]any)["id"] != studioA.String() {
		t.Fatalf("expected page 1 studio Alpha, got %+v", itemsP1)
	}
	cursor1, _ := pageP1["next_cursor"].(string)

	p2URL := f.srv.URL + PathPrefix + "/adult/studios?limit=1&cursor=" + url.QueryEscape(cursor1)
	resP2 := doHTTP(t, f.client, http.MethodGet, p2URL, "", nil)
	var pageP2 map[string]any
	_ = json.Unmarshal(resP2.body, &pageP2)
	itemsP2 := pageP2["items"].([]any)
	if len(itemsP2) != 1 || itemsP2[0].(map[string]any)["id"] != studioB.String() {
		t.Fatalf("expected page 2 studio Beta, got %+v", itemsP2)
	}
	cursor2, _ := pageP2["next_cursor"].(string)

	p3URL := f.srv.URL + PathPrefix + "/adult/studios?limit=1&cursor=" + url.QueryEscape(cursor2)
	resP3 := doHTTP(t, f.client, http.MethodGet, p3URL, "", nil)
	var pageP3 map[string]any
	_ = json.Unmarshal(resP3.body, &pageP3)
	itemsP3 := pageP3["items"].([]any)
	if len(itemsP3) != 1 || itemsP3[0].(map[string]any)["id"] != studioC.String() {
		t.Fatalf("expected page 3 studio Gamma, got %+v", itemsP3)
	}

	// 4. Detail GET /adult/studios/{studioId}
	resDetail := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/studios/"+studioA.String(), "", nil)
	if resDetail.statusCode != http.StatusOK {
		t.Fatalf("get studio A status = %d, want 200", resDetail.statusCode)
	}
	var detail map[string]any
	_ = json.Unmarshal(resDetail.body, &detail)
	if detail["name"] != "Studio Alpha" {
		t.Errorf("unexpected studio detail: %+v", detail)
	}
	refs, ok := detail["external_refs"].([]any)
	if !ok || len(refs) != 2 {
		t.Fatalf("expected 2 external_refs, got %d", len(refs))
	}
}
