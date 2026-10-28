-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2
--
-- The connection test can name the readarr-rresurrected fork.
ALTER TABLE instances DROP CONSTRAINT instances_last_seen_product_check;
ALTER TABLE instances ADD CONSTRAINT instances_last_seen_product_check
    CHECK (last_seen_product IN ('readarr', 'bookshelf', 'chaptarr', 'readarr_faustvii', 'readarr_rresurrected', 'unknown'));
