// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Session fixation (#268): every sign-in and every password change issues a
// fresh session id; an id the client held before is refused afterwards.

const renewSessionCookie = "snatcharr_session"

func TestLoginRenewsSessionID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		seed bool // planted id exists server-side (anonymous pre-login session)
	}{
		{"negative - planted session id is refused after login", true},
		{"boundary - unknown session id is replaced, never adopted", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newRouterFixture(t)
			completeInitialSetup(t, f)
			planted := "planted-" + strings.Repeat("a", 32)
			if tc.seed {
				if err := f.st.Q().SaveSession(t.Context(), sqlcgen.SaveSessionParams{
					ID: planted, Data: []byte(`{}`), ExpiresAt: f.clk.Now().Add(time.Hour),
				}); err != nil {
					t.Fatalf("SaveSession: %v", err)
				}
			}
			f.jar.SetCookies(renewBaseURL(t, f), []*http.Cookie{{Name: renewSessionCookie, Value: planted, Path: "/"}})

			renewLogin(t, f)
			fresh := renewJarSession(t, f)
			if fresh == "" || fresh == planted {
				t.Fatalf("session id after login = %q, want a new id (planted %q)", fresh, planted)
			}
			renewAssertSession(t, f, planted, http.StatusUnauthorized)
			renewAssertSession(t, f, fresh, http.StatusOK)
		})
	}
}

func TestLoginWithoutCookieIssuesSession(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	completeInitialSetup(t, f)
	// boundary - no prior session at all: login still issues exactly one usable id.
	f.jar.SetCookies(renewBaseURL(t, f), []*http.Cookie{{Name: renewSessionCookie, Value: "", Path: "/", MaxAge: -1}})
	renewLogin(t, f)
	renewAssertSession(t, f, renewJarSession(t, f), http.StatusOK)
}

func TestChangePasswordRenewsSessionID(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	completeInitialSetup(t, f)
	token := renewLogin(t, f)
	before := renewJarSession(t, f)

	// boundary - a refused change keeps the session id.
	if code := renewChangePassword(t, f, token, "wrong-password-1234"); code != http.StatusUnauthorized {
		t.Fatalf("change with wrong password = %d, want 401", code)
	}
	if got := renewJarSession(t, f); got != before {
		t.Fatalf("session id after refused change = %q, want unchanged %q", got, before)
	}
	renewAssertSession(t, f, before, http.StatusOK)

	// negative - the id from before a successful change is refused afterwards.
	if code := renewChangePassword(t, f, token, "password-admin-1234"); code != http.StatusNoContent {
		t.Fatalf("change password = %d, want 204", code)
	}
	after := renewJarSession(t, f)
	if after == "" || after == before {
		t.Fatalf("session id after change = %q, want a new id (before %q)", after, before)
	}
	renewAssertSession(t, f, before, http.StatusUnauthorized)
	renewAssertSession(t, f, after, http.StatusOK)
}

func renewBaseURL(t *testing.T, f *routerFixture) *url.URL {
	t.Helper()
	u, err := url.Parse(f.srv.URL + "/")
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	return u
}

func renewJarSession(t *testing.T, f *routerFixture) string {
	t.Helper()
	for _, c := range f.jar.Cookies(renewBaseURL(t, f)) {
		if c.Name == renewSessionCookie {
			return c.Value
		}
	}
	return ""
}

// renewLogin signs the admin in through the fixture's cookie jar and returns the CSRF token.
func renewLogin(t *testing.T, f *routerFixture) string {
	t.Helper()
	body := strings.NewReader(`{"username":"admin","password":"password-admin-1234"}`)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/auth/login", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	var sess struct {
		CsrfToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return sess.CsrfToken
}

func renewChangePassword(t *testing.T, f *routerFixture, token, current string) int {
	t.Helper()
	body := strings.NewReader(`{"current_password":"` + current + `","new_password":"changed-admin-password-1234"}`)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/auth/password", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Csrf-Token", token)
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("change password: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// renewAssertSession reads /auth/session with only the given session id, outside the jar.
func renewAssertSession(t *testing.T, f *routerFixture, sessID string, want int) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+PathPrefix+"/auth/session", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: renewSessionCookie, Value: sessID})
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("GET /auth/session with id %q = %d, want %d", sessID, resp.StatusCode, want)
	}
}
