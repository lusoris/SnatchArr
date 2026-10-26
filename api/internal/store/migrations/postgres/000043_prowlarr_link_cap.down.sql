-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

DROP TABLE IF EXISTS prowlarr_rate_buckets;

ALTER TABLE prowlarr_links
    DROP COLUMN IF EXISTS hourly_cap;
