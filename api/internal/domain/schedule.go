// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ScheduleAction is what a matching schedule window does to snatching.
type ScheduleAction string

// Schedule actions.
const (
	SchedulePause       ScheduleAction = "pause"
	ScheduleCapOverride ScheduleAction = "cap_override"
)

// Schedule is a weekly time window that pauses snatching or lowers the hourly cap, for one
// instance or globally (InstanceID nil). Start and End are "HH:MM" in TZ; a window whose
// End is before Start crosses midnight.
type Schedule struct {
	ID         uuid.UUID
	InstanceID *uuid.UUID
	Name       string
	Days       []int // 0 = Sunday ... 6 = Saturday
	Start      string
	End        string
	TZ         string
	Action     ScheduleAction
	CapValue   int
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// DaysMask packs Days into the bitmask stored in the database.
func (s Schedule) DaysMask() int {
	mask := 0
	for _, d := range s.Days {
		if d >= 0 && d <= 6 {
			mask |= 1 << d
		}
	}
	return mask
}

// DaysFromMask unpacks a bitmask into sorted weekday numbers.
func DaysFromMask(mask int) []int {
	days := make([]int, 0, 7)
	for d := range 7 {
		if mask&(1<<d) != 0 {
			days = append(days, d)
		}
	}
	return days
}

// ParseClock parses "HH:MM" into minutes since midnight.
func ParseClock(s string) (int, error) {
	hh, mm, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || len(hh) != 2 || len(mm) != 2 {
		return 0, fmt.Errorf("%w: time %q must be HH:MM", ErrInvalid, s)
	}
	h, err1 := strconv.Atoi(hh)
	m, err2 := strconv.Atoi(mm)
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("%w: time %q must be HH:MM", ErrInvalid, s)
	}
	return h*60 + m, nil
}

// FormatClock renders minutes since midnight as "HH:MM".
func FormatClock(minutes int) string {
	minutes = ((minutes % 1440) + 1440) % 1440
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// Validate checks every user-supplied field.
func (s Schedule) Validate() error {
	name := strings.TrimSpace(s.Name)
	if name == "" || len(name) > 64 {
		return fmt.Errorf("%w: name must be 1-64 characters", ErrInvalid)
	}
	if err := s.validateDays(); err != nil {
		return err
	}
	if err := s.validateWindow(); err != nil {
		return err
	}
	return s.validateAction()
}

func (s Schedule) validateDays() error {
	if len(s.Days) == 0 || s.DaysMask() == 0 {
		return fmt.Errorf("%w: at least one weekday is required", ErrInvalid)
	}
	for _, d := range s.Days {
		if d < 0 || d > 6 {
			return fmt.Errorf("%w: weekday %d out of range 0-6", ErrInvalid, d)
		}
	}
	return nil
}

func (s Schedule) validateWindow() error {
	if _, err := ParseClock(s.Start); err != nil {
		return err
	}
	if _, err := ParseClock(s.End); err != nil {
		return err
	}
	if _, err := time.LoadLocation(s.TZ); err != nil {
		return fmt.Errorf("%w: unknown timezone %q", ErrInvalid, s.TZ)
	}
	return nil
}

func (s Schedule) validateAction() error {
	switch s.Action {
	case SchedulePause:
		return nil
	case ScheduleCapOverride:
		if s.CapValue < 1 || s.CapValue > 500 {
			return fmt.Errorf("%w: cap_value must be between 1 and 500 for cap_override", ErrInvalid)
		}
		return nil
	default:
		return fmt.Errorf("%w: action must be pause or cap_override", ErrInvalid)
	}
}
