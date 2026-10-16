-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE snatch_policies
    DROP COLUMN dry_run;

ALTER TABLE snatch_runs
    DROP COLUMN seed,
    DROP COLUMN dry_run;
