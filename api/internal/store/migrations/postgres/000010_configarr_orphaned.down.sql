-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Drops the orphan marker on Configarr instances.
ALTER TABLE instances DROP COLUMN configarr_orphaned_at;
