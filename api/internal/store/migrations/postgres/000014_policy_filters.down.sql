-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE snatch_policies
    DROP COLUMN IF EXISTS include_tags,
    DROP COLUMN IF EXISTS exclude_tags,
    DROP COLUMN IF EXISTS include_quality_profiles,
    DROP COLUMN IF EXISTS exclude_quality_profiles,
    DROP COLUMN IF EXISTS release_delay_h;
