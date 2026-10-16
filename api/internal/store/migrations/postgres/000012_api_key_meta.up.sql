-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE api_keys
    ADD COLUMN name TEXT NOT NULL DEFAULT '',
    ADD COLUMN last_used_at TIMESTAMPTZ;
