-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2
--
-- An older build does not know the unidentified fork; its rows read as unknown until the next connection test.
UPDATE instances SET last_seen_product = 'unknown' WHERE last_seen_product = 'readarr_fork';
ALTER TABLE instances DROP CONSTRAINT instances_last_seen_product_check;
ALTER TABLE instances ADD CONSTRAINT instances_last_seen_product_check
    CHECK (last_seen_product IN ('readarr', 'bookshelf', 'chaptarr', 'readarr_faustvii', 'readarr_rresurrected', 'unknown'));
