// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"log/slog"
	"testing"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/config"
)

func TestNewPriorityReconciler(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		seconds int
		valid   bool
	}{
		{"default", 60, true}, {"minimum", 15, true}, {"maximum", 3600, true}, {"below minimum", 14, false}, {"zero", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.DownloadPriority.IntervalSeconds = tc.seconds
			got, err := NewReconciler(nil, nil, nil, cfg, clock.NewFake(), slog.New(slog.DiscardHandler), nil)
			if (err == nil) != tc.valid {
				t.Fatalf("constructor error = %v", err)
			}
			if !tc.valid {
				return
			}
			if got.Running() || got.Name() != "download_priority_observe" {
				t.Fatal("constructor started loop or changed its name")
			}
		})
	}
}
