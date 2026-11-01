// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config

import "errors"

// DownloadPriorityOptions configures the leader's read-only priority reconciler.
type DownloadPriorityOptions struct {
	IntervalSeconds int `koanf:"interval_seconds"`
}

// Validate bounds reconciliation work and duration conversion.
func (o DownloadPriorityOptions) Validate() error {
	if o.IntervalSeconds < 15 || o.IntervalSeconds > 3600 {
		return errors.New("config: snatcharr.download_priority.interval_seconds (APP_SNATCHARR_DOWNLOAD_PRIORITY_INTERVAL_SECONDS) must be between 15 and 3600")
	}
	return nil
}
