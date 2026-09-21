-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

DROP TABLE IF EXISTS global_rate_buckets;
ALTER TABLE settings DROP COLUMN IF EXISTS global_hourly_cap;
ALTER TABLE processed_items DROP COLUMN IF EXISTS attempts, DROP COLUMN IF EXISTS last_snatched_at;
ALTER TABLE snatch_policies DROP CONSTRAINT snatch_policies_selection_check;
ALTER TABLE snatch_policies ADD CONSTRAINT snatch_policies_selection_check CHECK (selection IN ('random', 'sequential'));
ALTER TABLE snatch_policies DROP COLUMN IF EXISTS recent_grab_window_h, DROP COLUMN IF EXISTS afterglow_max_h;
