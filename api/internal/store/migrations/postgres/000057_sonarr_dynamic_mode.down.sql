-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE snatch_policies
    DROP CONSTRAINT IF EXISTS snatch_policies_sonarr_missing_mode_check,
    DROP CONSTRAINT IF EXISTS snatch_policies_sonarr_upgrade_mode_check;

UPDATE snatch_policies SET sonarr_missing_mode = 'episodes' WHERE sonarr_missing_mode = 'dynamic';
UPDATE snatch_policies SET sonarr_upgrade_mode = 'episodes' WHERE sonarr_upgrade_mode = 'dynamic';

ALTER TABLE snatch_policies
    ADD CONSTRAINT snatch_policies_sonarr_missing_mode_check
        CHECK (sonarr_missing_mode IN ('episodes', 'season_packs', 'shows')),
    ADD CONSTRAINT snatch_policies_sonarr_upgrade_mode_check
        CHECK (sonarr_upgrade_mode IN ('episodes', 'season_packs'));
