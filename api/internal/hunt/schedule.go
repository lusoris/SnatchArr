// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package hunt

import (
	"time"
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
		if !w.matches(now) {
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

// matches reports whether now falls inside the window, handling ranges that cross
// midnight (e.g. 22:00–06:00 matches 23:30 on an enabled day and 02:00 on the next day).
func (w ScheduleWindow) matches(now time.Time) bool {
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

func (w ScheduleWindow) dayEnabled(day time.Weekday) bool {
	return w.DaysMask&(1<<uint(day)) != 0
}

// AllDays is the mask for every weekday.
const AllDays = 0b1111111
