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

// LLMWriteTool constants name the MCP write tools that an administrator may opt in to.
const (
	LLMWriteToolTriggerRun           = "snatcharr_trigger_run"
	LLMWriteToolUpdatePolicy         = "snatcharr_update_policy"
	LLMWriteToolCancelRun            = "snatcharr_cancel_run"
	LLMWriteToolDeleteInstance       = "snatcharr_delete_instance"
	LLMWriteToolCreateInstance       = "snatcharr_create_instance"
	LLMWriteToolTestInstanceInput    = "snatcharr_test_instance_input"
	LLMWriteToolUpdateInstance       = "snatcharr_update_instance"
	LLMWriteToolTestInstance         = "snatcharr_test_instance"
	LLMWriteToolUpdateQueuePolicy    = "snatcharr_update_queue_policy"
	LLMWriteToolPreviewRun           = "snatcharr_preview_run"
	LLMWriteToolDeleteInstanceEvents = "snatcharr_delete_instance_events"
	LLMWriteToolResetState           = "snatcharr_reset_state"
	LLMWriteToolCreateSchedule       = "snatcharr_create_schedule"
	LLMWriteToolUpdateSchedule       = "snatcharr_update_schedule"
	LLMWriteToolDeleteSchedule       = "snatcharr_delete_schedule"
	LLMWriteToolImportConfigarr      = "snatcharr_import_configarr"
	LLMWriteToolUpdateSettings       = "snatcharr_update_settings"
)

var validLLMWriteTools = map[string]struct{}{
	LLMWriteToolTriggerRun:           {},
	LLMWriteToolUpdatePolicy:         {},
	LLMWriteToolCancelRun:            {},
	LLMWriteToolDeleteInstance:       {},
	LLMWriteToolCreateInstance:       {},
	LLMWriteToolTestInstanceInput:    {},
	LLMWriteToolUpdateInstance:       {},
	LLMWriteToolTestInstance:         {},
	LLMWriteToolUpdateQueuePolicy:    {},
	LLMWriteToolPreviewRun:           {},
	LLMWriteToolDeleteInstanceEvents: {},
	LLMWriteToolResetState:           {},
	LLMWriteToolCreateSchedule:       {},
	LLMWriteToolUpdateSchedule:       {},
	LLMWriteToolDeleteSchedule:       {},
	LLMWriteToolImportConfigarr:      {},
	LLMWriteToolUpdateSettings:       {},
}

// Settings is the singleton service configuration users edit at runtime.
type Settings struct {
	HistoryRetentionDays int
	UserAgent            string
	// GlobalHourlyCap is stamina shared by every instance (0 = off), for setups where
	// several *arr apps hit the same indexers.
	GlobalHourlyCap        int
	SearchPriorityPreset   PriorityPreset
	DownloadPriorityPreset PriorityPreset
	LLMEnabled             bool
	LLMWriteTools          []string
	UpdatedAt              time.Time
}

// ValidateLLMWriteTools checks that every write tool is known and appears at most once.
func ValidateLLMWriteTools(tools []string) ([]string, error) {
	if len(tools) == 0 {
		return []string{}, nil
	}
	seen := make(map[string]struct{}, len(tools))
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if _, ok := validLLMWriteTools[t]; !ok {
			return nil, fmt.Errorf("%w: unknown llm_write_tools value %q", ErrBadRequest, t)
		}
		if _, dup := seen[t]; dup {
			return nil, fmt.Errorf("%w: duplicate llm_write_tools value %q", ErrBadRequest, t)
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out, nil
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
	return s.validateSelections()
}

// validateSelections checks the priority presets and the opted-in LLM write tools.
func (s Settings) validateSelections() error {
	if err := s.SearchPriorityPreset.Validate(); err != nil {
		return fmt.Errorf("search_priority_preset: %w", err)
	}
	if err := s.DownloadPriorityPreset.Validate(); err != nil {
		return fmt.Errorf("download_priority_preset: %w", err)
	}
	if _, err := ValidateLLMWriteTools(s.LLMWriteTools); err != nil {
		return err
	}
	return nil
}
