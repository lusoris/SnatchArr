// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// ListSchedules returns every window.
func (h *Handlers) ListSchedules(ctx context.Context) ([]oas.Schedule, error) {
	list, err := h.schedules.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.Schedule, 0, len(list))
	for _, s := range list {
		out = append(out, scheduleToOAS(s))
	}
	return out, nil
}

// CreateSchedule adds a window.
func (h *Handlers) CreateSchedule(ctx context.Context, req *oas.ScheduleInput) (*oas.Schedule, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	s, err := h.schedules.Create(ctx, scheduleFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := scheduleToOAS(s)
	return &out, nil
}

// UpdateSchedule replaces a window.
func (h *Handlers) UpdateSchedule(ctx context.Context, req *oas.ScheduleInput, params oas.UpdateScheduleParams) (*oas.Schedule, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	s, err := h.schedules.Update(ctx, params.ScheduleId, scheduleFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := scheduleToOAS(s)
	return &out, nil
}

// DeleteSchedule removes a window.
func (h *Handlers) DeleteSchedule(ctx context.Context, params oas.DeleteScheduleParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.schedules.Delete(ctx, params.ScheduleId)
}

func scheduleFromOAS(in *oas.ScheduleInput) domain.Schedule {
	s := domain.Schedule{
		Name: in.Name, Days: in.Days, Start: in.Start, End: in.End, TZ: in.Tz,
		Action: domain.ScheduleAction(in.Action), CapValue: in.CapValue.Or(0), Enabled: in.Enabled,
	}
	if v, ok := in.InstanceID.Get(); ok {
		s.InstanceID = &v
	}
	return s
}

func scheduleToOAS(s domain.Schedule) oas.Schedule {
	out := oas.Schedule{
		ID: s.ID, Name: s.Name, Days: s.Days, Start: s.Start, End: s.End, Tz: s.TZ,
		Action: oas.ScheduleAction(s.Action), Enabled: s.Enabled, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
	if s.InstanceID != nil {
		out.InstanceID = oas.NewOptUUID(*s.InstanceID)
	}
	if s.CapValue > 0 {
		out.CapValue = oas.NewOptInt(s.CapValue)
	}
	return out
}
