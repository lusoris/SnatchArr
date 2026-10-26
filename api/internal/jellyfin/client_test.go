// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package jellyfin_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/jellyfin"
)

func intPtr(v int) *int {
	return &v
}

func TestAdultAllowed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		policy         jellyfin.Policy
		threshold      int
		restrictTags   []string
		adultLibraryID string
		want           bool
	}{
		{
			name:      "boundary 17 deny at threshold 18",
			policy:    jellyfin.Policy{MaxParentalRating: intPtr(17), EnableAllFolders: true},
			threshold: 18,
			want:      false,
		},
		{
			name:      "boundary 18 allow at threshold 18",
			policy:    jellyfin.Policy{MaxParentalRating: intPtr(18), EnableAllFolders: true},
			threshold: 18,
			want:      true,
		},
		{
			name:      "positive nil rating unrestricted allow",
			policy:    jellyfin.Policy{MaxParentalRating: nil, EnableAllFolders: true},
			threshold: 18,
			want:      true,
		},
		{
			name:      "boundary 0 rating deny at threshold 18",
			policy:    jellyfin.Policy{MaxParentalRating: intPtr(0), EnableAllFolders: true},
			threshold: 18,
			want:      false,
		},
		{
			name: "negative blocked tag Adult vs restrict adult case-insensitive deny",
			policy: jellyfin.Policy{
				MaxParentalRating: intPtr(18),
				BlockedTags:       []string{"  Adult "},
				EnableAllFolders:  true,
			},
			threshold:    18,
			restrictTags: []string{"adult"},
			want:         false,
		},
		{
			name: "positive blocked tag unrelated allow",
			policy: jellyfin.Policy{
				MaxParentalRating: intPtr(18),
				BlockedTags:       []string{"horror"},
				EnableAllFolders:  true,
			},
			threshold:    18,
			restrictTags: []string{"adult", "nsfw"},
			want:         true,
		},
		{
			name: "positive folder allow-list enabled folder matches",
			policy: jellyfin.Policy{
				MaxParentalRating: intPtr(18),
				EnableAllFolders:  false,
				EnabledFolders:    []string{"lib-adult", "lib-movies"},
			},
			threshold:      18,
			adultLibraryID: "lib-adult",
			want:           true,
		},
		{
			name: "negative folder allow-list enabled folder absent",
			policy: jellyfin.Policy{
				MaxParentalRating: intPtr(18),
				EnableAllFolders:  false,
				EnabledFolders:    []string{"lib-movies"},
			},
			threshold:      18,
			adultLibraryID: "lib-adult",
			want:           false,
		},
		{
			name: "positive enable all folders true ignores library list",
			policy: jellyfin.Policy{
				MaxParentalRating: intPtr(18),
				EnableAllFolders:  true,
				EnabledFolders:    nil,
			},
			threshold:      18,
			adultLibraryID: "lib-adult",
			want:           true,
		},
		{
			name: "positive empty adult library id ignores folder check",
			policy: jellyfin.Policy{
				MaxParentalRating: intPtr(18),
				EnableAllFolders:  false,
				EnabledFolders:    nil,
			},
			threshold:      18,
			adultLibraryID: "",
			want:           true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := jellyfin.AdultAllowed(tc.policy, tc.threshold, tc.restrictTags, tc.adultLibraryID)
			if got != tc.want {
				t.Fatalf("AdultAllowed() = %v, want %v", got, tc.want)
			}
		})
	}
}

type fakeJellyfinServer struct {
	srv                    *httptest.Server
	lastAuthHeader         string
	signInAuthHeader       string
	quickConnectAuthHeader string
	userAuthHeader         string
	failAuth               bool
	unknownSecret          bool
	nilPolicy              bool
	qcAuthorized           bool
}

func newFakeJellyfinServer(t *testing.T) *fakeJellyfinServer {
	t.Helper()
	s := &fakeJellyfinServer{}
	mux := http.NewServeMux()

	mux.HandleFunc("/Users/AuthenticateByName", func(w http.ResponseWriter, r *http.Request) {
		s.signInAuthHeader = r.Header.Get("Authorization")
		if s.failAuth {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Invalid username or password"}`))
			return
		}
		var policy any
		if !s.nilPolicy {
			policy = map[string]any{
				"IsAdministrator":   true,
				"IsDisabled":        false,
				"MaxParentalRating": 18,
				"EnableAllFolders":  true,
			}
		}
		resp := map[string]any{
			"AccessToken": "user-access-token-12345",
			"ServerId":    "server-uuid-1",
			"User": map[string]any{
				"Id":     "user-uuid-1",
				"Name":   "Alice",
				"Policy": policy,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/QuickConnect/Enabled", func(w http.ResponseWriter, r *http.Request) {
		s.lastAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("true"))
	})

	mux.HandleFunc("/QuickConnect/Initiate", func(w http.ResponseWriter, r *http.Request) {
		s.lastAuthHeader = r.Header.Get("Authorization")
		resp := map[string]any{
			"Code":          "123456",
			"Secret":        "qc-secret-abcdef",
			"Authenticated": false,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/QuickConnect/Connect", func(w http.ResponseWriter, r *http.Request) {
		s.lastAuthHeader = r.Header.Get("Authorization")
		if s.unknownSecret {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Secret not found"}`))
			return
		}
		resp := map[string]any{
			"Code":          "123456",
			"Secret":        r.URL.Query().Get("secret"),
			"Authenticated": s.qcAuthorized,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/Users/AuthenticateWithQuickConnect", func(w http.ResponseWriter, r *http.Request) {
		s.quickConnectAuthHeader = r.Header.Get("Authorization")
		if s.unknownSecret || s.failAuth {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Unauthorized"}`))
			return
		}
		var policy any
		if !s.nilPolicy {
			policy = map[string]any{
				"IsAdministrator":   false,
				"IsDisabled":        false,
				"MaxParentalRating": 16,
				"EnableAllFolders":  false,
				"EnabledFolders":    []string{"lib-1"},
			}
		}
		resp := map[string]any{
			"AccessToken": "qc-user-token-789",
			"ServerId":    "server-uuid-1",
			"User": map[string]any{
				"Id":     "user-uuid-2",
				"Name":   "Bob",
				"Policy": policy,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/Users/", func(w http.ResponseWriter, r *http.Request) {
		s.userAuthHeader = r.Header.Get("Authorization")
		userID := strings.TrimPrefix(r.URL.Path, "/Users/")
		var policy any
		if !s.nilPolicy {
			policy = map[string]any{
				"IsAdministrator": false,
				"IsDisabled":      false,
			}
		}
		resp := map[string]any{
			"Id":     userID,
			"Name":   "Charlie",
			"Policy": policy,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func TestClient_LifecycleAndTokenIsolation(t *testing.T) {
	t.Parallel()

	fake := newFakeJellyfinServer(t)
	const serviceKey = "service-admin-api-key"

	client, err := jellyfin.NewClient(fake.srv.URL, serviceKey, 2*time.Second, "TestUA", fake.srv.Client())
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	ctx := context.Background()

	// 1. QuickConnectEnabled should carry the service token.
	enabled, err := client.QuickConnectEnabled(ctx)
	if err != nil {
		t.Fatalf("QuickConnectEnabled failed: %v", err)
	}
	if !enabled {
		t.Fatal("expected QuickConnectEnabled = true")
	}
	if !strings.Contains(fake.lastAuthHeader, `Token="service-admin-api-key"`) {
		t.Fatalf("QuickConnectEnabled auth header %q does not contain service token", fake.lastAuthHeader)
	}

	// 2. InitiateQuickConnect
	code, secret, err := client.InitiateQuickConnect(ctx)
	if err != nil {
		t.Fatalf("InitiateQuickConnect failed: %v", err)
	}
	if code != "123456" || secret != "qc-secret-abcdef" {
		t.Fatalf("unexpected code/secret: %s / %s", code, secret)
	}

	// 3. QuickConnectAuthorized (initially false)
	authz, err := client.QuickConnectAuthorized(ctx, secret)
	if err != nil {
		t.Fatalf("QuickConnectAuthorized failed: %v", err)
	}
	if authz {
		t.Fatal("expected QuickConnectAuthorized = false")
	}

	// 4. SignIn: Must carry NO service token in Authorization header!
	member, err := client.SignIn(ctx, "Alice", "password123")
	if err != nil {
		t.Fatalf("SignIn failed: %v", err)
	}
	if member.Name != "Alice" || !member.Admin {
		t.Fatalf("unexpected member: %+v", member)
	}
	if strings.Contains(fake.signInAuthHeader, "Token=") {
		t.Fatalf("SignIn request carried token in Authorization header: %q", fake.signInAuthHeader)
	}

	// 5. Verify the service client's token was not modified by the sign-in!
	// Calling User(ctx, id) must still carry the service token, NOT the user's token.
	m, err := client.User(ctx, "user-uuid-1")
	if err != nil {
		t.Fatalf("User failed: %v", err)
	}
	if m.ID != "user-uuid-1" {
		t.Fatalf("User returned ID = %q, want user-uuid-1", m.ID)
	}
	if !strings.Contains(fake.userAuthHeader, `Token="service-admin-api-key"`) {
		t.Fatalf("User auth header after SignIn does not carry service token: %q", fake.userAuthHeader)
	}
	if strings.Contains(fake.userAuthHeader, "user-access-token-12345") {
		t.Fatalf("User auth header leaked user access token: %q", fake.userAuthHeader)
	}

	// 6. SignInQuickConnect: Also must carry NO service token
	fake.qcAuthorized = true
	authz, err = client.QuickConnectAuthorized(ctx, secret)
	if err != nil || !authz {
		t.Fatalf("QuickConnectAuthorized failed: authz=%v, err=%v", authz, err)
	}
	qcMember, err := client.SignInQuickConnect(ctx, secret)
	if err != nil {
		t.Fatalf("SignInQuickConnect failed: %v", err)
	}
	if qcMember.Name != "Bob" {
		t.Fatalf("unexpected qcMember: %+v", qcMember)
	}
	if strings.Contains(fake.quickConnectAuthHeader, "Token=") {
		t.Fatalf("SignInQuickConnect carried token in header: %q", fake.quickConnectAuthHeader)
	}
}

func TestClient_PlantedDefect_ServiceTokenLeak(t *testing.T) {
	t.Parallel()

	fake := newFakeJellyfinServer(t)
	const serviceKey = "service-admin-api-key"

	client, err := jellyfin.NewClient(fake.srv.URL, serviceKey, 2*time.Second, "TestUA", fake.srv.Client())
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	_, err = client.SignIn(context.Background(), "Alice", "password123")
	if err != nil {
		t.Fatalf("SignIn failed: %v", err)
	}

	// Planted check: if SignIn had reused the service client, fake.signInAuthHeader would contain serviceKey.
	if strings.Contains(fake.signInAuthHeader, serviceKey) {
		t.Fatalf("PLANTED DEFECT TRIGGERED: SignIn reused service client with key %q", serviceKey)
	}
}

func TestClient_ErrorMapping(t *testing.T) {
	t.Parallel()

	t.Run("401 returns ErrUnauthorized", func(t *testing.T) {
		t.Parallel()
		fake := newFakeJellyfinServer(t)
		fake.failAuth = true
		client, err := jellyfin.NewClient(fake.srv.URL, "key", 2*time.Second, "TestUA", fake.srv.Client())
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.SignIn(context.Background(), "Alice", "wrong")
		if !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("expected ErrUnauthorized, got %v", err)
		}
	})

	t.Run("unknown quick connect secret returns ErrUnauthorized", func(t *testing.T) {
		t.Parallel()
		fake := newFakeJellyfinServer(t)
		fake.unknownSecret = true
		client, err := jellyfin.NewClient(fake.srv.URL, "key", 2*time.Second, "TestUA", fake.srv.Client())
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.QuickConnectAuthorized(context.Background(), "non-existent")
		if !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("expected ErrUnauthorized for unknown secret, got %v", err)
		}
	})

	t.Run("nil policy returns ErrUnauthorized", func(t *testing.T) {
		t.Parallel()
		fake := newFakeJellyfinServer(t)
		fake.nilPolicy = true
		client, err := jellyfin.NewClient(fake.srv.URL, "key", 2*time.Second, "TestUA", fake.srv.Client())
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.SignIn(context.Background(), "Alice", "pwd")
		if !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("expected ErrUnauthorized for nil policy, got %v", err)
		}
	})
}

func TestProvideAuthenticator_Disabled(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Jellyfin.Enabled = false

	auth, err := jellyfin.ProvideAuthenticator(cfg)
	if err != nil {
		t.Fatalf("ProvideAuthenticator returned error: %v", err)
	}

	ctx := context.Background()
	if _, err := auth.SignIn(ctx, "a", "b"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := auth.QuickConnectEnabled(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, _, err := auth.InitiateQuickConnect(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := auth.QuickConnectAuthorized(ctx, "s"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := auth.SignInQuickConnect(ctx, "s"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := auth.User(ctx, "u"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
