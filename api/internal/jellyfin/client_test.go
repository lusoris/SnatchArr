// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package jellyfin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/jellyfin"
)

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
			policy:    jellyfin.Policy{MaxParentalRating: new(17), EnableAllFolders: true},
			threshold: 18,
			want:      false,
		},
		{
			name:      "boundary 18 allow at threshold 18",
			policy:    jellyfin.Policy{MaxParentalRating: new(18), EnableAllFolders: true},
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
			policy:    jellyfin.Policy{MaxParentalRating: new(0), EnableAllFolders: true},
			threshold: 18,
			want:      false,
		},
		{
			name: "negative blocked tag Adult vs restrict adult case-insensitive deny",
			policy: jellyfin.Policy{
				MaxParentalRating: new(18),
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
				MaxParentalRating: new(18),
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
				MaxParentalRating: new(18),
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
				MaxParentalRating: new(18),
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
				MaxParentalRating: new(18),
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
				MaxParentalRating: new(18),
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
	failSignIn404          bool
	failUser404            bool
}

func newFakeJellyfinServer(t *testing.T) *fakeJellyfinServer {
	t.Helper()
	s := &fakeJellyfinServer{}
	mux := http.NewServeMux()

	mux.HandleFunc("/Users/AuthenticateByName", func(w http.ResponseWriter, r *http.Request) {
		s.signInAuthHeader = r.Header.Get("Authorization")
		if s.failSignIn404 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Endpoint not found"}`))
			return
		}
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
		if s.failUser404 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"User not found"}`))
			return
		}
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

	t.Run("User 404 returns ErrNotFound", func(t *testing.T) {
		t.Parallel()
		fake := newFakeJellyfinServer(t)
		fake.failUser404 = true
		client, err := jellyfin.NewClient(fake.srv.URL, "key", 2*time.Second, "TestUA", fake.srv.Client())
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.User(context.Background(), "user-1")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for User 404, got %v", err)
		}
	})

	t.Run("SignIn 404 does not return ErrNotFound or ErrUnauthorized", func(t *testing.T) {
		t.Parallel()
		fake := newFakeJellyfinServer(t)
		fake.failSignIn404 = true
		client, err := jellyfin.NewClient(fake.srv.URL, "key", 2*time.Second, "TestUA", fake.srv.Client())
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		_, err = client.SignIn(context.Background(), "Alice", "pwd")
		if errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("SignIn 404 should not return ErrNotFound, got %v", err)
		}
		if errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("SignIn 404 should not return ErrUnauthorized, got %v", err)
		}
		if err == nil {
			t.Fatal("expected error for SignIn 404, got nil")
		}
	})
}

func TestProvideAuthenticator_Disabled(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Jellyfin.Enabled = false

	auth, err := jellyfin.ProvideAuthenticator(cfg, nil)
	if err != nil {
		t.Fatalf("ProvideAuthenticator returned error: %v", err)
	}

	ctx := t.Context()
	if _, err := auth.SignIn(ctx, "a", "b"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := auth.ByToken(ctx, "tok"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound from ByToken, got %v", err)
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

func TestProvideAuthenticator_TokenTransportPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range tokenTransportCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

			cfg := config.Default()
			cfg.Profile = tc.profile
			cfg.Jellyfin.Enabled = true
			cfg.Jellyfin.URL = tc.rawURL
			cfg.Jellyfin.APIKey = "svc-key"
			cfg.Jellyfin.Timeout = 2 * time.Second

			auth, err := jellyfin.ProvideAuthenticator(cfg, logger)
			if err != nil {
				t.Fatalf("ProvideAuthenticator() error = %v", err)
			}
			cl, ok := auth.(*jellyfin.Client)
			if !ok {
				t.Fatalf("ProvideAuthenticator() = %T, want *jellyfin.Client", auth)
			}
			if got := cl.AllowPlainHTTPToken(); got != tc.wantAllowPlain {
				t.Fatalf("AllowPlainHTTPToken() = %v, want %v", got, tc.wantAllowPlain)
			}
			logged := strings.Contains(buf.String(), "jellyfin token sign-in refused: url is not https")
			if logged != tc.wantWarn {
				t.Fatalf("warn logged = %v (buf=%q), want %v", logged, buf.String(), tc.wantWarn)
			}
		})
	}
}

type tokenTransportCase struct {
	name           string
	profile        config.Profile
	rawURL         string
	wantAllowPlain bool
	wantWarn       bool
}

func tokenTransportCases() []tokenTransportCase {
	return []tokenTransportCase{
		{"negative prod plain http logs warning and refuses plain token", config.ProfileProd, "http://jellyfin.local:8096", false, true},
		{"positive prod https logs no warning", config.ProfileProd, "https://jellyfin.example.com", false, false},
		{"positive dev plain http allows plain token without warning", config.ProfileDev, "http://jellyfin.local:8096", true, false},
	}
}

type clientByTokenCase struct {
	name                                                           string
	useTLS, allowPlainHTTP, memberAdmin, memberDisabled, nilPolicy bool
	token                                                          string
	upstreamStatus                                                 int
	wantErr                                                        error
	wantHits                                                       int32
	wantAdmin, wantDisabled                                        bool
}

func clientByTokenCases() []clientByTokenCase {
	const token = "secret-user-token-xyz987"
	return append(clientByTokenSuccessCases(token), clientByTokenFailureCases(token)...)
}

func clientByTokenSuccessCases(token string) []clientByTokenCase {
	return []clientByTokenCase{
		{"positive https valid token returns member with policy", true, false, true, false, false, token, http.StatusOK, nil, 1, true, false},
		{"positive disabled user returned with Disabled true", true, false, false, true, false, token, http.StatusOK, nil, 1, false, true},
		{"positive dev profile allows plain http base url", false, true, false, false, false, token, http.StatusOK, nil, 1, false, false},
	}
}

func clientByTokenFailureCases(token string) []clientByTokenCase {
	return []clientByTokenCase{
		{"negative prod plain http base url refused before network call", false, false, false, false, false, token, http.StatusOK, domain.ErrForbidden, 0, false, false},
		{"boundary empty token refused before network call", true, false, false, false, false, "", http.StatusOK, domain.ErrBadRequest, 0, false, false},
		{"boundary whitespace token refused before network call", true, false, false, false, false, "   \t\n ", http.StatusOK, domain.ErrBadRequest, 0, false, false},
		{"negative upstream 401 maps to ErrUnauthorized without leaking token", true, false, false, false, false, token, http.StatusUnauthorized, domain.ErrUnauthorized, 1, false, false},
		{"negative upstream 403 maps to ErrUnauthorized", true, false, false, false, false, token, http.StatusForbidden, domain.ErrUnauthorized, 1, false, false},
		{"negative upstream 404 maps to ErrUnauthorized", true, false, false, false, false, token, http.StatusNotFound, domain.ErrUnauthorized, 1, false, false},
		{"negative nil policy maps to ErrUnauthorized", true, false, false, false, true, token, http.StatusOK, domain.ErrUnauthorized, 1, false, false},
	}
}

func TestClient_ByToken(t *testing.T) {
	t.Parallel()
	const (
		serviceKey = "service-admin-api-key"
		userToken  = "secret-user-token-xyz987"
	)
	for _, tc := range clientByTokenCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var hits atomic.Int32
			var gotMeAuth, gotMePath, gotUserAuth string

			mux := http.NewServeMux()
			mux.HandleFunc("/Users/Me", func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				gotMePath = r.URL.Path
				gotMeAuth = r.Header.Get("Authorization")
				if tc.upstreamStatus != http.StatusOK {
					w.WriteHeader(tc.upstreamStatus)
					_, _ = w.Write([]byte(`{"message":"token rejected"}`))
					return
				}
				var policy any
				if !tc.nilPolicy {
					policy = map[string]any{
						"IsAdministrator":   tc.memberAdmin,
						"IsDisabled":        tc.memberDisabled,
						"MaxParentalRating": 18,
						"BlockedTags":       []string{"gore"},
						"AllowedTags":       []string{"family"},
						"EnableAllFolders":  false,
						"EnabledFolders":    []string{"lib-movies"},
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Id":     "jf-me-id-1",
					"Name":   "Eve",
					"Policy": policy,
				})
			})
			mux.HandleFunc("/Users/jf-me-id-1", func(w http.ResponseWriter, r *http.Request) {
				gotUserAuth = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Id":   "jf-me-id-1",
					"Name": "Eve",
					"Policy": map[string]any{
						"IsAdministrator": false,
						"IsDisabled":      false,
					},
				})
			})

			var srv *httptest.Server
			if tc.useTLS {
				srv = httptest.NewTLSServer(mux)
			} else {
				srv = httptest.NewServer(mux)
			}
			t.Cleanup(srv.Close)

			origClient := srv.Client()
			cl, err := jellyfin.NewClient(srv.URL, serviceKey, 2*time.Second, "TestUA", origClient)
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			cl.SetAllowPlainHTTPToken(tc.allowPlainHTTP)

			m, err := cl.ByToken(t.Context(), tc.token)
			if gotHits := hits.Load(); gotHits != tc.wantHits {
				t.Fatalf("server hits = %d, want %d (err=%v)", gotHits, tc.wantHits, err)
			}
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ByToken() err = %v, want %v", err, tc.wantErr)
				}
				if tc.token != "" && strings.TrimSpace(tc.token) != "" && strings.Contains(err.Error(), strings.TrimSpace(tc.token)) {
					t.Fatalf("ByToken() error leaked user token: %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("ByToken() unexpected error = %v", err)
			}
			if origClient.CheckRedirect != nil {
				t.Fatal("ByToken mutated shared http.Client CheckRedirect")
			}
			if gotMePath != "/Users/Me" {
				t.Fatalf("path = %q, want /Users/Me", gotMePath)
			}
			if !strings.Contains(gotMeAuth, `Token="`+userToken+`"`) {
				t.Fatalf("Authorization %q missing user token", gotMeAuth)
			}
			if strings.Contains(gotMeAuth, serviceKey) {
				t.Fatalf("Authorization %q leaked service key", gotMeAuth)
			}
			if m.ID != "jf-me-id-1" || m.Name != "Eve" || m.Admin != tc.wantAdmin || m.Disabled != tc.wantDisabled {
				t.Fatalf("Member = %+v, want ID=jf-me-id-1 Name=Eve Admin=%v Disabled=%v", m, tc.wantAdmin, tc.wantDisabled)
			}
			if m.Policy.MaxParentalRating == nil || *m.Policy.MaxParentalRating != 18 || len(m.Policy.EnabledFolders) != 1 || m.Policy.EnabledFolders[0] != "lib-movies" {
				t.Fatalf("Member.Policy = %+v, want rating 18 and EnabledFolders=[lib-movies]", m.Policy)
			}

			// Service client must remain isolated from the user token.
			if _, uErr := cl.User(t.Context(), "jf-me-id-1"); uErr != nil {
				t.Fatalf("User() after ByToken error = %v", uErr)
			}
			if !strings.Contains(gotUserAuth, `Token="`+serviceKey+`"`) || strings.Contains(gotUserAuth, userToken) {
				t.Fatalf("service User() auth header = %q, want service key only", gotUserAuth)
			}
		})
	}
}

func TestClient_ByToken_Redirects(t *testing.T) {
	t.Parallel()

	const userToken = "redirect-check-token-999"
	testTokenRedirectDowngrade(t, userToken)
	testTokenRedirectForeignHost(t, userToken)

	for _, tc := range tokenRedirectHopCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testTokenRedirectHop(t, userToken, tc)
		})
	}
}

type tokenRedirectHopCase struct {
	name     string
	hops     int
	wantErr  error
	wantName string
}

func tokenRedirectHopCases() []tokenRedirectHopCase {
	return []tokenRedirectHopCase{
		{"positive 1 same-host same-scheme redirect followed", 1, nil, "Hopper"},
		{"boundary 5 same-host redirects followed", 5, nil, "Hopper"},
		{"boundary 6 same-host redirects refused", 6, domain.ErrUnreachable, ""},
	}
}

func testTokenRedirectDowngrade(t *testing.T, token string) {
	t.Helper()
	t.Run("negative https to plain http downgrade refused before contacting target", func(t *testing.T) {
		t.Parallel()
		var hits atomic.Int32
		plainTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(plainTarget.Close)
		tlsBase := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", plainTarget.URL+"/Users/Me")
			w.WriteHeader(http.StatusTemporaryRedirect)
		}))
		t.Cleanup(tlsBase.Close)
		cl, err := jellyfin.NewClient(tlsBase.URL, "svc-key", 2*time.Second, "TestUA", tlsBase.Client())
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		if _, err = cl.ByToken(t.Context(), token); !errors.Is(err, domain.ErrUnreachable) {
			t.Fatalf("ByToken() on scheme downgrade = %v, want ErrUnreachable", err)
		}
		if got := hits.Load(); got != 0 {
			t.Fatalf("plain http redirect target hits = %d, want 0", got)
		}
	})
}

func testTokenRedirectForeignHost(t *testing.T, token string) {
	t.Helper()
	t.Run("negative foreign host redirect refused and target never sees Authorization", func(t *testing.T) {
		t.Parallel()
		var hits atomic.Int32
		foreignSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(foreignSrv.Close)
		baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", foreignSrv.URL+"/Users/Me")
			w.WriteHeader(http.StatusTemporaryRedirect)
		}))
		t.Cleanup(baseSrv.Close)
		cl, err := jellyfin.NewClient(baseSrv.URL, "svc-key", 2*time.Second, "TestUA", baseSrv.Client())
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		cl.SetAllowPlainHTTPToken(true)
		if _, err = cl.ByToken(t.Context(), token); !errors.Is(err, domain.ErrUnreachable) {
			t.Fatalf("ByToken() on foreign host redirect = %v, want ErrUnreachable", err)
		}
		if got := hits.Load(); got != 0 {
			t.Fatalf("foreign redirect target hits = %d, want 0", got)
		}
	})
}

func testTokenRedirectHop(t *testing.T, token string, tc tokenRedirectHopCase) {
	t.Helper()
	var finalAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("hop")
		step, err := strconv.Atoi(q)
		if err != nil && q != "" {
			http.Error(w, "bad hop", http.StatusBadRequest)
			return
		}
		if step < tc.hops {
			w.Header().Set("Location", "/Users/Me?hop="+strconv.Itoa(step+1))
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		finalAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"Id": "jf-hop-id", "Name": "Hopper", "Policy": map[string]any{"IsAdministrator": false, "IsDisabled": false}})
	}))
	t.Cleanup(srv.Close)
	cl, err := jellyfin.NewClient(srv.URL, "svc-key", 2*time.Second, "TestUA", srv.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	m, err := cl.ByToken(t.Context(), token)
	if tc.wantErr != nil {
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("ByToken() with %d hops = %v, want %v", tc.hops, err, tc.wantErr)
		}
		return
	}
	if err != nil || m.Name != tc.wantName || !strings.Contains(finalAuth, `Token="`+token+`"`) {
		t.Fatalf("ByToken() = (%+v, %v), want %q with token authorization", m, err, tc.wantName)
	}
}
