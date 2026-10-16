-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE api_keys
    DROP COLUMN IF EXISTS last_used_at,
    DROP COLUMN IF EXISTS name;
