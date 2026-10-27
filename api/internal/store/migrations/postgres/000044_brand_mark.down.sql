-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

DROP TABLE IF EXISTS users_prefs;

ALTER TABLE settings
    DROP COLUMN IF EXISTS sfw_mode,
    DROP COLUMN IF EXISTS brand_mark;
