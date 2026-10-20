-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Policy defaults updated per ADR-0020.

ALTER TABLE snatch_policies ALTER COLUMN missing_per_cycle SET DEFAULT 2;
ALTER TABLE snatch_policies ALTER COLUMN upgrade_per_cycle SET DEFAULT 1;
ALTER TABLE snatch_policies ALTER COLUMN hourly_cap SET DEFAULT 15;
