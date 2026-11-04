-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

-- After the up migration every stored import_fixable blocklist false is an operator choice.
-- Stepping down keeps each value and marks such a false with blocklist_kept, so a later up
-- migration preserves it. Version 54 decodes rules leniently and ignores the marker.
UPDATE queue_policies
SET rules = jsonb_set(rules, '{import_fixable,blocklist_kept}', 'true'::jsonb)
WHERE (rules->'import_fixable'->'blocklist') = 'false'::jsonb;
