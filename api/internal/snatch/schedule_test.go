// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func at(t *testing.T, s string, loc *time.Location) time.Time {
	t.Helper()
	ts, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func TestEvaluate(t *testing.T) {
	t.Parallel()
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	nightPause := ScheduleWindow{DaysMask: AllDays, StartMin: 22 * 60, EndMin: 6 * 60, Location: berlin, Action: ActionPause}
	weekdayCap := ScheduleWindow{DaysMask: 0b0111110, StartMin: 9 * 60, EndMin: 17 * 60, Location: berlin, Action: ActionCapOverride, CapValue: 5}
	stricter := ScheduleWindow{DaysMask: AllDays, StartMin: 0, EndMin: 24 * 60, Location: berlin, Action: ActionCapOverride, CapValue: 3}

	tests := []struct {
		name    string
		windows []ScheduleWindow
		now     string
		want    Decision
	}{
		{"positive pause in evening", []ScheduleWindow{nightPause}, "2026-09-21 23:30", Decision{Paused: true}},
		{"positive pause after midnight (previous day rule)", []ScheduleWindow{nightPause}, "2026-09-22 02:00", Decision{Paused: true}},
		{"boundary end exclusive", []ScheduleWindow{nightPause}, "2026-09-22 06:00", Decision{}},
		{"boundary start inclusive", []ScheduleWindow{nightPause}, "2026-09-21 22:00", Decision{Paused: true}},
		{"negative daytime", []ScheduleWindow{nightPause}, "2026-09-21 12:00", Decision{}},
		{"positive weekday cap (Monday)", []ScheduleWindow{weekdayCap}, "2026-09-21 10:00", Decision{CapOverride: 5}},
		{"negative weekend cap (Sunday)", []ScheduleWindow{weekdayCap}, "2026-09-20 10:00", Decision{}},
		{"positive strictest cap wins", []ScheduleWindow{weekdayCap, stricter}, "2026-09-21 10:00", Decision{CapOverride: 3}},
		{"positive pause and cap combine", []ScheduleWindow{nightPause, stricter}, "2026-09-21 23:00", Decision{Paused: true, CapOverride: 3}},
		{"boundary no windows", nil, "2026-09-21 10:00", Decision{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Evaluate(tc.windows, at(t, tc.now, berlin))
			if got != tc.want {
				t.Fatalf("Evaluate = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMatchesRespectsLocation(t *testing.T) {
	t.Parallel()
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	w := ScheduleWindow{DaysMask: AllDays, StartMin: 9 * 60, EndMin: 10 * 60, Location: tokyo, Action: ActionPause}
	utc := time.Date(2026, 9, 21, 0, 30, 0, 0, time.UTC) // 09:30 in Tokyo
	if !Evaluate([]ScheduleWindow{w}, utc).Paused {
		t.Fatal("window must be evaluated in its own location")
	}
	if Evaluate([]ScheduleWindow{w}, utc.Add(2*time.Hour)).Paused {
		t.Fatal("11:30 Tokyo is outside the window")
	}
}

func TestIsScheduleActive(t *testing.T) {
	t.Parallel()
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	midnightCross := domain.Schedule{
		Name: "night", Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "22:00", End: "06:00",
		TZ: "Europe/Berlin", Action: domain.SchedulePause, Enabled: true,
	}
	disabled := midnightCross
	disabled.Enabled = false

	weekdayOnly := domain.Schedule{
		Name: "weekday", Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "17:00",
		TZ: "Europe/Berlin", Action: domain.SchedulePause, Enabled: true,
	}

	tests := []struct {
		name string
		s    domain.Schedule
		now  string
		want bool
	}{
		{"positive evening in midnight cross", midnightCross, "2026-09-21 23:30", true},
		{"positive morning in midnight cross", midnightCross, "2026-09-22 02:00", true},
		{"boundary start minute inclusive", midnightCross, "2026-09-21 22:00", true},
		{"boundary end minute exclusive", midnightCross, "2026-09-22 06:00", false},
		{"negative daytime outside midnight cross", midnightCross, "2026-09-21 12:00", false},
		{"negative disabled schedule", disabled, "2026-09-21 23:30", false},
		{"positive weekday active", weekdayOnly, "2026-09-21 10:00", true},
		{"negative weekend inactive", weekdayOnly, "2026-09-20 10:00", false},
		{"negative invalid timezone", domain.Schedule{Name: "bad", Days: []int{1}, Start: "09:00", End: "17:00", TZ: "Mars/Invalid", Enabled: true}, "2026-09-21 10:00", false},
		{"negative invalid start clock", domain.Schedule{Name: "bad", Days: []int{1}, Start: "25:00", End: "17:00", TZ: "Europe/Berlin", Enabled: true}, "2026-09-21 10:00", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsScheduleActive(tc.s, at(t, tc.now, berlin))
			if got != tc.want {
				t.Fatalf("IsScheduleActive = %v, want %v", got, tc.want)
			}
		})
	}
}
