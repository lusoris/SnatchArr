-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE adult_favorite_performers
    ADD COLUMN urls TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE adult_favorite_studios
    ADD COLUMN urls TEXT[] NOT NULL DEFAULT '{}';
