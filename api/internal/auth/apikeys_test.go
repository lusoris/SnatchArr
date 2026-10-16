// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/auth/apikey"
	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

type fakeKeyIssuer struct {
	issuedRaw  string
	issuedKey  apikey.Key
	issueErr   error
	revokedIDs []string
	revokeErr  error
}

func (f *fakeKeyIssuer) Issue(_ context.Context, _ string, _ []string) (string, apikey.Key, error) {
	if f.issueErr != nil {
		return "", apikey.Key{}, f.issueErr
	}
	return f.issuedRaw, f.issuedKey, nil
}

func (f *fakeKeyIssuer) Revoke(_ context.Context, id string) error {
	f.revokedIDs = append(f.revokedIDs, id)
	return f.revokeErr
}

type stubQuerier struct {
	sqlcgen.Querier
	countActive int64
	countErr    error
	setMetaRows int64
	setMetaErr  error
	listRows    []sqlcgen.ListAPIKeysForOwnerRow
	listErr     error
	revokeRows  int64
	revokeErr   error
	touchErr    error
}

func (s *stubQuerier) CountActiveAPIKeysByOwner(_ context.Context, _ uuid.UUID) (int64, error) {
	return s.countActive, s.countErr
}

func (s *stubQuerier) SetAPIKeyMeta(_ context.Context, _ sqlcgen.SetAPIKeyMetaParams) (int64, error) {
	return s.setMetaRows, s.setMetaErr
}

func (s *stubQuerier) ListAPIKeysForOwner(_ context.Context, _ uuid.UUID) ([]sqlcgen.ListAPIKeysForOwnerRow, error) {
	return s.listRows, s.listErr
}

func (s *stubQuerier) RevokeAPIKeyForOwner(_ context.Context, _ sqlcgen.RevokeAPIKeyForOwnerParams) (int64, error) {
	return s.revokeRows, s.revokeErr
}

func (s *stubQuerier) TouchAPIKey(_ context.Context, _ sqlcgen.TouchAPIKeyParams) error {
	return s.touchErr
}

func TestAPIKeysCreateValidation(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	now := clk.Now()
	owner := uuid.New()
	past := now.Add(-1 * time.Minute)
	exactNow := now
	future := now.Add(1 * time.Hour)
	oneSecondFuture := now.Add(1 * time.Second)

	cases := []struct {
		name      string
		keyName   string
		expiresAt *time.Time
		count     int64
		wantErr   error
	}{
		{"negative empty name", "", nil, 0, domain.ErrBadRequest},
		{"negative blank whitespace name", "    ", nil, 0, domain.ErrBadRequest},
		{"negative 101 char name", strings.Repeat("a", 101), nil, 0, domain.ErrBadRequest},
		{"boundary 1 char name", "a", nil, 0, nil},
		{"boundary 100 char name", strings.Repeat("a", 100), nil, 0, nil},
		{"boundary 100 umlaut chars name", strings.Repeat("ä", 100), nil, 0, nil},
		{"negative 101 umlaut chars name", strings.Repeat("ä", 101), nil, 0, domain.ErrBadRequest},
		{"positive trimmed name", "  valid name  ", nil, 0, nil},
		{"negative expires in past", "valid", &past, 0, domain.ErrBadRequest},
		{"negative expires equal now", "valid", &exactNow, 0, domain.ErrBadRequest},
		{"boundary expires 1s future", "valid", &oneSecondFuture, 0, nil},
		{"positive expires future", "valid", &future, 0, nil},
		{"boundary 49 live keys allowed", "valid", nil, 49, nil},
		{"negative 50 live keys quota exceeded", "valid", nil, 50, domain.ErrConflict},
		{"negative 55 live keys quota exceeded", "valid", nil, 55, domain.ErrConflict},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issuer := &fakeKeyIssuer{
				issuedRaw: "sk_test_raw_key",
				issuedKey: apikey.Key{ID: "sk_test_id", CreatedAt: now},
			}
			q := &stubQuerier{countActive: tc.count, setMetaRows: 1}
			svc := newAPIKeysWithQuerier(q, issuer, clk, slog.New(slog.DiscardHandler))

			created, err := svc.Create(t.Context(), owner, tc.keyName, tc.expiresAt)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Create() err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create() unexpected err: %v", err)
			}
			if created.Secret != "sk_test_raw_key" || created.ID != "sk_test_id" {
				t.Fatalf("unexpected created key: %+v", created)
			}
			if created.Name != strings.TrimSpace(tc.keyName) {
				t.Fatalf("created name %q != trimmed %q", created.Name, strings.TrimSpace(tc.keyName))
			}
		})
	}
}

func TestAPIKeysCreateRollbackOnMetaFailure(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	now := clk.Now()
	owner := uuid.New()

	cases := []struct {
		name        string
		setMetaRows int64
		setMetaErr  error
	}{
		{"negative db error during set meta", 0, errors.New("db connection lost")},
		{"boundary zero rows updated during set meta", 0, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issuer := &fakeKeyIssuer{
				issuedRaw: "sk_rollback_raw",
				issuedKey: apikey.Key{ID: "sk_rollback_id", CreatedAt: now},
			}
			q := &stubQuerier{countActive: 0, setMetaRows: tc.setMetaRows, setMetaErr: tc.setMetaErr}
			svc := newAPIKeysWithQuerier(q, issuer, clk, slog.New(slog.DiscardHandler))

			_, err := svc.Create(t.Context(), owner, "my-key", nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if len(issuer.revokedIDs) != 1 || issuer.revokedIDs[0] != "sk_rollback_id" {
				t.Fatalf("expected key to be revoked on metadata failure, got revokedIDs: %v", issuer.revokedIDs)
			}
		})
	}
}

func TestAPIKeysListAndRevoke(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	now := clk.Now()
	owner := uuid.New()

	t.Run("positive list returns active keys", func(t *testing.T) {
		t.Parallel()
		q := &stubQuerier{
			listRows: []sqlcgen.ListAPIKeysForOwnerRow{
				{ID: "k1", OwnerID: owner, Name: "key 1", CreatedAt: now},
				{ID: "k2", OwnerID: owner, Name: "key 2", CreatedAt: now.Add(time.Minute)},
			},
		}
		svc := newAPIKeysWithQuerier(q, &fakeKeyIssuer{}, clk, slog.New(slog.DiscardHandler))
		keys, err := svc.List(t.Context(), owner)
		if err != nil {
			t.Fatalf("List() err: %v", err)
		}
		if len(keys) != 2 || keys[0].ID != "k1" || keys[1].ID != "k2" {
			t.Fatalf("unexpected list result: %+v", keys)
		}
	})

	t.Run("positive revoke existing key", func(t *testing.T) {
		t.Parallel()
		q := &stubQuerier{revokeRows: 1}
		svc := newAPIKeysWithQuerier(q, &fakeKeyIssuer{}, clk, slog.New(slog.DiscardHandler))
		if err := svc.Revoke(t.Context(), owner, "k1"); err != nil {
			t.Fatalf("Revoke() err: %v", err)
		}
	})

	t.Run("negative revoke unknown or already revoked key", func(t *testing.T) {
		t.Parallel()
		q := &stubQuerier{revokeRows: 0}
		svc := newAPIKeysWithQuerier(q, &fakeKeyIssuer{}, clk, slog.New(slog.DiscardHandler))
		err := svc.Revoke(t.Context(), owner, "k1")
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("Revoke() err = %v, want ErrNotFound", err)
		}
	})
}
