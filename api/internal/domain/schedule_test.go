// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain_test

import (
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestScheduleValidate(t *testing.T) {
	t.Parallel()
	base := domain.Schedule{Name: "night", Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "22:00", End: "06:00", TZ: "Europe/Berlin", Action: domain.SchedulePause}
	tests := []struct {
		name    string
		mutate  func(*domain.Schedule)
		wantErr bool
	}{
		{"positive pause window", func(*domain.Schedule) {}, false},
		{"positive cap override", func(s *domain.Schedule) { s.Action = domain.ScheduleCapOverride; s.CapValue = 5 }, false},
		{"negative cap override without value", func(s *domain.Schedule) { s.Action = domain.ScheduleCapOverride }, true},
		{"boundary cap 500 ok", func(s *domain.Schedule) { s.Action = domain.ScheduleCapOverride; s.CapValue = 500 }, false},
		{"boundary cap 501", func(s *domain.Schedule) { s.Action = domain.ScheduleCapOverride; s.CapValue = 501 }, true},
		{"negative empty days", func(s *domain.Schedule) { s.Days = nil }, true},
		{"negative day 7", func(s *domain.Schedule) { s.Days = []int{7} }, true},
		{"negative bad time", func(s *domain.Schedule) { s.Start = "24:00" }, true},
		{"negative bad tz", func(s *domain.Schedule) { s.TZ = "Mars/Olympus" }, true},
		{"negative action", func(s *domain.Schedule) { s.Action = "sleep" }, true},
		{"boundary empty name", func(s *domain.Schedule) { s.Name = " " }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := base
			tc.mutate(&s)
			if err := s.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestDaysMaskRoundTrip(t *testing.T) {
	t.Parallel()
	s := domain.Schedule{Days: []int{1, 3, 5, 5, 9}}
	if got := s.DaysMask(); got != 0b0101010 {
		t.Fatalf("mask = %b", got)
	}
	if got := domain.DaysFromMask(0b0101010); len(got) != 3 || got[0] != 1 || got[2] != 5 {
		t.Fatalf("days = %v", got)
	}
}

func TestClock(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"00:00", "23:59", "07:05"} {
		m, err := domain.ParseClock(in)
		if err != nil || domain.FormatClock(m) != in {
			t.Fatalf("%s -> %d (%v) -> %s", in, m, err, domain.FormatClock(m))
		}
	}
	for _, in := range []string{"", "7:5", "12", "12:60", "-1:00"} {
		if _, err := domain.ParseClock(in); err == nil {
			t.Fatalf("%q should be invalid", in)
		}
	}
}
