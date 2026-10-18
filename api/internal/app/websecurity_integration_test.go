// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

// Server-side web security cases on the full control plane. The case lists follow
// s4ntrx/Cybersecurity-Threats-Guide 02-web-application-security at commit
// 34c9097f8d40f573b8ff10612813424c6711d574 (MIT): csrf/prevention/csrf_protection.py,
// security-misconfig/detection/security_headers_checker.py,
// session-hijacking/detection/session_security_checker.py and
// xss-attacks/prevention/output_encoding.py; REUSE.toml carries their MIT notice.
// Session rotation on sign-in is covered by httpapi/session_renew_integration_test.go; the
// browser-side cases live in web/e2e/security.test.ts.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const setupBody = `{"username":"admin","password":"session-test-password"}`

// TestCrossSiteRequestsAreRefused sends a state-changing request (a repeated setup, 409 once
// it passes the CSRF check) under each origin signal of csrf_protection.py.
func TestCrossSiteRequestsAreRefused(t *testing.T) {
	t.Parallel()
	router, _ := startTestControlPlane(t, nil)
	_ = setupSession(t, router)

	cases := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{"positive same-origin fetch metadata", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusConflict},
		{"positive same-origin Origin header", http.MethodPost, map[string]string{"Origin": "http://example.com"}, http.StatusConflict},
		{"positive non-browser client without origin signals", http.MethodPost, nil, http.StatusConflict},
		{"positive API key from a foreign origin", http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site", "Authorization": "Bearer sk_unknown"}, http.StatusConflict},
		{"negative cross-site fetch metadata", http.MethodPost, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"negative foreign Origin header", http.MethodPost, map[string]string{"Origin": "https://attacker.example"}, http.StatusForbidden},
		{"negative null Origin of a sandboxed frame", http.MethodPost, map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"boundary same-site sibling subdomain", http.MethodPost, map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"boundary safe method from a foreign origin", http.MethodGet, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusOK},
	}
	for _, tc := range cases {
		path := "/api/v1/auth/setup"
		if tc.method == http.MethodGet {
			path = "/api/v1/auth/setup/status"
		}
		req := httptest.NewRequest(tc.method, path, bytes.NewBufferString(setupBody))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range tc.headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: %s %s = %d %s, want %d", tc.name, tc.method, path, rec.Code, rec.Body, tc.want)
		}
		if rec.Code == http.StatusForbidden && rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: refusal Content-Type = %q, want application/problem+json", tc.name, rec.Header().Get("Content-Type"))
		}
	}
}

// TestSecurityHeaders checks the headers security_headers_checker.py grades, on an API
// error and on a public route.
func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	router, _ := startTestControlPlane(t, nil)
	want := map[string]string{
		"Strict-Transport-Security": "max-age=31536000",
		"Content-Security-Policy":   "frame-ancestors 'none'",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Permissions-Policy":        "camera=()",
	}
	for _, path := range []string{"/api/v1/auth/session", "/api/v1/auth/setup/status"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		for name, fragment := range want {
			if got := rec.Header().Get(name); !strings.Contains(got, fragment) {
				t.Errorf("GET %s: %s = %q, want it to contain %q", path, name, got, fragment)
			}
		}
		for _, leak := range []string{"Server", "X-Powered-By"} {
			if got := rec.Header().Get(leak); got != "" {
				t.Errorf("GET %s: %s = %q leaks the server version", path, leak, got)
			}
		}
		policy := rec.Header().Get("Content-Security-Policy")
		for _, directive := range []string{"object-src 'none'", "base-uri 'none'"} {
			if !strings.Contains(policy, directive) {
				t.Errorf("GET %s: Content-Security-Policy %q lacks %s", path, policy, directive)
			}
		}
	}
}

func TestSecurityHeadersHSTSFollowsSecureCookies(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		secure bool
		want   string
	}{
		{"positive HTTPS deployment pins HTTPS", true, hstsPolicy},
		{"negative plain HTTP deployment sends no HSTS", false, ""},
	}
	for _, tc := range cases {
		if got := securityHeaders(tc.secure).HSTS; got != tc.want {
			t.Errorf("%s: HSTS = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestSessionCookieAttributes checks the cookie and id rules of session_security_checker.py
// on the cookies of one setup and four sign-ins.
func TestSessionCookieAttributes(t *testing.T) {
	t.Parallel()
	router, _ := startTestControlPlane(t, nil)
	cookies := make([]*http.Cookie, 0, 5)
	cookies = append(cookies, setupSession(t, router))
	for range 4 {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(setupBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || len(rec.Result().Cookies()) != 1 {
			t.Fatalf("POST /auth/login = %d with %d cookies, want 200 with 1", rec.Code, len(rec.Result().Cookies()))
		}
		cookies = append(cookies, rec.Result().Cookies()[0])
	}
	seen := map[string]bool{}
	for _, c := range cookies {
		checkSessionCookie(t, c)
		if seen[c.Value] {
			t.Errorf("session id %q issued twice", c.Value)
		}
		seen[c.Value] = true
	}
}

// checkSessionCookie applies the per-cookie rules: flags, scope, lifetime and an id long and
// varied enough to be unguessable (at least 128 bits as base64).
func checkSessionCookie(t *testing.T, c *http.Cookie) {
	t.Helper()
	if c.Name != sessionCookie || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Errorf("cookie %s: HttpOnly=%v Secure=%v SameSite=%v Path=%q, want %s HttpOnly Secure Lax /",
			c.Name, c.HttpOnly, c.Secure, c.SameSite, c.Path, sessionCookie)
	}
	if c.Domain != "" {
		t.Errorf("cookie %s: Domain=%q widens it to sibling hosts", c.Name, c.Domain)
	}
	if c.MaxAge > 24*60*60 {
		t.Errorf("cookie %s: Max-Age=%d outlives a day", c.Name, c.MaxAge)
	}
	distinct := map[rune]bool{}
	for _, r := range c.Value {
		distinct[r] = true
	}
	if len(c.Value) < 22 || len(distinct) < 12 {
		t.Errorf("cookie %s: id %q is short or repetitive (%d characters, %d distinct)", c.Name, c.Value, len(c.Value), len(distinct))
	}
}

// TestErrorsNeverReflectMarkup sends the html and attribute contexts of output_encoding.py
// through a path parameter: the error body must stay inert problem+json.
func TestErrorsNeverReflectMarkup(t *testing.T) {
	t.Parallel()
	router, _ := startTestControlPlane(t, nil)
	cookie := setupSession(t, router)
	cases := []struct {
		name string
		path string
	}{
		{"positive plain unknown id", "/api/v1/instances/not-a-uuid"},
		{"negative script element", "/api/v1/instances/%3Cscript%3Ealert(1)%3C%2Fscript%3E"},
		{"negative attribute breakout", "/api/v1/instances/%22%3E%3Cimg%20src%3Dx%20onerror%3Dalert(1)%3E"},
		{"boundary bare angle brackets", "/api/v1/instances/%3C%3E"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code < 400 || rec.Header().Get("Content-Type") != "application/problem+json" ||
			rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: GET = %d %q nosniff=%q, want an error as problem+json with nosniff",
				tc.name, rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("X-Content-Type-Options"))
		}
		if body := rec.Body.String(); strings.ContainsAny(body, "<>") {
			t.Errorf("%s: body %s carries raw markup", tc.name, body)
		}
	}
}
