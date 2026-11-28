// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package libraryprocessing

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestScopeRank(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		scope   Scope
		want    int32
		wantErr bool
	}{
		{"item_override", ScopeItemOverride, 1, false},
		{"user_language", ScopeUserLanguage, 2, false},
		{"library", ScopeLibrary, 3, false},
		{"instance", ScopeInstance, 4, false},
		{"media_type", ScopeMediaType, 5, false},
		{"global_default", ScopeGlobalDefault, 6, false},
		{"zero invalid", Scope(0), 0, true},
		{"out of range invalid", Scope(99), 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ScopeRank(tc.scope)
			if tc.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("ScopeRank(%v) err = %v, want ErrInvalid", tc.scope, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ScopeRank(%v) = (%d, %v), want (%d, nil)", tc.scope, got, err, tc.want)
			}
		})
	}
}

func TestResolveRulePageLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		limit   int
		want    int
		wantErr bool
	}{
		{"omitted defaults to 50", 0, DefaultRulePageLimit, false},
		{"minimum 1", 1, 1, false},
		{"mid range", 75, 75, false},
		{"maximum 200", MaxRulePageLimit, MaxRulePageLimit, false},
		{"negative rejected", -1, 0, true},
		{"above maximum rejected", MaxRulePageLimit + 1, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveRulePageLimit(tc.limit)
			if tc.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("ResolveRulePageLimit(%d) err = %v, want ErrInvalid", tc.limit, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ResolveRulePageLimit(%d) = (%d, %v), want (%d, nil)", tc.limit, got, err, tc.want)
			}
		})
	}
}

func TestRuleCursorRoundTripAndPrecision(t *testing.T) {
	t.Parallel()
	ruleID := uuid.MustParse("018f0000-0000-7000-8000-000000000042")
	subMicroTime := time.Date(2026, 10, 25, 14, 30, 15, 123_456_789, time.FixedZone("CET", 3600))
	wantMicroTime := time.Date(2026, 10, 25, 13, 30, 15, 123_456_000, time.UTC)

	for i, scope := range Precedence() {
		t.Run(scope.String(), func(t *testing.T) {
			t.Parallel()
			encoded, err := EncodeRuleCursor(StoredRule{
				ID:        ruleID,
				Scope:     scope,
				CreatedAt: subMicroTime,
			})
			if err != nil {
				t.Fatalf("EncodeRuleCursor: %v", err)
			}
			decoded, err := DecodeRuleCursor(encoded)
			if err != nil {
				t.Fatalf("DecodeRuleCursor(%q): %v", encoded, err)
			}
			if decoded.Scope != scope {
				t.Errorf("Scope = %v, want %v", decoded.Scope, scope)
			}
			if decoded.Rank != int32(i+1) {
				t.Errorf("Rank = %d, want %d", decoded.Rank, i+1)
			}
			if !decoded.CreatedAt.Equal(wantMicroTime) {
				t.Errorf("CreatedAt = %s, want %s", decoded.CreatedAt.Format(time.RFC3339Nano), wantMicroTime.Format(time.RFC3339Nano))
			}
			if decoded.ID != ruleID {
				t.Errorf("ID = %s, want %s", decoded.ID, ruleID)
			}
		})
	}
}

func TestEncodeRuleCursorRejectsInvalidRule(t *testing.T) {
	t.Parallel()
	validID := uuid.MustParse("018f0000-0000-7000-8000-000000000001")
	validAt := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		rule StoredRule
	}{
		{"invalid scope", StoredRule{ID: validID, Scope: Scope(0), CreatedAt: validAt}},
		{"zero created_at", StoredRule{ID: validID, Scope: ScopeGlobalDefault}},
		{"nil id", StoredRule{ID: uuid.Nil, Scope: ScopeGlobalDefault, CreatedAt: validAt}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := EncodeRuleCursor(tc.rule); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("EncodeRuleCursor(%+v) err = %v, want ErrInvalid", tc.rule, err)
			}
		})
	}
}

func TestDecodeRuleCursorRejectsMalformedInputs(t *testing.T) {
	t.Parallel()
	b64 := func(s string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(s))
	}
	validID := "018f0000-0000-7000-8000-000000000001"
	validCursor, err := EncodeRuleCursor(StoredRule{
		ID:        uuid.MustParse(validID),
		Scope:     ScopeLibrary,
		CreatedAt: time.Date(2026, 10, 25, 1, 2, 3, 456_000, time.UTC),
	})
	if err != nil {
		t.Fatalf("EncodeRuleCursor setup: %v", err)
	}

	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"leading whitespace", " " + validCursor},
		{"trailing whitespace", validCursor + "\n"},
		{"oversized", strings.Repeat("a", MaxRuleCursorLen+1)},
		{"not base64", "!!!invalid-base64!!!"},
		{"padded base64", base64.URLEncoding.EncodeToString([]byte(`{"v":1,"s":"library","c":"2026-10-25T00:00:00Z","i":"` + validID + `"}`))},
		{"non json payload", b64("not-json")},
		{"json array", b64("[]")},
		{"trailing json tokens", b64(`{"v":1,"s":"library","c":"2026-10-25T00:00:00Z","i":"` + validID + `"}extra`)},
		{"unknown json field", b64(`{"v":1,"s":"library","c":"2026-10-25T00:00:00Z","i":"` + validID + `","extra":true}`)},
		{"wrong version zero", b64(`{"v":0,"s":"library","c":"2026-10-25T00:00:00Z","i":"` + validID + `"}`)},
		{"wrong version two", b64(`{"v":2,"s":"library","c":"2026-10-25T00:00:00Z","i":"` + validID + `"}`)},
		{"unknown scope", b64(`{"v":1,"s":"unknown","c":"2026-10-25T00:00:00Z","i":"` + validID + `"}`)},
		{"missing timestamp", b64(`{"v":1,"s":"library","c":"","i":"` + validID + `"}`)},
		{"malformed timestamp", b64(`{"v":1,"s":"library","c":"2026-13-99","i":"` + validID + `"}`)},
		{"zero timestamp", b64(`{"v":1,"s":"library","c":"0001-01-01T00:00:00Z","i":"` + validID + `"}`)},
		{"sub-microsecond timestamp", b64(`{"v":1,"s":"library","c":"2026-10-25T00:00:00.123456789Z","i":"` + validID + `"}`)},
		{"nil uuid", b64(`{"v":1,"s":"library","c":"2026-10-25T00:00:00Z","i":"00000000-0000-0000-0000-000000000000"}`)},
		{"uppercase uuid", b64(`{"v":1,"s":"library","c":"2026-10-25T00:00:00Z","i":"` + strings.ToUpper(validID) + `"}`)},
		{"unhyphenated uuid", b64(`{"v":1,"s":"library","c":"2026-10-25T00:00:00Z","i":"018f0000000070008000000000000001"}`)},
		{"malformed uuid", b64(`{"v":1,"s":"library","c":"2026-10-25T00:00:00Z","i":"not-a-uuid-at-all-36-characters-long"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, decErr := DecodeRuleCursor(tc.raw); !errors.Is(decErr, domain.ErrInvalid) {
				t.Fatalf("DecodeRuleCursor(%q) err = %v, want ErrInvalid", tc.raw, decErr)
			}
		})
	}
}

func TestServiceReadOperationsRespectCanceledContext(t *testing.T) {
	t.Parallel()
	svc := NewServiceWithChecker(nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := svc.ListPage(ctx, PageQuery{}); !errors.Is(err, context.Canceled) {
		t.Errorf("ListPage canceled err = %v, want context.Canceled", err)
	}
	if err := svc.StreamAll(ctx, 10, func([]StoredRule) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("StreamAll canceled err = %v, want context.Canceled", err)
	}
	if _, err := svc.List(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("List canceled err = %v, want context.Canceled", err)
	}
}
