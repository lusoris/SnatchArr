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
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
)

func postCreateAPIKey(t *testing.T, f *routerFixture, client *http.Client, csrfToken string, body any) (*http.Response, []byte) {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/auth/api-keys", bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if csrfToken != "" {
		req.Header.Set("X-Csrf-Token", csrfToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post create api key: %v", err)
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	return resp, raw
}

func deleteAPIKey(t *testing.T, f *routerFixture, client *http.Client, csrfToken, keyID string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, f.srv.URL+PathPrefix+"/auth/api-keys/"+keyID, nil)
	if err != nil {
		t.Fatalf("new delete request: %v", err)
	}
	if csrfToken != "" {
		req.Header.Set("X-Csrf-Token", csrfToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("delete api key: %v", err)
	}
	return resp
}

func listAPIKeysViaCookie(t *testing.T, f *routerFixture, client *http.Client) []oas.ApiKey {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+PathPrefix+"/auth/api-keys", nil)
	if err != nil {
		t.Fatalf("new list request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}
	var keys []oas.ApiKey
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		t.Fatalf("decode keys: %v", err)
	}
	return keys
}

func getWithBearer(t *testing.T, f *routerFixture, secret string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+PathPrefix+"/auth/session", nil)
	if err != nil {
		t.Fatalf("new bearer request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("bearer do: %v", err)
	}
	return resp
}

func setupSecondLoggedInUser(t *testing.T, f *routerFixture) (*http.Client, string) {
	t.Helper()
	username, pass := createAdminUser(t, f)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second, Jar: jar}
	loginBody := bytes.NewReader([]byte(fmt.Sprintf(`{"username":%q,"password":%q}`, username, pass)))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/auth/login", loginBody)
	if err != nil {
		t.Fatalf("new login req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("second admin login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second admin login status = %d, want 200", resp.StatusCode)
	}
	var sess oas.Session
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return client, sess.CsrfToken
}

func getAdminCsrfToken(t *testing.T, f *routerFixture) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+PathPrefix+"/auth/session", nil)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var sess oas.Session
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return sess.CsrfToken
}

func assertKeyInListNoSecret(t *testing.T, keys []oas.ApiKey, wantID, wantName string) {
	t.Helper()
	var found *oas.ApiKey
	for i := range keys {
		if keys[i].ID == wantID {
			found = &keys[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("key %s not found in list", wantID)
	}
	if found.Name != wantName {
		t.Fatalf("key name = %q, want %q", found.Name, wantName)
	}
	listJSON, _ := json.Marshal(keys)
	if strings.Contains(string(listJSON), "secret") || strings.Contains(string(listJSON), "hash") {
		t.Fatalf("list response contains secret/hash: %s", string(listJSON))
	}
}

func TestAPIKeyLifecycle(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrfToken := getAdminCsrfToken(t, f)

	resp, raw := postCreateAPIKey(t, f, f.client, csrfToken, map[string]any{"name": "test-key-1"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body: %s", resp.StatusCode, string(raw))
	}
	var created oas.CreatedApiKey
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("unmarshal created: %v", err)
	}
	if created.Secret == "" || created.ID == "" || created.Name != "test-key-1" {
		t.Fatalf("created key invalid: %+v", created)
	}

	bResp := getWithBearer(t, f, created.Secret)
	defer func() { _ = bResp.Body.Close() }()
	if bResp.StatusCode != http.StatusOK {
		t.Fatalf("bearer get status = %d, want 200", bResp.StatusCode)
	}

	keys := listAPIKeysViaCookie(t, f, f.client)
	assertKeyInListNoSecret(t, keys, created.ID, "test-key-1")

	delResp := deleteAPIKey(t, f, f.client, csrfToken, created.ID)
	_ = delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", delResp.StatusCode)
	}

	bResp2 := getWithBearer(t, f, created.Secret)
	defer func() { _ = bResp2.Body.Close() }()
	assertProblem(t, bResp2, http.StatusUnauthorized)

	delResp2 := deleteAPIKey(t, f, f.client, csrfToken, created.ID)
	defer func() { _ = delResp2.Body.Close() }()
	assertProblem(t, delResp2, http.StatusNotFound)
}

func TestAPIKeyCrossUserRevoke(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)
	secondClient, secondCsrf := setupSecondLoggedInUser(t, f)

	resp, raw := postCreateAPIKey(t, f, secondClient, secondCsrf, map[string]any{"name": "second-admin-key"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("second admin create key status = %d; body: %s", resp.StatusCode, string(raw))
	}
	var secondKey oas.CreatedApiKey
	if err := json.Unmarshal(raw, &secondKey); err != nil {
		t.Fatalf("unmarshal second key: %v", err)
	}

	delResp := deleteAPIKey(t, f, f.client, adminCsrf, secondKey.ID)
	defer func() { _ = delResp.Body.Close() }()
	assertProblem(t, delResp, http.StatusNotFound)

	bResp := getWithBearer(t, f, secondKey.Secret)
	defer func() { _ = bResp.Body.Close() }()
	if bResp.StatusCode != http.StatusOK {
		t.Fatalf("second admin key after cross-user delete status = %d, want 200", bResp.StatusCode)
	}
}

func TestAPIKeyCookieOnly(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	resp, raw := postCreateAPIKey(t, f, f.client, adminCsrf, map[string]any{"name": "bearer-key"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create key: %s", string(raw))
	}
	var created oas.CreatedApiKey
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("unmarshal created: %v", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	postReq, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/auth/api-keys", strings.NewReader(`{"name":"nested"}`))
	postReq.Header.Set("Authorization", "Bearer "+created.Secret)
	postReq.Header.Set("Content-Type", "application/json")
	pResp, err := client.Do(postReq)
	if err != nil {
		t.Fatalf("do post: %v", err)
	}
	defer func() { _ = pResp.Body.Close() }()
	assertProblem(t, pResp, http.StatusUnauthorized)

	getReq, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+PathPrefix+"/auth/api-keys", nil)
	getReq.Header.Set("Authorization", "Bearer "+created.Secret)
	gResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("do get: %v", err)
	}
	defer func() { _ = gResp.Body.Close() }()
	assertProblem(t, gResp, http.StatusUnauthorized)

	delReq, _ := http.NewRequestWithContext(t.Context(), http.MethodDelete, f.srv.URL+PathPrefix+"/auth/api-keys/"+created.ID, nil)
	delReq.Header.Set("Authorization", "Bearer "+created.Secret)
	dResp, err := client.Do(delReq)
	if err != nil {
		t.Fatalf("do del: %v", err)
	}
	defer func() { _ = dResp.Body.Close() }()
	assertProblem(t, dResp, http.StatusUnauthorized)
}

func TestAPIKeyNameValidation(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrfToken := getAdminCsrfToken(t, f)

	tests := []struct {
		name     string
		input    string
		wantCode int
	}{
		{name: "empty", input: "", wantCode: http.StatusBadRequest},
		{name: "spaces_only", input: "   ", wantCode: http.StatusBadRequest},
		{name: "too_long_101", input: strings.Repeat("a", 101), wantCode: http.StatusBadRequest},
		{name: "max_len_100", input: strings.Repeat("a", 100), wantCode: http.StatusCreated},
		{name: "umlaut_100", input: strings.Repeat("ä", 100), wantCode: http.StatusCreated},
		{name: "umlaut_101", input: strings.Repeat("ä", 101), wantCode: http.StatusBadRequest},
		{name: "trimmed_valid", input: "  trimmed-name  ", wantCode: http.StatusCreated},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, raw := postCreateAPIKey(t, f, f.client, csrfToken, map[string]any{"name": tc.input})
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, tc.wantCode, string(raw))
			}
			if tc.wantCode == http.StatusBadRequest {
				assertProblem(t, resp, http.StatusBadRequest)
			}
		})
	}
}

func TestAPIKeyExpiryValidation(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrfToken := getAdminCsrfToken(t, f)

	now := f.clk.Now()
	tests := []struct {
		name      string
		expiresAt time.Time
		wantCode  int
	}{
		{name: "in_past", expiresAt: now.Add(-1 * time.Hour), wantCode: http.StatusBadRequest},
		{name: "equal_now", expiresAt: now, wantCode: http.StatusBadRequest},
		{name: "future_boundary", expiresAt: now.Add(1 * time.Second), wantCode: http.StatusCreated},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := map[string]any{
				"name":       "expiry-" + tc.name,
				"expires_at": tc.expiresAt.Format(time.RFC3339Nano),
			}
			resp, raw := postCreateAPIKey(t, f, f.client, csrfToken, body)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, tc.wantCode, string(raw))
			}
			if tc.wantCode == http.StatusBadRequest {
				assertProblem(t, resp, http.StatusBadRequest)
			}
		})
	}
}

func TestAPIKeyQuotaAndRevocationRecovery(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrfToken := getAdminCsrfToken(t, f)

	var firstKeyID string
	for i := 1; i <= 50; i++ {
		resp, raw := postCreateAPIKey(t, f, f.client, csrfToken, map[string]any{
			"name": fmt.Sprintf("quota-key-%d", i),
		})
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create key %d failed with status %d: %s", i, resp.StatusCode, string(raw))
		}
		if i == 1 {
			var created oas.CreatedApiKey
			_ = json.Unmarshal(raw, &created)
			firstKeyID = created.ID
		}
	}

	resp51, raw51 := postCreateAPIKey(t, f, f.client, csrfToken, map[string]any{
		"name": "quota-key-51",
	})
	defer func() { _ = resp51.Body.Close() }()
	if resp51.StatusCode != http.StatusConflict {
		t.Fatalf("51st key status = %d, want 409; body: %s", resp51.StatusCode, string(raw51))
	}
	assertProblem(t, resp51, http.StatusConflict)

	delResp := deleteAPIKey(t, f, f.client, csrfToken, firstKeyID)
	_ = delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke key 1 status = %d, want 204", delResp.StatusCode)
	}

	resp51Retry, raw51Retry := postCreateAPIKey(t, f, f.client, csrfToken, map[string]any{
		"name": "quota-key-51-retry",
	})
	defer func() { _ = resp51Retry.Body.Close() }()
	if resp51Retry.StatusCode != http.StatusCreated {
		t.Fatalf("retry 51st key status = %d, want 201; body: %s", resp51Retry.StatusCode, string(raw51Retry))
	}
}

func TestAPIKeyBearerExpiration(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrfToken := getAdminCsrfToken(t, f)

	now := f.clk.Now()
	resp, raw := postCreateAPIKey(t, f, f.client, csrfToken, map[string]any{
		"name":       "short-lived-key",
		"expires_at": now.Add(10 * time.Second).Format(time.RFC3339Nano),
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create key status = %d: %s", resp.StatusCode, string(raw))
	}
	var created oas.CreatedApiKey
	_ = json.Unmarshal(raw, &created)

	bResp1 := getWithBearer(t, f, created.Secret)
	defer func() { _ = bResp1.Body.Close() }()
	if bResp1.StatusCode != http.StatusOK {
		t.Fatalf("bearer get status = %d, want 200", bResp1.StatusCode)
	}

	f.clk.Advance(15 * time.Second)

	bResp2 := getWithBearer(t, f, created.Secret)
	defer func() { _ = bResp2.Body.Close() }()
	assertProblem(t, bResp2, http.StatusUnauthorized)
}

func TestAPIKeyLastUsedAtTouch(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrfToken := getAdminCsrfToken(t, f)

	resp, raw := postCreateAPIKey(t, f, f.client, csrfToken, map[string]any{"name": "touch-key"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create key: %s", string(raw))
	}
	var created oas.CreatedApiKey
	_ = json.Unmarshal(raw, &created)

	keysBefore := listAPIKeysViaCookie(t, f, f.client)
	for _, k := range keysBefore {
		if k.ID == created.ID {
			if _, ok := k.LastUsedAt.Get(); ok {
				t.Fatalf("key has last_used_at set before any bearer call: %v", k.LastUsedAt)
			}
		}
	}

	f.clk.Advance(5 * time.Minute)
	touchTime := f.clk.Now()
	bResp := getWithBearer(t, f, created.Secret)
	_ = bResp.Body.Close()
	if bResp.StatusCode != http.StatusOK {
		t.Fatalf("bearer get status = %d, want 200", bResp.StatusCode)
	}

	keysAfter := listAPIKeysViaCookie(t, f, f.client)
	var foundAfter *oas.ApiKey
	for i := range keysAfter {
		if keysAfter[i].ID == created.ID {
			foundAfter = &keysAfter[i]
			break
		}
	}
	if foundAfter == nil {
		t.Fatalf("key %s not found in list", created.ID)
	}
	lu, ok := foundAfter.LastUsedAt.Get()
	if !ok {
		t.Fatal("last_used_at not set after bearer call")
	}
	if !lu.Truncate(time.Second).Equal(touchTime.Truncate(time.Second)) {
		t.Fatalf("last_used_at = %v, want %v", lu, touchTime)
	}
}
