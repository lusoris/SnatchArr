-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- The connection test of a stored instance records which product answered, for an instance
-- kind that several products serve (#22). NULL: never tested, or a kind one product serves.
ALTER TABLE instances ADD COLUMN last_seen_product TEXT
    CHECK (last_seen_product IN ('readarr', 'bookshelf', 'chaptarr', 'readarr_faustvii', 'unknown'));
