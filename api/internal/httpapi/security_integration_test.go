// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/golusoris/golusoris/auth/apikey"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// streamCreds are the credentials one stream test case sends.
type streamCreds struct {
	cookies       []*http.Cookie
	authorization string
}

// streamFixture mounts the event and log stream routes over a stub stream that answers 200
// only when the middleware attached a user.
type streamFixture struct {
	router    chi.Router
	session   []*http.Cookie
	key       string
	viewerKey string
}

func newStreamFixture(t *testing.T) streamFixture {
	t.Helper()
	f := newIntegrationFixture(t)
	rec := httptest.NewRecorder()
	ctx := withHTTPTest(t.Context(), rec, httptest.NewRequest(http.MethodPost, "/auth/setup", nil))
	res, err := f.h.CompleteSetup(ctx, &oas.SetupRequest{Username: "stream-admin", Password: "correct-horse-battery-staple"})
	if err != nil {
		t.Fatalf("CompleteSetup() = %v", err)
	}
	sess, ok := res.(*oas.Session)
	if !ok {
		t.Fatalf("CompleteSetup() returned %T, want *oas.Session", res)
	}
	keys, err := apikey.New(auth.NewPgAPIKeyStore(f.st, f.clk), apikey.Options{Prefix: "sk", HMACSecret: []byte("test-secret-of-at-least-32-bytes"), Clock: f.clk})
	if err != nil {
		t.Fatalf("apikey.New() = %v", err)
	}
	raw, _, err := keys.Issue(t.Context(), sess.User.ID.String(), nil)
	if err != nil {
		t.Fatalf("Issue() = %v", err)
	}
	viewer, err := f.st.Q().CreateUser(t.Context(), sqlcgen.CreateUserParams{
		ID: uuid.New(), Username: "stream-viewer", PasswordHash: "unused", Role: string(domain.RoleViewer), CreatedAt: f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("CreateUser() = %v", err)
	}
	viewerRaw, _, err := keys.Issue(t.Context(), viewer.ID.String(), nil)
	if err != nil {
		t.Fatalf("Issue(viewer) = %v", err)
	}
	apiKeys := auth.NewAPIKeys(f.st, keys, f.clk, slog.New(slog.DiscardHandler))
	r := chi.NewRouter()
	p := MountParams{Logger: slog.New(slog.DiscardHandler), Security: NewSecurity(f.sessMgr, keys, f.h.auth, apiKeys, f.clk, slog.New(slog.DiscardHandler))}
	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFrom(r.Context()); !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	})
	MountStream(r, p, stub)
	MountLogStream(r, p, stub)
	return streamFixture{router: r, session: rec.Result().Cookies(), key: raw, viewerKey: viewerRaw}
}

func TestMountStreamRequiresUser(t *testing.T) {
	t.Parallel()
	f := newStreamFixture(t)
	cases := []struct {
		name  string
		creds func(streamFixture) streamCreds
		want  int
	}{
		{"negative no credentials", func(streamFixture) streamCreds { return streamCreds{} }, http.StatusUnauthorized},
		{"negative unknown session cookie", func(streamFixture) streamCreds {
			return streamCreds{cookies: []*http.Cookie{{Name: "snatcharr_session", Value: "not-a-session"}}}
		}, http.StatusUnauthorized},
		{"negative invalid api key", func(streamFixture) streamCreds {
			return streamCreds{authorization: "Bearer sk_not-a-key"}
		}, http.StatusUnauthorized},
		{"negative invalid api key beside a valid session", func(f streamFixture) streamCreds {
			return streamCreds{cookies: f.session, authorization: "Bearer sk_not-a-key"}
		}, http.StatusUnauthorized},
		{"boundary other scheme falls back to the cookie", func(streamFixture) streamCreds {
			return streamCreds{authorization: "Basic YWRtaW46YWRtaW4="}
		}, http.StatusUnauthorized},
		{"positive session cookie", func(f streamFixture) streamCreds { return streamCreds{cookies: f.session} }, http.StatusOK},
		{"positive api key", func(f streamFixture) streamCreds { return streamCreds{authorization: "Bearer " + f.key} }, http.StatusOK},
		{"boundary lower-case bearer scheme", func(f streamFixture) streamCreds { return streamCreds{authorization: "bearer " + f.key} }, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertStreamStatus(t, f.router, StreamPath, tc.creds(f), tc.want)
		})
	}
}

// The log tail adds the admin check to the user check of the event stream.
func TestMountLogStreamRequiresAdmin(t *testing.T) {
	t.Parallel()
	f := newStreamFixture(t)
	admin := func(f streamFixture) streamCreds { return streamCreds{authorization: "Bearer " + f.key} }
	viewer := func(f streamFixture) streamCreds { return streamCreds{authorization: "Bearer " + f.viewerKey} }
	cases := []struct {
		name  string
		path  string
		creds func(streamFixture) streamCreds
		want  int
	}{
		{"positive admin session cookie", LogStreamPath, func(f streamFixture) streamCreds { return streamCreds{cookies: f.session} }, http.StatusOK},
		{"positive admin api key", LogStreamPath, admin, http.StatusOK},
		{"negative viewer api key", LogStreamPath, viewer, http.StatusForbidden},
		{"negative no credentials", LogStreamPath, func(streamFixture) streamCreds { return streamCreds{} }, http.StatusUnauthorized},
		{"boundary viewer still reads the event stream", StreamPath, viewer, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertStreamStatus(t, f.router, tc.path, tc.creds(f), tc.want)
		})
	}
}

func assertStreamStatus(t *testing.T, h http.Handler, path string, c streamCreds, want int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	if c.authorization != "" {
		req.Header.Set("Authorization", c.authorization)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	wantType := "text/event-stream"
	if want != http.StatusOK {
		wantType = "application/problem+json"
	}
	if rec.Code != want || rec.Header().Get("Content-Type") != wantType {
		t.Fatalf("GET %s = %d %q, want %d %q (body %s)", path, rec.Code, rec.Header().Get("Content-Type"), want, wantType, rec.Body)
	}
}
