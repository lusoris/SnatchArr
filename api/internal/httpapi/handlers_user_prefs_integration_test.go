// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestUserPreferencesDirectHandler(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)

	userA := domain.User{ID: uuid.New(), Username: "alice-pref", Role: domain.RoleViewer}
	userB := domain.User{ID: uuid.New(), Username: "bob-pref", Role: domain.RoleRequester}
	seedTestUser(t, f, userA)
	seedTestUser(t, f, userB)

	if _, err := f.h.GetMyPreferences(t.Context()); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("GetMyPreferences unauthenticated err = %v, want ErrUnauthorized", err)
	}
	if _, err := f.h.UpdateMyPreferences(t.Context(), &oas.UserPreferences{}); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("UpdateMyPreferences unauthenticated err = %v, want ErrUnauthorized", err)
	}

	ctxA := withUser(t.Context(), userA)
	ctxB := withUser(t.Context(), userB)

	empty, err := f.h.GetMyPreferences(ctxA)
	if err != nil {
		t.Fatalf("GetMyPreferences empty: %v", err)
	}
	if !empty.BrandMark.IsNull() || !empty.SfwMode.IsNull() {
		t.Fatalf("expected both null preferences, got %+v", empty)
	}

	reqSet := &oas.UserPreferences{
		BrandMark: oas.NewOptNilBrandMark(oas.BrandMarkHand),
		SfwMode:   oas.NewOptNilSfwMode(oas.SfwModeOff),
	}
	setRes, err := f.h.UpdateMyPreferences(ctxA, reqSet)
	if err != nil {
		t.Fatalf("UpdateMyPreferences set: %v", err)
	}
	if bm, ok := setRes.BrandMark.Get(); !ok || bm != oas.BrandMarkHand {
		t.Fatalf("set BrandMark = %v, want hand", bm)
	}
	if sm, ok := setRes.SfwMode.Get(); !ok || sm != oas.SfwModeOff {
		t.Fatalf("set SfwMode = %v, want off", sm)
	}

	bPref, err := f.h.GetMyPreferences(ctxB)
	if err != nil {
		t.Fatalf("GetMyPreferences user B: %v", err)
	}
	if !bPref.BrandMark.IsNull() || !bPref.SfwMode.IsNull() {
		t.Fatalf("user B preferences contaminated: %+v", bPref)
	}

	var reqClear oas.UserPreferences
	reqClear.BrandMark.SetToNull()
	reqClear.SfwMode.SetToNull()
	clearRes, err := f.h.UpdateMyPreferences(ctxA, &reqClear)
	if err != nil {
		t.Fatalf("UpdateMyPreferences clear: %v", err)
	}
	if !clearRes.BrandMark.IsNull() || !clearRes.SfwMode.IsNull() {
		t.Fatalf("expected cleared null preferences, got %+v", clearRes)
	}
}

func TestUserPreferencesHTTPEndpoints(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	anonClient := &http.Client{Timeout: 10 * time.Second}

	testAnonPrefEndpoint(t, f, anonClient, http.MethodGet, nil)
	testAnonPrefEndpoint(t, f, anonClient, http.MethodPut, []byte(`{"brand_mark":"hand"}`))

	_ = completeInitialSetup(t, f)

	testAuthedPrefGet(t, f, nil, nil)

	hand := "hand"
	off := "off"
	testAuthedPrefPut(t, f, `{"brand_mark":"hand","sfw_mode":"off"}`, http.StatusOK, &hand, &off)
	testAuthedPrefGet(t, f, &hand, &off)

	// PUT invalid enum -> 422 Unprocessable Entity Problem (ogen validation error).
	testAuthedPrefPut(t, f, `{"brand_mark":"invalid_mark"}`, http.StatusUnprocessableEntity, nil, nil)
	testAuthedPrefPut(t, f, `{"sfw_mode":"invalid_sfw"}`, http.StatusUnprocessableEntity, nil, nil)

	// Malformed JSON body -> 400 Bad Request Problem.
	testAuthedPrefPut(t, f, `{"brand_mark":`, http.StatusBadRequest, nil, nil)

	testAuthedPrefPut(t, f, `{"brand_mark":null,"sfw_mode":null}`, http.StatusOK, nil, nil)
	testAuthedPrefGet(t, f, nil, nil)
}

func testAnonPrefEndpoint(t *testing.T, f *routerFixture, client *http.Client, method string, body []byte) {
	t.Helper()
	var r io.Reader
	if len(body) > 0 {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, f.srv.URL+PathPrefix+"/me/preferences", r)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertProblem(t, resp, http.StatusUnauthorized)
}

func testAuthedPrefGet(t *testing.T, f *routerFixture, wantBM, wantSfw *string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+PathPrefix+"/me/preferences", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /me/preferences status = %d, want 200", resp.StatusCode)
	}
	var res map[string]*string
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode preferences: %v", err)
	}
	checkPrefString(t, "brand_mark", res["brand_mark"], wantBM)
	checkPrefString(t, "sfw_mode", res["sfw_mode"], wantSfw)
}

func checkPrefString(t *testing.T, field string, got, want *string) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil && want != nil:
		t.Fatalf("%s got null, want %q", field, *want)
	case got != nil && want == nil:
		t.Fatalf("%s got %q, want null", field, *got)
	case *got != *want:
		t.Fatalf("%s got %q, want %q", field, *got, *want)
	}
}

func testAuthedPrefPut(t *testing.T, f *routerFixture, bodyStr string, wantStatus int, wantBM, wantSfw *string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, f.srv.URL+PathPrefix+"/me/preferences", strings.NewReader(bodyStr))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if wantStatus != http.StatusOK {
		assertProblem(t, resp, wantStatus)
		return
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /me/preferences status = %d, want 200", resp.StatusCode)
	}
	var res map[string]*string
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode preferences: %v", err)
	}
	checkPrefString(t, "brand_mark", res["brand_mark"], wantBM)
	checkPrefString(t, "sfw_mode", res["sfw_mode"], wantSfw)
}
