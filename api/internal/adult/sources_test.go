// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func TestValidateSourceInput_PriorityBounds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		priority int
		wantErr  bool
	}{
		{name: "boundary min 0 accepted", priority: 0, wantErr: false},
		{name: "boundary max 1000 accepted", priority: 1000, wantErr: false},
		{name: "negative -1 rejected", priority: -1, wantErr: true},
		{name: "negative 1001 rejected", priority: 1001, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateSourceInput(SourceInput{
				Name:     "Source Bound Check",
				Endpoint: "http://127.0.0.1:1/graphql",
				APIKey:   "fake-api-key-0123456789",
				Priority: tc.priority,
			}, true)
			if tc.wantErr {
				if !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("priority %d: expected domain.ErrInvalid, got %v", tc.priority, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("priority %d: unexpected error: %v", tc.priority, err)
			}
		})
	}
}

func TestAggregateSyncStatus_AndTruncateError(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	errOld := "older failure"
	errRecent := "most recent failure"

	rows := []sqlcgen.AdultStashboxInstance{
		{
			ID:                   uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			Name:                 "Source A",
			Endpoint:             "http://127.0.0.1:1/graphql",
			Enabled:              true,
			LastSyncAt:           &t1,
			LastSyncError:        &errOld,
			LastPerformersSynced: 2,
			LastStudiosSynced:    1,
		},
		{
			ID:                   uuid.MustParse("00000000-0000-0000-0000-000000000002"),
			Name:                 "Source B",
			Endpoint:             "http://127.0.0.1:2/graphql",
			Enabled:              true,
			LastSyncAt:           &t2,
			LastSyncError:        &errRecent,
			LastPerformersSynced: 3,
			LastStudiosSynced:    4,
		},
		{
			ID:                   uuid.MustParse("00000000-0000-0000-0000-000000000003"),
			Name:                 "Source C",
			Endpoint:             "http://127.0.0.1:3/graphql",
			Enabled:              false,
			LastSyncAt:           &t3,
			LastSyncError:        nil,
			LastPerformersSynced: 5,
			LastStudiosSynced:    6,
		},
	}

	st := aggregateSyncStatus(rows)
	if st.TotalSources != 3 || st.EnabledSources != 2 {
		t.Fatalf("counts = (%d, %d), want (3, 2)", st.TotalSources, st.EnabledSources)
	}
	if st.PerformersSynced != 10 || st.StudiosSynced != 11 {
		t.Fatalf("sums = (%d, %d), want (10, 11)", st.PerformersSynced, st.StudiosSynced)
	}
	if st.LastSyncAt == nil || !st.LastSyncAt.Equal(t3) {
		t.Fatalf("LastSyncAt = %v, want %v", st.LastSyncAt, t3)
	}
	if st.LastError != errRecent {
		t.Fatalf("LastError = %q, want %q", st.LastError, errRecent)
	}

	srcB := toSource(rows[1])
	if srcB.LastSyncAt == nil || !srcB.LastSyncAt.Equal(t2) || srcB.LastSyncError != errRecent {
		t.Fatalf("toSource sync fields = (%v, %q), want (%v, %q)", srcB.LastSyncAt, srcB.LastSyncError, t2, errRecent)
	}

	longErr := strings.Repeat("e", 600)
	if got := truncateSyncError(longErr); len(got) != MaxSyncErrorLen {
		t.Fatalf("truncateSyncError len = %d, want %d", len(got), MaxSyncErrorLen)
	}
	// Boundary: a two-byte character straddling the limit is dropped, not split.
	splitErr := strings.Repeat("e", MaxSyncErrorLen-1) + "ü"
	if got := truncateSyncError(splitErr); len(got) != MaxSyncErrorLen-1 || !utf8.ValidString(got) {
		t.Fatalf("truncateSyncError split len = %d valid = %v, want %d valid", len(got), utf8.ValidString(got), MaxSyncErrorLen-1)
	}
	if got := truncateSyncError("short"); got != "short" {
		t.Fatalf("truncateSyncError(short) = %q", got)
	}
}

func TestSyncNow_SingleFlight_AndLeaderSkip(t *testing.T) {
	t.Parallel()
	svc := &Service{logger: slog.New(slog.DiscardHandler)}
	svc.syncing.Store(true)
	defer svc.syncing.Store(false)

	_, err := svc.SyncNow(context.Background())
	if !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("expected ErrSyncInProgress on concurrent SyncNow, got %v", err)
	}

	if err := svc.SyncAll(context.Background()); err != nil {
		t.Fatalf("expected SyncAll to skip silently with nil error when busy, got %v", err)
	}
}
