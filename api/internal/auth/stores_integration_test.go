// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth_test

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/auth/apikey"
	"github.com/golusoris/golusoris/auth/session"
	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	gerr "github.com/golusoris/golusoris/core/errors"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

const sessionTTL = time.Hour

func isCode(err error, code gerr.Code) bool {
	var coded *gerr.Error
	return errors.As(err, &coded) && coded.Code == code
}

func TestSessionExpiresAtItsTTL(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	sessions := auth.NewPgSessionStore(storetest.New(t), clk)
	if err := sessions.Save(t.Context(), "s1", map[string]any{"user": "u1"}, sessionTTL); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	clk.Advance(sessionTTL - time.Microsecond)
	data, err := sessions.Load(t.Context(), "s1")
	if err != nil || data["user"] != "u1" {
		t.Fatalf("Load() just before expiry = %v, %v; want user u1", data, err)
	}
	clk.Advance(time.Microsecond)
	if _, err := sessions.Load(t.Context(), "s1"); !isCode(err, gerr.CodeNotFound) {
		t.Errorf("Load() at expiry = %v, want not found", err)
	}
}

func TestSessionSaveReplacesDataAndExpiry(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	sessions := auth.NewPgSessionStore(storetest.New(t), clk)
	if err := sessions.Save(t.Context(), "s1", map[string]any{"user": "u1"}, sessionTTL); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	clk.Advance(sessionTTL / 2)
	if err := sessions.Save(t.Context(), "s1", map[string]any{"user": "u2"}, sessionTTL); err != nil {
		t.Fatalf("second Save() = %v", err)
	}
	clk.Advance(sessionTTL - time.Microsecond)
	data, err := sessions.Load(t.Context(), "s1")
	if err != nil || data["user"] != "u2" {
		t.Errorf("Load() = %v, %v; want user u2 with the renewed expiry", data, err)
	}
}

func TestSessionDelete(t *testing.T) {
	t.Parallel()
	sessions := auth.NewPgSessionStore(storetest.New(t), clock.NewFake())
	if err := sessions.Save(t.Context(), "s1", map[string]any{}, sessionTTL); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	if err := sessions.Delete(t.Context(), "s1"); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if _, err := sessions.Load(t.Context(), "s1"); !isCode(err, gerr.CodeNotFound) {
		t.Errorf("Load() after Delete() = %v, want not found", err)
	}
	if err := sessions.Delete(t.Context(), "never-saved"); err != nil {
		t.Errorf("Delete() of an unknown id = %v, want nil", err)
	}
}

func TestPurgeExpiredRemovesOneBatchOfExpiredSessions(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	sessions := auth.NewPgSessionStore(storetest.New(t), clk)
	for i := range 3 {
		if err := sessions.Save(t.Context(), fmt.Sprintf("short%d", i), map[string]any{}, time.Minute); err != nil {
			t.Fatalf("Save() = %v", err)
		}
	}
	if err := sessions.Save(t.Context(), "long", map[string]any{}, sessionTTL); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	if n, err := sessions.PurgeExpired(t.Context(), 10); err != nil || n != 0 {
		t.Fatalf("PurgeExpired() before expiry = %d, %v; want 0", n, err)
	}
	clk.Advance(time.Minute)
	for _, want := range []int64{2, 1, 0} {
		if n, err := sessions.PurgeExpired(t.Context(), 2); err != nil || n != want {
			t.Fatalf("PurgeExpired(2) = %d, %v; want %d", n, err, want)
		}
	}
	if _, err := sessions.Load(t.Context(), "long"); err != nil {
		t.Errorf("Load() of the live session = %v", err)
	}
}

// TestLogoutEndsTheSession drives the golusoris manager over the Postgres store the way the
// login and logout handlers do.
func TestLogoutEndsTheSession(t *testing.T) {
	t.Parallel()
	mgr, err := session.NewManager(auth.NewPgSessionStore(storetest.New(t), clock.NewFake()),
		session.Options{CookieName: "snatcharr_session", TTL: sessionTTL})
	if err != nil {
		t.Fatalf("NewManager() = %v", err)
	}
	sess, err := mgr.Load(httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil))
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	sess.Set("user", "u1")
	login := httptest.NewRecorder()
	if err := mgr.SaveContext(t.Context(), login, sess); err != nil {
		t.Fatalf("SaveContext() = %v", err)
	}
	cookies := login.Result().Cookies()
	withCookie := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		for _, c := range cookies {
			r.AddCookie(c)
		}
		return r
	}
	if got, err := mgr.Load(withCookie()); err != nil || got.Get("user") != "u1" {
		t.Fatalf("Load() with the cookie = %v, %v; want user u1", got, err)
	}
	if err := mgr.Destroy(httptest.NewRecorder(), withCookie()); err != nil {
		t.Fatalf("Destroy() = %v", err)
	}
	if got, err := mgr.Load(withCookie()); err != nil || got.Get("user") != nil {
		t.Errorf("Load() after logout = %v, %v; want an empty session", got, err)
	}
}

// keyFixture issues keys through the golusoris service over the Postgres key store, for an
// owner that exists (api_keys.owner_id references users).
type keyFixture struct {
	keys  *apikey.Service
	store *auth.PgAPIKeyStore
	clk   interface{ Advance(time.Duration) }
	owner string
}

func newKeyFixture(t *testing.T) keyFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	owner := newOwner(t, st)
	keyStore := auth.NewPgAPIKeyStore(st, clk)
	keys, err := apikey.New(keyStore, apikey.Options{Prefix: "sk", HMACSecret: []byte("test-secret-of-at-least-32-bytes"), Clock: clk})
	if err != nil {
		t.Fatalf("apikey.New() = %v", err)
	}
	return keyFixture{keys: keys, store: keyStore, clk: clk, owner: owner}
}

func newOwner(t *testing.T, st *store.Store) string {
	t.Helper()
	svc, err := auth.NewService(st, crypto.NewPasswordHasher(1), clock.NewFake(), id.New(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewService() = %v", err)
	}
	user, err := svc.Setup(t.Context(), adminName, adminPass)
	if err != nil {
		t.Fatalf("Setup() = %v", err)
	}
	return user.ID.String()
}

func TestAPIKeyVerifiesUntilRevoked(t *testing.T) {
	t.Parallel()
	f := newKeyFixture(t)
	raw, issued, err := f.keys.Issue(t.Context(), f.owner, []string{"read"})
	if err != nil {
		t.Fatalf("Issue() = %v", err)
	}
	got, err := f.keys.Verify(t.Context(), raw)
	if err != nil || got.ID != issued.ID || got.OwnerID != f.owner || !slices.Equal(got.Scopes, []string{"read"}) {
		t.Fatalf("Verify() = %+v, %v; want the issued key", got, err)
	}
	if _, err := f.keys.Verify(t.Context(), raw+"x"); !isCode(err, gerr.CodeUnauthorized) {
		t.Errorf("Verify() of a tampered key = %v, want unauthorized", err)
	}
	if err := f.keys.Revoke(t.Context(), issued.ID); err != nil {
		t.Fatalf("Revoke() = %v", err)
	}
	if _, err := f.keys.Verify(t.Context(), raw); !isCode(err, gerr.CodeUnauthorized) {
		t.Errorf("Verify() after Revoke() = %v, want unauthorized", err)
	}
	if err := f.store.Revoke(t.Context(), issued.ID); !isCode(err, gerr.CodeNotFound) {
		t.Errorf("second Revoke() = %v, want not found", err)
	}
}

func TestAPIKeyListHoldsTheOwnersLiveKeys(t *testing.T) {
	t.Parallel()
	f := newKeyFixture(t)
	ids := make([]string, 0, 3)
	for range 3 {
		_, k, err := f.keys.Issue(t.Context(), f.owner, nil)
		if err != nil {
			t.Fatalf("Issue() = %v", err)
		}
		ids = append(ids, k.ID)
		f.clk.Advance(time.Second)
	}
	if err := f.keys.Revoke(t.Context(), ids[1]); err != nil {
		t.Fatalf("Revoke() = %v", err)
	}
	listed, err := f.keys.ListByOwner(t.Context(), f.owner)
	if err != nil {
		t.Fatalf("ListByOwner() = %v", err)
	}
	got := make([]string, 0, len(listed))
	for _, k := range listed {
		got = append(got, k.ID)
	}
	if want := []string{ids[0], ids[2]}; !slices.Equal(got, want) {
		t.Errorf("ListByOwner() = %v, want %v in creation order", got, want)
	}
	if listed, err := f.keys.ListByOwner(t.Context(), uuid.NewString()); err != nil || len(listed) != 0 {
		t.Errorf("ListByOwner() of another owner = %v, %v; want none", listed, err)
	}
}

func TestAPIKeyStoreKeepsTheExpiry(t *testing.T) {
	t.Parallel()
	f := newKeyFixture(t)
	expires := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	key := apikey.Key{ID: "sk_fixed", OwnerID: f.owner, Hash: []byte{1, 2, 3}, CreatedAt: expires.Add(-time.Hour), ExpiresAt: &expires}
	if err := f.store.Save(t.Context(), key); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	got, err := f.store.FindByID(t.Context(), key.ID)
	if err != nil || got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) || got.RevokedAt != nil {
		t.Errorf("FindByID() = %+v, %v; want expiry %s and not revoked", got, err, expires)
	}
}

func TestAPIKeyStoreRefusals(t *testing.T) {
	t.Parallel()
	f := newKeyFixture(t)
	if _, err := f.store.FindByID(t.Context(), "sk_unknown"); !isCode(err, gerr.CodeNotFound) {
		t.Errorf("FindByID() of an unknown key = %v, want not found", err)
	}
	if err := f.store.Save(t.Context(), apikey.Key{ID: "sk_x", OwnerID: "not-a-uuid"}); err == nil {
		t.Error("Save() with a malformed owner id = nil, want an error")
	}
	if err := f.store.Save(t.Context(), apikey.Key{ID: "sk_y", OwnerID: uuid.NewString(), Hash: []byte{1}}); err == nil {
		t.Error("Save() for an owner that does not exist = nil, want an error")
	}
	if _, err := f.store.ListByOwner(t.Context(), "not-a-uuid"); err == nil {
		t.Error("ListByOwner() with a malformed owner id = nil, want an error")
	}
}
