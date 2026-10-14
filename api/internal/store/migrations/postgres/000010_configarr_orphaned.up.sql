-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Marks Configarr-imported instances that disappeared from config.yml ("orphaned"). Only those
-- can be deleted; an instance that config.yml still declares cannot, whether enabled or not.
-- Rows the import already switched off for that reason are marked as orphaned.
ALTER TABLE instances ADD COLUMN configarr_orphaned_at timestamptz;

UPDATE instances
SET configarr_orphaned_at = updated_at
WHERE source = 'configarr' AND NOT enabled AND last_error = 'removed from the Configarr configuration';
