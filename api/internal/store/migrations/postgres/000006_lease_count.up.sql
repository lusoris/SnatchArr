-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Count how often a run was leased, so a run whose lease keeps expiring (a run that
-- crashes every worker) ends failed instead of looping forever (#17).

ALTER TABLE snatch_runs ADD COLUMN lease_count INTEGER NOT NULL DEFAULT 0 CHECK (lease_count >= 0);
