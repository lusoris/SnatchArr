-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

-- Migration 000055: switch stored import_fixable blocklist false to true.
-- Hidden-toggle saves were never a user choice (#989 follow-up).
-- Rows without import_fixable key or with blocklist true or absent stay byte-identical.
UPDATE queue_policies
SET rules = jsonb_set(rules, '{import_fixable,blocklist}', 'true'::jsonb)
WHERE (rules->'import_fixable'->'blocklist') = 'false'::jsonb;
