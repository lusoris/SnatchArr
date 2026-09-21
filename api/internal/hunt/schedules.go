// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package hunt

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Schedules owns schedule windows (CRUD). Evaluation lives in schedule.go.
type Schedules struct {
	st  *store.Store
	clk clock.Clock
	ids id.Generator
}

// NewSchedules wires the service.
func NewSchedules(st *store.Store, clk clock.Clock, ids id.Generator) *Schedules {
	return &Schedules{st: st, clk: clk, ids: ids}
}

// List returns every schedule window.
func (s *Schedules) List(ctx context.Context) ([]domain.Schedule, error) {
	rows, err := s.st.Q().ListSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("hunt: list schedules: %w", store.MapError(err))
	}
	out := make([]domain.Schedule, 0, len(rows))
	for _, row := range rows {
		out = append(out, scheduleFromRow(row))
	}
	return out, nil
}

// Create validates and stores a window.
func (s *Schedules) Create(ctx context.Context, in domain.Schedule) (domain.Schedule, error) {
	if err := in.Validate(); err != nil {
		return domain.Schedule{}, fmt.Errorf("hunt: %w", err)
	}
	if err := s.checkInstance(ctx, in.InstanceID); err != nil {
		return domain.Schedule{}, err
	}
	schedID, err := s.ids.NewUUID()
	if err != nil {
		return domain.Schedule{}, fmt.Errorf("hunt: new schedule id: %w", err)
	}
	start, end := clockOf(in.Start), clockOf(in.End)
	row, err := s.st.Q().CreateSchedule(ctx, sqlcgen.CreateScheduleParams{
		ID: schedID, InstanceID: in.InstanceID, Name: strings.TrimSpace(in.Name), DaysMask: int32(in.DaysMask()), // #nosec G115 -- 7 bits
		StartTime: start, EndTime: end, Tz: in.TZ, Action: string(in.Action), CapValue: capPtr(in),
		Enabled: in.Enabled, CreatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.Schedule{}, fmt.Errorf("hunt: create schedule: %w", store.MapError(err))
	}
	return scheduleFromRow(row), nil
}

// Update validates and replaces a window.
func (s *Schedules) Update(ctx context.Context, schedID uuid.UUID, in domain.Schedule) (domain.Schedule, error) {
	if err := in.Validate(); err != nil {
		return domain.Schedule{}, fmt.Errorf("hunt: %w", err)
	}
	if err := s.checkInstance(ctx, in.InstanceID); err != nil {
		return domain.Schedule{}, err
	}
	start, end := clockOf(in.Start), clockOf(in.End)
	row, err := s.st.Q().UpdateSchedule(ctx, sqlcgen.UpdateScheduleParams{
		ID: schedID, InstanceID: in.InstanceID, Name: strings.TrimSpace(in.Name), DaysMask: int32(in.DaysMask()), // #nosec G115 -- 7 bits
		StartTime: start, EndTime: end, Tz: in.TZ, Action: string(in.Action), CapValue: capPtr(in),
		Enabled: in.Enabled, UpdatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.Schedule{}, fmt.Errorf("hunt: update schedule: %w", store.MapError(err))
	}
	return scheduleFromRow(row), nil
}

// Delete removes a window.
func (s *Schedules) Delete(ctx context.Context, schedID uuid.UUID) error {
	n, err := s.st.Q().DeleteSchedule(ctx, schedID)
	if err != nil {
		return fmt.Errorf("hunt: delete schedule: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("hunt: delete schedule: %w", domain.ErrNotFound)
	}
	return nil
}

func (s *Schedules) checkInstance(ctx context.Context, instanceID *uuid.UUID) error {
	if instanceID == nil {
		return nil
	}
	if _, err := s.st.Q().GetInstance(ctx, *instanceID); err != nil {
		return fmt.Errorf("hunt: schedule instance: %w", store.MapError(err))
	}
	return nil
}

func capPtr(in domain.Schedule) *int32 {
	if in.Action != domain.ScheduleCapOverride {
		return nil
	}
	v := int32(in.CapValue) // #nosec G115 -- validated 1..500
	return &v
}

// clockOf converts a validated "HH:MM" into a pgtype.Time (microseconds since midnight).
func clockOf(hhmm string) pgtype.Time {
	minutes, err := domain.ParseClock(hhmm)
	if err != nil {
		minutes = 0
	}
	return pgtype.Time{Microseconds: int64(minutes) * int64(time.Minute/time.Microsecond), Valid: true}
}

func scheduleFromRow(row sqlcgen.Schedule) domain.Schedule {
	capValue := 0
	if row.CapValue != nil {
		capValue = int(*row.CapValue)
	}
	perMinute := int64(time.Minute / time.Microsecond)
	return domain.Schedule{
		ID: row.ID, InstanceID: row.InstanceID, Name: row.Name, Days: domain.DaysFromMask(int(row.DaysMask)),
		Start: domain.FormatClock(int(row.StartTime.Microseconds / perMinute)),
		End:   domain.FormatClock(int(row.EndTime.Microseconds / perMinute)),
		TZ:    row.Tz, Action: domain.ScheduleAction(row.Action), CapValue: capValue, Enabled: row.Enabled,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
