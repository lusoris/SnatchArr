// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"fmt"
	"strings"
	"time"
)

// GlobalHourlyCapLimit bounds the shared stamina.
const GlobalHourlyCapLimit = 5000

// Settings is the singleton service configuration users edit at runtime.
type Settings struct {
	HistoryRetentionDays int
	UserAgent            string
	// GlobalHourlyCap is stamina shared by every instance (0 = off), for setups where
	// several *arr apps hit the same indexers.
	GlobalHourlyCap int
	UpdatedAt       time.Time
}

// Validate enforces the ranges the UI advertises.
func (s Settings) Validate() error {
	if s.HistoryRetentionDays < 1 || s.HistoryRetentionDays > 3650 {
		return fmt.Errorf("%w: history_retention_days must be between 1 and 3650", ErrInvalid)
	}
	ua := strings.TrimSpace(s.UserAgent)
	if ua == "" || len(ua) > 200 || strings.ContainsAny(ua, "\r\n") {
		return fmt.Errorf("%w: user_agent must be 1-200 characters on one line", ErrInvalid)
	}
	if s.GlobalHourlyCap < 0 || s.GlobalHourlyCap > GlobalHourlyCapLimit {
		return fmt.Errorf("%w: global_hourly_cap must be between 0 and %d", ErrInvalid, GlobalHourlyCapLimit)
	}
	return nil
}
