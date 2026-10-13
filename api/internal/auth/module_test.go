// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestNewAPIKeyServiceSecret(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		profile config.Profile
		secret  string
		want    error
		wantErr bool
	}{
		{"positive prod with a 32-byte secret", config.ProfileProd, strings.Repeat("s", 32), nil, false},
		{"positive dev falls back to the built-in secret", config.ProfileDev, "", nil, false},
		{"negative prod without a secret", config.ProfileProd, "", config.ErrMissingSecret, true},
		{"boundary prod with a 31-byte secret", config.ProfileProd, strings.Repeat("s", 31), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Options{Profile: tc.profile, APIKey: config.APIKeyOptions{Secret: tc.secret}}
			svc, err := newAPIKeyService(&PgAPIKeyStore{}, cfg, clock.NewFake(), slog.New(slog.DiscardHandler))
			if (err != nil) != tc.wantErr || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("newAPIKeyService() = %v, want error %v (any error: %v)", err, tc.want, tc.wantErr)
			}
			if !tc.wantErr && svc == nil {
				t.Error("newAPIKeyService() returned no service")
			}
		})
	}
}

func TestNewSessionManagerCookie(t *testing.T) {
	t.Parallel()
	for _, secure := range []bool{true, false} {
		cfg := config.Options{Session: config.SessionOptions{TTL: 2 * time.Hour, Secure: secure}}
		mgr, err := newSessionManager(NewPgSessionStore(storetest.New(t), clock.NewFake()), cfg)
		if err != nil {
			t.Fatalf("newSessionManager(secure=%v) = %v", secure, err)
		}
		sess, err := mgr.Load(httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil))
		if err != nil {
			t.Fatalf("Load() = %v", err)
		}
		rec := httptest.NewRecorder()
		if err := mgr.SaveContext(t.Context(), rec, sess); err != nil {
			t.Fatalf("SaveContext() = %v", err)
		}
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("SaveContext() set %d cookies, want 1", len(cookies))
		}
		c := cookies[0]
		if c.Name != cookieName || c.Secure != secure || c.MaxAge != int((2*time.Hour).Seconds()) {
			t.Errorf("cookie = %s secure=%v max-age=%d; want %s secure=%v max-age=7200", c.Name, c.Secure, c.MaxAge, cookieName, secure)
		}
	}
}
