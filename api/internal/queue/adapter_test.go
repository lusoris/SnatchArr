// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

type spyEventRecorder struct {
	err     error
	records []domain.Event
}

func (s *spyEventRecorder) Record(_ context.Context, e domain.Event) error {
	s.records = append(s.records, e)
	return s.err
}

func TestRecorderAdapterPublish(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.DiscardHandler)
	instID := uuid.New()
	runID := uuid.New()
	ts := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		recordErr   error
		frame       events.Frame
		wantRecords int
		wantEvent   domain.Event
	}{
		{
			name:      "positive/persists_and_forwards",
			recordErr: nil,
			frame: events.Frame{
				InstanceID: instID,
				Timestamp:  ts,
				Level:      "info",
				Type:       "queue_item_imported",
				EntityID:   42,
				Title:      "dl-42",
				Detail:     "imported 1 files by rule id_matched",
			},
			wantRecords: 1,
			wantEvent: domain.Event{
				RunID:      nil,
				InstanceID: instID,
				Timestamp:  ts,
				Level:      "info",
				Type:       "queue_item_imported",
				EntityID:   42,
				Title:      "dl-42",
				Detail:     "imported 1 files by rule id_matched",
			},
		},
		{
			name:      "positive/with_valid_run_id",
			recordErr: nil,
			frame: events.Frame{
				RunID:      runID.String(),
				InstanceID: instID,
				Timestamp:  ts,
				Level:      "info",
				Type:       "queue_item_struck",
				EntityID:   43,
				Title:      "dl-43",
				Detail:     "struck",
			},
			wantRecords: 1,
			wantEvent: domain.Event{
				RunID:      &runID,
				InstanceID: instID,
				Timestamp:  ts,
				Level:      "info",
				Type:       "queue_item_struck",
				EntityID:   43,
				Title:      "dl-43",
				Detail:     "struck",
			},
		},
		{
			name:      "boundary/invalid_run_id_maps_to_nil",
			recordErr: nil,
			frame: events.Frame{
				RunID:      "not-a-uuid",
				InstanceID: instID,
				Timestamp:  ts,
				Level:      "warn",
				Type:       "queue_item_import_failed",
				EntityID:   44,
				Title:      "dl-44",
				Detail:     "failed",
			},
			wantRecords: 1,
			wantEvent: domain.Event{
				RunID:      nil,
				InstanceID: instID,
				Timestamp:  ts,
				Level:      "warn",
				Type:       "queue_item_import_failed",
				EntityID:   44,
				Title:      "dl-44",
				Detail:     "failed",
			},
		},
		{
			name:      "negative/record_error_still_publishes",
			recordErr: errors.New("db down"),
			frame: events.Frame{
				InstanceID: instID,
				Timestamp:  ts,
				Level:      "warn",
				Type:       "queue_item_import_failed",
				EntityID:   45,
				Title:      "dl-45",
				Detail:     "no_clean_candidate",
			},
			wantRecords: 1,
			wantEvent: domain.Event{
				RunID:      nil,
				InstanceID: instID,
				Timestamp:  ts,
				Level:      "warn",
				Type:       "queue_item_import_failed",
				EntityID:   45,
				Title:      "dl-45",
				Detail:     "no_clean_candidate",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bus := events.New(logger)
			rec := &spyEventRecorder{err: tt.recordErr}
			adapter := queue.NewCustomRecorderAdapter(rec, bus, logger)

			adapter.Publish(t.Context(), tt.frame)

			if len(rec.records) != tt.wantRecords {
				t.Fatalf("records count = %d, want %d", len(rec.records), tt.wantRecords)
			}
			if len(rec.records) > 0 {
				got := rec.records[0]
				if !reflect.DeepEqual(got, tt.wantEvent) {
					t.Fatalf("recorded event = %+v, want %+v", got, tt.wantEvent)
				}
			}
		})
	}
}
