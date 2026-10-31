// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/auth/session"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func newTestSession(t *testing.T) *session.Session {
	t.Helper()
	mgr, err := session.NewManager(session.NewMemoryStore(), session.Options{CookieName: "test_sid"})
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	sess, err := mgr.Load(req)
	if err != nil {
		t.Fatalf("mgr.Load: %v", err)
	}
	return sess
}

func TestSSOOriginFrom_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	groupsBlob, err := json.Marshal([]string{"devs", "admins"})
	if err != nil {
		t.Fatalf("json.Marshal groups: %v", err)
	}

	initialData := map[string]any{
		SessionUserKey:       "00000000-0000-0000-0000-000000000001",
		SessionSSOSourceKey:  "oidc",
		SessionSSOSubjectKey: "oidc-sub-123",
		SessionSSOGroupsKey:  string(groupsBlob),
		SessionSSORoleKey:    string(domain.RoleAdmin),
	}

	raw, mErr := json.Marshal(initialData)
	if mErr != nil {
		t.Fatalf("json.Marshal data: %v", mErr)
	}

	var restoredData map[string]any
	if uErr := json.Unmarshal(raw, &restoredData); uErr != nil {
		t.Fatalf("json.Unmarshal data: %v", uErr)
	}

	sess := newTestSession(t)
	for k, v := range restoredData {
		sess.Set(k, v)
	}

	got, ok, err := SSOOriginFrom(sess)
	if err != nil {
		t.Fatalf("SSOOriginFrom returned unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("SSOOriginFrom returned ok = false, want true")
	}
	if got.Source != "oidc" {
		t.Errorf("got Source = %q, want %q", got.Source, "oidc")
	}
	if got.Subject != "oidc-sub-123" {
		t.Errorf("got Subject = %q, want %q", got.Subject, "oidc-sub-123")
	}
	if !slices.Equal(got.Groups, []string{"devs", "admins"}) {
		t.Errorf("got Groups = %v, want %v", got.Groups, []string{"devs", "admins"})
	}
	if got.Role != domain.RoleAdmin {
		t.Errorf("got Role = %q, want %q", got.Role, domain.RoleAdmin)
	}
}

func TestSSOOriginFrom_Table(t *testing.T) {
	t.Parallel()
	validGroups, _ := json.Marshal([]string{"users"})
	cases := []struct {
		name       string
		setup      func(*session.Session)
		wantOK     bool
		wantErr    bool
		nilSession bool
	}{
		{
			name:       "nil session",
			nilSession: true,
			wantOK:     false,
			wantErr:    false,
		},
		{
			name:    "empty session without sso source",
			setup:   func(_ *session.Session) {},
			wantOK:  false,
			wantErr: false,
		},
		{
			name: "sso source not string",
			setup: func(s *session.Session) {
				s.Set(SessionSSOSourceKey, 123)
			},
			wantOK:  false,
			wantErr: true,
		},
		{
			name: "sso source empty string",
			setup: func(s *session.Session) {
				s.Set(SessionSSOSourceKey, "")
			},
			wantOK:  false,
			wantErr: true,
		},
		{
			name: "missing subject",
			setup: func(s *session.Session) {
				s.Set(SessionSSOSourceKey, "proxy")
			},
			wantOK:  false,
			wantErr: true,
		},
		{
			name: "missing groups",
			setup: func(s *session.Session) {
				s.Set(SessionSSOSourceKey, "proxy")
				s.Set(SessionSSOSubjectKey, "alice")
			},
			wantOK:  false,
			wantErr: true,
		},
		{
			name: "malformed groups json string",
			setup: func(s *session.Session) {
				s.Set(SessionSSOSourceKey, "proxy")
				s.Set(SessionSSOSubjectKey, "alice")
				s.Set(SessionSSOGroupsKey, "{not-a-list")
			},
			wantOK:  false,
			wantErr: true,
		},
		{
			name: "groups json not string slice",
			setup: func(s *session.Session) {
				s.Set(SessionSSOSourceKey, "proxy")
				s.Set(SessionSSOSubjectKey, "alice")
				s.Set(SessionSSOGroupsKey, "123")
			},
			wantOK:  false,
			wantErr: true,
		},
		{
			name: "missing role",
			setup: func(s *session.Session) {
				s.Set(SessionSSOSourceKey, "proxy")
				s.Set(SessionSSOSubjectKey, "alice")
				s.Set(SessionSSOGroupsKey, string(validGroups))
			},
			wantOK:  false,
			wantErr: true,
		},
		{
			name: "invalid role string",
			setup: func(s *session.Session) {
				s.Set(SessionSSOSourceKey, "proxy")
				s.Set(SessionSSOSubjectKey, "alice")
				s.Set(SessionSSOGroupsKey, string(validGroups))
				s.Set(SessionSSORoleKey, "superuser")
			},
			wantOK:  false,
			wantErr: true,
		},
		{
			name: "positive requester",
			setup: func(s *session.Session) {
				s.Set(SessionSSOSourceKey, "proxy")
				s.Set(SessionSSOSubjectKey, "charlie")
				s.Set(SessionSSOGroupsKey, string(validGroups))
				s.Set(SessionSSORoleKey, string(domain.RoleRequester))
			},
			wantOK:  true,
			wantErr: false,
		},
		{
			name: "positive viewer",
			setup: func(s *session.Session) {
				s.Set(SessionSSOSourceKey, "proxy")
				s.Set(SessionSSOSubjectKey, "bob")
				s.Set(SessionSSOGroupsKey, string(validGroups))
				s.Set(SessionSSORoleKey, string(domain.RoleViewer))
			},
			wantOK:  true,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var sess *session.Session
			if !tc.nilSession {
				sess = newTestSession(t)
				tc.setup(sess)
			}
			_, ok, err := SSOOriginFrom(sess)
			if (err != nil) != tc.wantErr {
				t.Errorf("wantErr = %v, got err = %v", tc.wantErr, err)
			}
			if ok != tc.wantOK {
				t.Errorf("wantOK = %v, got ok = %v", tc.wantOK, ok)
			}
		})
	}
}

func TestPopulateSSOSession(t *testing.T) {
	t.Parallel()
	mgr, mErr := session.NewManager(session.NewMemoryStore(), session.Options{CookieName: "test_sid"})
	if mErr != nil {
		t.Fatalf("session.NewManager: %v", mErr)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	sess, lErr := mgr.Load(req)
	if lErr != nil {
		t.Fatalf("mgr.Load: %v", lErr)
	}

	userID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	origin := SSOOrigin{
		Source:  "oidc",
		Subject: "oidc-sub-123",
		Groups:  []string{"admins"},
		Role:    domain.RoleAdmin,
	}

	svc := &Service{}
	rec := httptest.NewRecorder()
	if pErr := svc.PopulateSSOSession(t.Context(), mgr, rec, sess, userID, origin); pErr != nil {
		t.Fatalf("PopulateSSOSession: %v", pErr)
	}

	if got := sess.Get(SessionUserKey); got != userID.String() {
		t.Errorf("got user ID = %v, want %s", got, userID)
	}
	gotOrigin, ok, oErr := SSOOriginFrom(sess)
	if oErr != nil || !ok {
		t.Fatalf("SSOOriginFrom: ok=%v, err=%v", ok, oErr)
	}
	if gotOrigin.Subject != origin.Subject || gotOrigin.Role != origin.Role {
		t.Errorf("got origin = %+v, want %+v", gotOrigin, origin)
	}
}
