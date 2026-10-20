-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Revert policy defaults back to pre-ADR-0020 values.

ALTER TABLE snatch_policies ALTER COLUMN missing_per_cycle SET DEFAULT 1;
ALTER TABLE snatch_policies ALTER COLUMN upgrade_per_cycle SET DEFAULT 0;
ALTER TABLE snatch_policies ALTER COLUMN hourly_cap SET DEFAULT 20;
