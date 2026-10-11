-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- The product is detected again by the next connection test; nothing else depends on it.
ALTER TABLE instances DROP COLUMN last_seen_product;
