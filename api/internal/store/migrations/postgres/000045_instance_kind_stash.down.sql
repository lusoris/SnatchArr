-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Restores the original instances_kind_check without deleting stash rows:
-- ADD CONSTRAINT fails loudly when any row with kind = 'stash' is still present.

ALTER TABLE instances DROP CONSTRAINT instances_kind_check;
ALTER TABLE instances ADD CONSTRAINT instances_kind_check
    CHECK (kind IN ('sonarr', 'radarr', 'lidarr', 'readarr', 'whisparr_v2', 'whisparr_v3'));
