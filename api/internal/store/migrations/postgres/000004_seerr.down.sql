-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE snatch_runs DROP COLUMN IF EXISTS focus_entity_id, DROP COLUMN IF EXISTS focus_group_id;
DROP TABLE IF EXISTS seerr_requests;
DROP TABLE IF EXISTS seerr_links;
ALTER TABLE instances DROP CONSTRAINT instances_source_check;
ALTER TABLE instances ADD CONSTRAINT instances_source_check CHECK (source IN ('manual', 'configarr'));
