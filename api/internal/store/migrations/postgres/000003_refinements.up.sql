-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Planner refinements: recency selection, skip-recently-grabbed window, exponential
-- afterglow, global stamina shared across instances.

ALTER TABLE snatch_policies
    ADD COLUMN recent_grab_window_h INTEGER NOT NULL DEFAULT 24 CHECK (recent_grab_window_h BETWEEN 0 AND 720),
    ADD COLUMN afterglow_max_h      INTEGER NOT NULL DEFAULT 720 CHECK (afterglow_max_h BETWEEN 1 AND 8760);
ALTER TABLE snatch_policies DROP CONSTRAINT snatch_policies_selection_check;
ALTER TABLE snatch_policies ADD CONSTRAINT snatch_policies_selection_check CHECK (selection IN ('random', 'sequential', 'recent'));

-- Afterglow doubles with every fruitless snatch; expired rows stay until purged so the
-- attempt count survives the rest period.
ALTER TABLE processed_items
    ADD COLUMN attempts         INTEGER NOT NULL DEFAULT 1 CHECK (attempts >= 1),
    ADD COLUMN last_snatched_at TIMESTAMPTZ;

ALTER TABLE settings
    ADD COLUMN global_hourly_cap INTEGER NOT NULL DEFAULT 0 CHECK (global_hourly_cap BETWEEN 0 AND 5000);

CREATE TABLE global_rate_buckets (
    window_start TIMESTAMPTZ PRIMARY KEY,
    used         INTEGER NOT NULL DEFAULT 0
);
