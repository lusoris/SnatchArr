// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// sessionCookie is the cookie the OpenAPI security scheme cookieAuth names.
const sessionCookie = "snatcharr_session"

// setupSession runs the setup wizard on the full control plane and returns the one cookie it
// set, whatever its name.
func setupSession(t *testing.T, router chi.Router) *http.Cookie {
	t.Helper()
	body := bytes.NewBufferString(`{"username":"admin","password":"session-test-password"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /auth/setup = %d %s, want 201", rec.Code, rec.Body)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("POST /auth/setup set %d cookies, want 1: %v", len(cookies), cookies)
	}
	return cookies[0]
}

// The session cookie the control plane sets is the one its generated security handler reads:
// the boot fix 302a8fa4 renamed a __Host- variant that never authenticated (gap audit C11-03).
// The app boots with the full Module, so a broken route mount or missing migrations fail it too.
func TestSessionCookieAuthenticates(t *testing.T) {
	t.Parallel()
	router, _ := startTestControlPlane(t, nil)
	issued := setupSession(t, router)

	cases := []struct {
		name   string
		cookie *http.Cookie
		want   int
	}{
		{"positive the issued cookie", issued, http.StatusOK},
		{"negative no cookie", nil, http.StatusUnauthorized},
		{"boundary the issued value under the __Host- name", &http.Cookie{Name: "__Host-" + sessionCookie, Value: issued.Value}, http.StatusUnauthorized},
		{"boundary the right name with an unknown value", &http.Cookie{Name: sessionCookie, Value: "not-a-session"}, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
		if tc.cookie != nil {
			req.AddCookie(tc.cookie)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: GET /auth/session = %d %s, want %d", tc.name, rec.Code, rec.Body, tc.want)
		}
	}
}
