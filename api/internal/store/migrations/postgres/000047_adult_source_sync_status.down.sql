-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE adult_stashbox_instances
    DROP COLUMN last_studios_synced,
    DROP COLUMN last_performers_synced,
    DROP COLUMN last_sync_error,
    DROP COLUMN last_sync_at;
