// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

type lpPageResponse struct {
	Items      []map[string]any `json:"items"`
	HasMore    bool             `json:"has_more"`
	NextCursor *string          `json:"next_cursor"`
}

func lpGetRulePage(t *testing.T, client *http.Client, base, rawQuery string) lpPageResponse {
	t.Helper()
	endpoint := base + "/library-processing/rules/page"
	if rawQuery != "" {
		endpoint += "?" + rawQuery
	}
	res := doHTTP(t, client, http.MethodGet, endpoint, "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200; body: %s", endpoint, res.statusCode, string(res.body))
	}
	var page lpPageResponse
	if err := json.Unmarshal(res.body, &page); err != nil {
		t.Fatalf("decode rule page: %v; body: %s", err, string(res.body))
	}
	if page.Items == nil {
		t.Fatalf("decode rule page items = nil, want non-nil []; body: %s", string(res.body))
	}
	return page
}

func lpWalkAllRulePages(t *testing.T, client *http.Client, base string, limit int) ([]map[string]any, int) {
	t.Helper()
	var collected []map[string]any
	var cursor string
	pages := 0
	for range 64 {
		q := url.Values{}
		if limit > 0 {
			q.Set("limit", strconv.Itoa(limit))
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		page := lpGetRulePage(t, client, base, q.Encode())
		pages++
		if limit > 0 && len(page.Items) > limit {
			t.Fatalf("page %d returned %d items, exceeds limit %d", pages, len(page.Items), limit)
		}
		collected = append(collected, page.Items...)
		if !page.HasMore {
			if page.NextCursor != nil {
				t.Fatalf("final page %d has_more=false but next_cursor=%q", pages, *page.NextCursor)
			}
			return collected, pages
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			t.Fatalf("non-final page %d has_more=true without next_cursor", pages)
		}
		cursor = *page.NextCursor
	}
	t.Fatal("pagination did not terminate within 64 pages")
	return nil, 0
}

func lpRuleIDs(items []map[string]any) []string {
	out := make([]string, 0, len(items))
	for _, m := range items {
		id, _ := m["id"].(string)
		out = append(out, id)
	}
	return out
}

func seedPrecedenceAndTieRules(t *testing.T, f *routerFixture, base, csrf string) {
	t.Helper()
	instA := lpCreateInstance(t, f, "lp-page-a")
	instB := lpCreateInstance(t, f, "lp-page-b")

	// Seed in intentionally scrambled scope order; pairs without clock advance share exact created_at
	// so tie-breaking on id ASC is exercised alongside scope_rank ASC and created_at ASC.
	lpSeedGlobalPrecedenceRule(t, f, base, csrf)

	lpSeedMediaTypePrecedenceRules(t, f, base, csrf)

	lpSeedInstancePrecedenceRules(t, f, base, csrf, instA, instB)

	lpSeedLibraryPrecedenceRules(t, f, base, csrf, instA)

	lpSeedLanguagePrecedenceRules(t, f, base, csrf)

	lpSeedItemOverridePrecedenceRules(t, f, base, csrf, instA)
}

func lpSeedGlobalPrecedenceRule(t *testing.T, f *routerFixture, base, csrf string) {
	t.Helper()
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("en"),
	})
	f.clk.Advance(time.Second)
}

func lpSeedMediaTypePrecedenceRules(t *testing.T, f *routerFixture, base, csrf string) {
	t.Helper()
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":      "media_type",
		"media_type": "movie",
		"rule":       lpRuleBody("en"),
	})
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":      "media_type",
		"media_type": "anime",
		"rule":       lpRuleBody("ja"),
	})
	f.clk.Advance(time.Microsecond)
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":      "media_type",
		"media_type": "episode",
		"rule":       lpRuleBody("de"),
	})
}

func lpSeedInstancePrecedenceRules(t *testing.T, f *routerFixture, base, csrf string, instA, instB any) {
	t.Helper()
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":       "instance",
		"instance_id": instA,
		"rule":        lpRuleBody("fr"),
	})
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":       "instance",
		"instance_id": instB,
		"rule":        lpRuleBody("es"),
	})
}

func lpSeedLibraryPrecedenceRules(t *testing.T, f *routerFixture, base, csrf string, instA any) {
	t.Helper()
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":       "library",
		"instance_id": instA,
		"root_path":   "/media/tv",
		"rule":        lpRuleBody("en"),
	})
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":       "library",
		"instance_id": instA,
		"root_path":   "/media/anime",
		"rule":        lpRuleBody("ja"),
	})
}

func lpSeedLanguagePrecedenceRules(t *testing.T, f *routerFixture, base, csrf string) {
	t.Helper()
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":         "user_language",
		"user_language": "de",
		"rule":          lpRuleBody("de"),
	})
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":         "user_language",
		"user_language": "fr",
		"rule":          lpRuleBody("fr"),
	})
}

func lpSeedItemOverridePrecedenceRules(t *testing.T, f *routerFixture, base, csrf string, instA any) {
	t.Helper()
	// Three item_override rules at the exact same timestamp -> pure UUID tie-break at rank 1.
	for _, itemID := range []string{"ep-3", "ep-1", "ep-2"} {
		lpCreateRule(t, f.client, base, csrf, map[string]any{
			"scope":       "item_override",
			"instance_id": instA,
			"item_id":     itemID,
			"rule":        lpRuleBody("en", "de"),
		})
	}
}

func assertStrictPrecedenceOrder(t *testing.T, items []map[string]any) {
	t.Helper()
	for i := 1; i < len(items); i++ {
		prev := items[i-1]
		curr := items[i]
		prevScope, err := libraryprocessing.ParseScope(prev["scope"].(string))
		if err != nil {
			t.Fatalf("parse prev scope: %v", err)
		}
		currScope, err := libraryprocessing.ParseScope(curr["scope"].(string))
		if err != nil {
			t.Fatalf("parse curr scope: %v", err)
		}
		prevRank, _ := libraryprocessing.ScopeRank(prevScope)
		currRank, _ := libraryprocessing.ScopeRank(currScope)
		if prevRank < currRank {
			continue
		}
		if prevRank > currRank {
			t.Fatalf("out-of-order scope at %d: %s (rank %d) after %s (rank %d)", i, currScope, currRank, prevScope, prevRank)
		}
		prevTime, err := time.Parse(time.RFC3339Nano, prev["created_at"].(string))
		if err != nil {
			t.Fatalf("parse prev created_at: %v", err)
		}
		currTime, err := time.Parse(time.RFC3339Nano, curr["created_at"].(string))
		if err != nil {
			t.Fatalf("parse curr created_at: %v", err)
		}
		if prevTime.Before(currTime) {
			continue
		}
		if prevTime.After(currTime) {
			t.Fatalf("out-of-order created_at at %d: %s after %s", i, currTime, prevTime)
		}
		prevID := prev["id"].(string)
		currID := curr["id"].(string)
		if prevID >= currID {
			t.Fatalf("out-of-order tie-breaker id at %d: %s after %s", i, currID, prevID)
		}
	}
}

func TestLibraryProcessingRulesPagingPrecedenceTiesAndOmittedParams(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix
	lpSetModule(t, f.client, base, csrf, true)

	lpCheckEmptyRulePages(t, f, base)

	seedPrecedenceAndTieRules(t, f, base, csrf)

	fullList := lpListRules(t, f.client, base)
	// Seeded total is 13: 1 global_default + 3 media_type + 2 instance + 2 library + 2 user_language + 3 item_override.
	if len(fullList) != 13 {
		t.Fatalf("full unpaged list len = %d, want 13", len(fullList))
	}
	assertStrictPrecedenceOrder(t, fullList)

	lpCheckOmittedRulePageParams(t, f, base, fullList)

	lpCheckRulePageWalks(t, f, base, fullList)

	lpCheckRulePageBoundaries(t, f, base, fullList)

	lpCheckRuleCursorWithOmittedLimit(t, f, base, fullList)
}

func lpCheckEmptyRulePages(t *testing.T, f *routerFixture, base string) {
	t.Helper()
	// Empty state: both unpaged array and paged endpoint return empty items and final-page status.
	if emptyList := lpListRules(t, f.client, base); emptyList == nil || len(emptyList) != 0 {
		t.Fatalf("empty unpaged list = %#v, want non-nil []", emptyList)
	}
	emptyPage := lpGetRulePage(t, f.client, base, "")
	if emptyPage.Items == nil || len(emptyPage.Items) != 0 || emptyPage.HasMore || emptyPage.NextCursor != nil {
		t.Fatalf("empty page = %+v, want non-nil 0 items, has_more=false, next_cursor=nil", emptyPage)
	}
}

func lpCheckOmittedRulePageParams(t *testing.T, f *routerFixture, base string, fullList []map[string]any) {
	t.Helper()
	// Omitted limit and cursor on /page returns the complete first page (13 <= default 50) with has_more=false.
	omittedPage := lpGetRulePage(t, f.client, base, "")
	if omittedPage.HasMore || omittedPage.NextCursor != nil {
		t.Fatalf("omitted params page has_more=%v next_cursor=%v, want false/nil", omittedPage.HasMore, omittedPage.NextCursor)
	}
	if !reflect.DeepEqual(omittedPage.Items, fullList) {
		t.Fatalf("omitted params page items != unpaged full list")
	}
}

func lpCheckRulePageWalks(t *testing.T, f *routerFixture, base string, fullList []map[string]any) {
	t.Helper()
	// Multi-page walks over 13 seeded rows (13 is prime, so the only exact-fit limits are 1 and 13):
	// limit=1 (schema minimum; 13 pages, every tie boundary), limit=4 (partial last page: 4+4+4+1),
	// limit=5 (5+5+3), limit=12 (partial: 12+1), limit=13 (exact fit: 1 page, no empty trailing page),
	// limit=200 (schema maximum; 1 page).
	for _, tc := range []struct {
		limit     int
		wantPages int
	}{
		{limit: 1, wantPages: 13},
		{limit: 4, wantPages: 4},
		{limit: 5, wantPages: 3},
		{limit: 12, wantPages: 2},
		{limit: 13, wantPages: 1},
		{limit: 200, wantPages: 1},
	} {
		walked, pages := lpWalkAllRulePages(t, f.client, base, tc.limit)
		if pages != tc.wantPages {
			t.Errorf("limit=%d walked %d pages, want %d", tc.limit, pages, tc.wantPages)
		}
		if !reflect.DeepEqual(walked, fullList) {
			t.Errorf("limit=%d walked IDs = %v, want %v", tc.limit, lpRuleIDs(walked), lpRuleIDs(fullList))
		}
	}
}

func lpCheckRulePageBoundaries(t *testing.T, f *routerFixture, base string, fullList []map[string]any) {
	t.Helper()
	// Exact boundary: limit == total (13) returns every row with has_more=false and no cursor.
	exactPage := lpGetRulePage(t, f.client, base, "limit=13")
	if len(exactPage.Items) != 13 || exactPage.HasMore || exactPage.NextCursor != nil {
		t.Fatalf("limit=13 page = len %d has_more=%v next_cursor=%v, want 13/false/nil", len(exactPage.Items), exactPage.HasMore, exactPage.NextCursor)
	}
	if !reflect.DeepEqual(exactPage.Items, fullList) {
		t.Fatalf("limit=13 items != unpaged full list")
	}

	// Partial boundary: limit=12 leaves exactly one row for the cursor remainder.
	partialPage := lpGetRulePage(t, f.client, base, "limit=12")
	if len(partialPage.Items) != 12 || !partialPage.HasMore || partialPage.NextCursor == nil {
		t.Fatalf("limit=12 page = len %d has_more=%v next_cursor=%v, want 12/true/non-nil", len(partialPage.Items), partialPage.HasMore, partialPage.NextCursor)
	}
	if !reflect.DeepEqual(partialPage.Items, fullList[:12]) {
		t.Fatalf("limit=12 items != first 12 of unpaged full list")
	}
	tail := lpGetRulePage(t, f.client, base, "limit=12&cursor="+url.QueryEscape(*partialPage.NextCursor))
	if len(tail.Items) != 1 || tail.HasMore || tail.NextCursor != nil {
		t.Fatalf("limit=12 tail = len %d has_more=%v next_cursor=%v, want 1/false/nil", len(tail.Items), tail.HasMore, tail.NextCursor)
	}
	if !reflect.DeepEqual(tail.Items, fullList[12:]) {
		t.Fatalf("limit=12 tail items != last row of unpaged full list")
	}
}

func lpCheckRuleCursorWithOmittedLimit(t *testing.T, f *routerFixture, base string, fullList []map[string]any) {
	t.Helper()
	// Cursor with omitted limit uses the default limit (50) for the remainder: 13 - 2 = 11 rows.
	firstTwo := lpGetRulePage(t, f.client, base, "limit=2")
	if !firstTwo.HasMore || firstTwo.NextCursor == nil {
		t.Fatalf("firstTwo = %+v, want has_more=true with cursor", firstTwo)
	}
	if !reflect.DeepEqual(firstTwo.Items, fullList[:2]) {
		t.Fatalf("limit=2 items != first 2 of unpaged full list")
	}
	restDefaultLimit := lpGetRulePage(t, f.client, base, "cursor="+url.QueryEscape(*firstTwo.NextCursor))
	if restDefaultLimit.HasMore || restDefaultLimit.NextCursor != nil || len(restDefaultLimit.Items) != 11 {
		t.Fatalf("remainder with omitted limit = len %d has_more=%v, want 11 and false", len(restDefaultLimit.Items), restDefaultLimit.HasMore)
	}
	if !reflect.DeepEqual(restDefaultLimit.Items, fullList[2:]) {
		t.Fatalf("remainder items != unpaged full list minus first 2")
	}
}

func TestLibraryProcessingRulesPagingBoundsAndMalformedCursors(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix

	// Module off -> 404 Problem Details on the paged endpoint as well.
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules/page", "", nil), http.StatusNotFound)

	lpSetModule(t, f.client, base, csrf, true)

	lpCheckMalformedRulePageQueries(t, f, base)

	lpCheckDirectEmptyRulePage(t, f)

	lpCheckEncodedRulePageLimit(t, f, base, csrf)

	lpCheckRulePageSchemaViolations(t, f, base)

	lpCheckInvalidRulePageCursors(t, f, base)
}

func lpCheckMalformedRulePageQueries(t *testing.T, f *routerFixture, base string) {
	t.Helper()
	// Malformed raw query strings must be rejected with 400 before defaults or cursor decoding apply.
	malformedRawQueries := []string{
		"limit=%zz",
		"cursor=%zz",
		"limit=1;cursor=abc",
		"limit=%zz&cursor=not-valid-base64!!!",
		"unknown=%zz",
	}
	for _, rq := range malformedRawQueries {
		res := doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules/page?"+rq, "", nil)
		lpAssertProblem(t, res, http.StatusBadRequest)
	}
}

func lpCheckDirectEmptyRulePage(t *testing.T, f *routerFixture) {
	t.Helper()
	// Direct Go callers can omit params without an HTTP request in the context.
	directPage, err := f.h.ListLibraryProcessingRulesPage(t.Context(), oas.ListLibraryProcessingRulesPageParams{})
	if err != nil {
		t.Fatalf("direct ListLibraryProcessingRulesPage without HTTP request: %v", err)
	}
	if _, hasCursor := directPage.NextCursor.Get(); directPage.Items == nil || len(directPage.Items) != 0 || directPage.HasMore || hasCursor {
		t.Fatalf("direct page = %+v, want non-nil empty final page without cursor", directPage)
	}
}

func lpCheckEncodedRulePageLimit(t *testing.T, f *routerFixture, base, csrf string) {
	t.Helper()
	// Two rows distinguish the encoded limit of 1 from the default limit.
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("en"),
	})
	lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":      "media_type",
		"media_type": "movie",
		"rule":       lpRuleBody("en"),
	})
	encodedPage := lpGetRulePage(t, f.client, base, "limit=%31")
	if len(encodedPage.Items) != 1 || !encodedPage.HasMore || encodedPage.NextCursor == nil || *encodedPage.NextCursor == "" {
		t.Fatalf("percent-encoded limit page = %+v, want one item, has_more=true, and a cursor", encodedPage)
	}
}

func lpCheckRulePageSchemaViolations(t *testing.T, f *routerFixture, base string) {
	t.Helper()
	// Generated OpenAPI parse/schema violations -> exactly 400 Problem Details (rejected before the service runs).
	// Schema: limit integer minimum 1 maximum 200; cursor string minLength 1 maxLength 512.
	schemaInvalidQueries := []string{
		"limit=abc", // not an integer (parse error)
		"limit=0",   // below minimum 1
		"limit=-5",  // below minimum 1
		"limit=201", // above maximum 200
		"cursor=",   // below minLength 1
		"cursor=" + strings.Repeat("a", libraryprocessing.MaxRuleCursorLen+1), // above maxLength 512
	}
	for _, rq := range schemaInvalidQueries {
		res := doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules/page?"+rq, "", nil)
		lpAssertProblem(t, res, http.StatusBadRequest)
	}
}

func lpCheckInvalidRulePageCursors(t *testing.T, f *routerFixture, base string) {
	t.Helper()
	// Schema-valid but semantically invalid cursors -> exactly 422 Problem Details (service-level validation).
	b64 := func(s string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(s))
	}
	validID := "018f0000-0000-7000-8000-000000000001"
	badQueries := []string{
		"cursor=%20", // length 1 satisfies minLength; not a decodable cursor
		"cursor=not-valid-base64!!!",
		"cursor=" + url.QueryEscape(b64(`{"v":2,"s":"global_default","c":"2026-10-25T00:00:00Z","i":"`+validID+`"}`)),
		"cursor=" + url.QueryEscape(b64(`{"v":1,"s":"bogus","c":"2026-10-25T00:00:00Z","i":"`+validID+`"}`)),
		"cursor=" + url.QueryEscape(b64(`{"v":1,"s":"global_default","c":"bad-time","i":"`+validID+`"}`)),
		"cursor=" + url.QueryEscape(b64(`{"v":1,"s":"global_default","c":"2026-10-25T00:00:00Z","i":"00000000-0000-0000-0000-000000000000"}`)),
		"cursor=" + strings.Repeat("a", libraryprocessing.MaxRuleCursorLen), // exactly maxLength 512: passes schema, fails semantic decode
	}
	for _, rq := range badQueries {
		res := doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules/page?"+rq, "", nil)
		lpAssertProblem(t, res, http.StatusUnprocessableEntity)
	}
}

func TestLibraryProcessingLegacySnapshotConsistencyAndStreamFailures(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix
	lpSetModule(t, f.client, base, csrf, true)
	instID := lpCreateInstance(t, f, "lp-snapshot")

	// Seed 5 rules so batchSize=2 spans 3 database batches.
	for _, root := range []string{"/media/a", "/media/b", "/media/c", "/media/d", "/media/e"} {
		lpCreateRule(t, f.client, base, csrf, map[string]any{
			"scope":       "library",
			"instance_id": instID,
			"root_path":   root,
			"rule":        lpRuleBody("en"),
		})
		f.clk.Advance(time.Second)
	}
	baseline := lpListRules(t, f.client, base)
	if len(baseline) != 5 {
		t.Fatalf("baseline len = %d, want 5", len(baseline))
	}

	lpCheckRuleStreamSnapshot(t, f, base, csrf, instID, baseline)
	lpCheckMultiBatchRuleStream(t, f)
	lpCheckRuleStreamPreCommitFailure(t, f)
	lpCheckRuleStreamMidStreamFailure(t, f)
}

func lpCheckRuleStreamSnapshot(t *testing.T, f *routerFixture, base, csrf string, instID any, baseline []map[string]any) {
	t.Helper()
	// 1. Snapshot consistency: mutate rows on another connection after batch 1 inside StreamAll(batchSize=2).
	var streamedIDs []string
	batchesSeen := 0
	err := f.h.libraryProcessing.StreamAll(t.Context(), 2, func(batch []libraryprocessing.StoredRule) error {
		batchesSeen++
		if batchesSeen == 1 {
			// Insert both a higher-precedence item_override and a lower-precedence global_default mid-scan.
			lpCreateRule(t, f.client, base, csrf, map[string]any{
				"scope":       "item_override",
				"instance_id": instID,
				"item_id":     "concurrent-item",
				"rule":        lpRuleBody("de"),
			})
			lpCreateRule(t, f.client, base, csrf, map[string]any{
				"scope": "global_default",
				"rule":  lpRuleBody("fr"),
			})
		}
		for _, r := range batch {
			streamedIDs = append(streamedIDs, r.ID.String())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamAll: %v", err)
	}
	if batchesSeen != 3 {
		t.Fatalf("batchesSeen = %d, want 3", batchesSeen)
	}
	if !reflect.DeepEqual(streamedIDs, lpRuleIDs(baseline)) {
		t.Fatalf("snapshot stream IDs = %v, want pre-mutation snapshot %v", streamedIDs, lpRuleIDs(baseline))
	}
}

func lpCheckMultiBatchRuleStream(t *testing.T, f *routerFixture) {
	t.Helper()
	// 2. Multi-batch HTTP streaming handler with batchSize=2 produces the exact 7-item JSON array:
	// the 3 intermediate flushes inside SnapshotReadTx arm and clear the snapshot deadline before
	// commit, and the final batch after commit runs under the live request context.
	dw := newDeadlineRecordingWriter()
	streamCutoff := time.Now().Add(time.Minute)
	dw.currentDeadline = streamCutoff
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
	if invokeRulesWithNativeCancellation(f.h.serveLibraryProcessingRulesBatch(2), dw, req) {
		t.Fatal("multi-batch stream aborted")
	}
	if dw.Code != http.StatusOK {
		t.Fatalf("multi-batch stream status = %d, want 200; body: %s", dw.Code, dw.Body.String())
	}
	var multiBatchList []map[string]any
	if decErr := json.Unmarshal(dw.Body.Bytes(), &multiBatchList); decErr != nil {
		t.Fatalf("decode multi-batch JSON array: %v; body: %s", decErr, dw.Body.String())
	}
	if len(multiBatchList) != 7 {
		t.Fatalf("multi-batch list len = %d, want 7", len(multiBatchList))
	}
	assertStrictPrecedenceOrder(t, multiBatchList)
	// 7 items with batchSize=2 -> 4 chunks (3 inside the snapshot, 1 after commit); the native cutoff is never touched.
	dw.assertNativeRulesCutoff(t, streamCutoff, 4)
}

func lpCheckRuleStreamPreCommitFailure(t *testing.T, f *routerFixture) {
	t.Helper()
	// 3. Pre-commit failure (canceled context before batch 1): returns Problem JSON, never 200 or '['.
	preCtx, preCancel := context.WithCancel(t.Context())
	preCancel()
	preRec := newDeadlineRecordingWriter()
	preCutoff := time.Now().Add(time.Minute)
	preRec.currentDeadline = preCutoff
	preReq := httptest.NewRequestWithContext(preCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
	if invokeRulesWithNativeCancellation(f.h.serveLibraryProcessingRulesBatch(2), preRec, preReq) {
		t.Fatal("pre-commit failure aborted instead of returning a problem response")
	}
	if preRec.Code == http.StatusOK {
		t.Fatalf("pre-commit canceled request returned 200; body: %s", preRec.Body.String())
	}
	if ct := preRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("pre-commit error Content-Type = %q, want application/problem+json", ct)
	}
	preRec.assertNativeCutoffPreserved(t, preCutoff)
}

func lpCheckRuleStreamMidStreamFailure(t *testing.T, f *routerFixture) {
	t.Helper()
	// 4. Mid-stream failure after batch 1 committed headers: aborts with http.ErrAbortHandler,
	// clears the write deadline on supported writers, and leaves JSON array unterminated.
	midCtx, midCancel := context.WithCancel(t.Context())
	defer midCancel()
	abortWriter := &cancelOnWriteResponseWriter{
		deadlineRecordingWriter: newDeadlineRecordingWriter(),
		cancelAfterWrite:        midCancel,
	}
	midReq := httptest.NewRequestWithContext(midCtx, http.MethodGet, PathPrefix+"/library-processing/rules", nil)
	abortWriter.currentDeadline = time.Now().Add(time.Minute)
	aborted := invokeRulesWithNativeCancellation(f.h.serveLibraryProcessingRulesBatch(2), abortWriter, midReq)
	if !aborted {
		t.Fatalf("expected panic(http.ErrAbortHandler) on mid-stream failure; body: %s", abortWriter.Body.String())
	}
	// Cancellation lands while the first chunk is being written, so the
	// native write deadline is expired exactly once and never restored.
	abortWriter.assertNativeExpiredOnce(t)
	var shouldFail []any
	if json.Unmarshal(abortWriter.Body.Bytes(), &shouldFail) == nil {
		t.Fatalf("truncated mid-stream body parsed as complete JSON array: %s", abortWriter.Body.String())
	}
	if strings.Contains(abortWriter.Body.String(), "about:blank") {
		t.Fatalf("mid-stream failure appended problem+json onto partial array: %s", abortWriter.Body.String())
	}
}

type cancelOnWriteResponseWriter struct {
	*deadlineRecordingWriter
	writes           int
	cancelAfterWrite context.CancelFunc
}

func (w *cancelOnWriteResponseWriter) Write(p []byte) (int, error) {
	n, err := w.deadlineRecordingWriter.Write(p)
	w.writes++
	if w.writes == 1 && w.cancelAfterWrite != nil {
		w.cancelAfterWrite()
	}
	return n, err
}
