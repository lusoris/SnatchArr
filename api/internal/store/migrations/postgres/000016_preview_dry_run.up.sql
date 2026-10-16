-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Dry run and preview (#37): dry_run flag and seed on runs, dry_run on policies.

ALTER TABLE snatch_runs
    ADD COLUMN dry_run BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN seed    BIGINT  NOT NULL DEFAULT 0;

ALTER TABLE snatch_policies
    ADD COLUMN dry_run BOOLEAN NOT NULL DEFAULT FALSE;
