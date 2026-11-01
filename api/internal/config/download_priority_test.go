// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config

import "testing"

func TestDownloadPriorityInterval(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		seconds int
		valid   bool
	}{
		{"default", 60, true},
		{"minimum", 15, true},
		{"maximum", 3600, true},
		{"below minimum", 14, false},
		{"zero", 0, false},
		{"negative", -1, false},
		{"above maximum", 3601, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := (DownloadPriorityOptions{IntervalSeconds: tc.seconds}).Validate(); (err == nil) != tc.valid {
				t.Fatalf("validate = %v", err)
			}
		})
	}
}

func TestDownloadPrioritySetting(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, value string
		want        int
		invalid     bool
	}{
		{"default", "60", 60, false}, {"minimum", "15", 15, false}, {"below minimum", "14", 14, true}, {"invalid int", "oops", 60, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			options := Default()
			err := ApplySetting(&options, "snatcharr.download_priority.interval_seconds", tc.value)
			if err == nil {
				err = options.DownloadPriority.Validate()
			}
			if options.DownloadPriority.IntervalSeconds != tc.want || (err != nil) != tc.invalid {
				t.Fatalf("interval = %d, error = %v", options.DownloadPriority.IntervalSeconds, err)
			}
		})
	}
}
