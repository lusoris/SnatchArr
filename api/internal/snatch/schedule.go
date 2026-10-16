// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"fmt"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// ScheduleAction is what a matching schedule window does.
type ScheduleAction string

// Schedule actions.
const (
	ActionPause       ScheduleAction = "pause"
	ActionCapOverride ScheduleAction = "cap_override"
)

// ScheduleWindow is one schedule rule as evaluated by the planner. Times are minutes since
// midnight in Location; DaysMask bit 0 = Sunday ... bit 6 = Saturday.
type ScheduleWindow struct {
	DaysMask int
	StartMin int
	EndMin   int
	Location *time.Location
	Action   ScheduleAction
	CapValue int
}

// Decision is the combined effect of every matching window.
type Decision struct {
	Paused      bool
	CapOverride int // 0 = none; otherwise the lowest override among matching windows
}

// Evaluate applies windows to now. Pause wins over overrides; the strictest cap wins.
func Evaluate(windows []ScheduleWindow, now time.Time) Decision {
	var d Decision
	for _, w := range windows {
		if !w.Matches(now) {
			continue
		}
		switch w.Action {
		case ActionPause:
			d.Paused = true
		case ActionCapOverride:
			if w.CapValue > 0 && (d.CapOverride == 0 || w.CapValue < d.CapOverride) {
				d.CapOverride = w.CapValue
			}
		}
	}
	return d
}

// Matches reports whether now falls inside the window, handling ranges that cross
// midnight (e.g. 22:00–06:00 matches 23:30 on an enabled day and 02:00 on the next day).
func (w ScheduleWindow) Matches(now time.Time) bool {
	loc := w.Location
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	minute := local.Hour()*60 + local.Minute()
	if w.StartMin <= w.EndMin {
		return w.dayEnabled(local.Weekday()) && minute >= w.StartMin && minute < w.EndMin
	}
	// Crossing midnight: the evening part belongs to today, the morning part to the
	// previous day's rule.
	if minute >= w.StartMin {
		return w.dayEnabled(local.Weekday())
	}
	if minute < w.EndMin {
		return w.dayEnabled((local.Weekday() + 6) % 7)
	}
	return false
}

// WindowFromSchedule converts a domain.Schedule into a ScheduleWindow for evaluation.
func WindowFromSchedule(s domain.Schedule) (ScheduleWindow, error) {
	loc, err := time.LoadLocation(s.TZ)
	if err != nil {
		return ScheduleWindow{}, fmt.Errorf("timezone %q: %w", s.TZ, err)
	}
	startMin, err := domain.ParseClock(s.Start)
	if err != nil {
		return ScheduleWindow{}, fmt.Errorf("start clock: %w", err)
	}
	endMin, err := domain.ParseClock(s.End)
	if err != nil {
		return ScheduleWindow{}, fmt.Errorf("end clock: %w", err)
	}
	return ScheduleWindow{
		DaysMask: s.DaysMask(),
		StartMin: startMin,
		EndMin:   endMin,
		Location: loc,
		Action:   ScheduleAction(s.Action),
		CapValue: s.CapValue,
	}, nil
}

// IsScheduleActive reports whether the given schedule is enabled and matches now.
func IsScheduleActive(s domain.Schedule, now time.Time) bool {
	if !s.Enabled {
		return false
	}
	w, err := WindowFromSchedule(s)
	if err != nil {
		return false
	}
	return w.Matches(now)
}

func (w ScheduleWindow) dayEnabled(day time.Weekday) bool {
	return w.DaysMask&(1<<uint(day)) != 0
}

// AllDays is the mask for every weekday.
const AllDays = 0b1111111
