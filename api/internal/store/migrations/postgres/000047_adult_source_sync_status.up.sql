-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE adult_stashbox_instances
    ADD COLUMN last_sync_at TIMESTAMPTZ NULL,
    ADD COLUMN last_sync_error TEXT NULL,
    ADD COLUMN last_performers_synced INT NOT NULL DEFAULT 0,
    ADD COLUMN last_studios_synced INT NOT NULL DEFAULT 0;
