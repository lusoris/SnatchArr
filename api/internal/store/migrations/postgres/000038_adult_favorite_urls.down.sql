-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE adult_favorite_studios
    DROP COLUMN IF EXISTS urls;

ALTER TABLE adult_favorite_performers
    DROP COLUMN IF EXISTS urls;
