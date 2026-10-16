-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Policy filters (#36): include and exclude lists of *arr tag ids and quality profile ids, and
-- a delay after release before an item is searched.

ALTER TABLE snatch_policies
    ADD COLUMN include_tags             INTEGER[] NOT NULL DEFAULT '{}'
        CHECK (cardinality(include_tags) <= 100),
    ADD COLUMN exclude_tags             INTEGER[] NOT NULL DEFAULT '{}'
        CHECK (cardinality(exclude_tags) <= 100),
    ADD COLUMN include_quality_profiles INTEGER[] NOT NULL DEFAULT '{}'
        CHECK (cardinality(include_quality_profiles) <= 100),
    ADD COLUMN exclude_quality_profiles INTEGER[] NOT NULL DEFAULT '{}'
        CHECK (cardinality(exclude_quality_profiles) <= 100),
    ADD COLUMN release_delay_h          INTEGER NOT NULL DEFAULT 0
        CHECK (release_delay_h BETWEEN 0 AND 8760);
