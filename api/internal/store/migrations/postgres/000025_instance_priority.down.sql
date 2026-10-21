-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE instances
    DROP COLUMN IF EXISTS search_priority,
    DROP COLUMN IF EXISTS download_priority;

ALTER TABLE settings
    DROP COLUMN IF EXISTS search_priority_preset,
    DROP COLUMN IF EXISTS download_priority_preset;

-- Leave the settings singleton row: it predates nothing this migration removes.
