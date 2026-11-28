-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

BEGIN;

-- Replace the btree uniqueness on (instance_id, root_path) with a hash
-- exclusion constraint, so long root_path values cannot exceed btree tuple limits.
-- Existing rows are preserved; the ADD CONSTRAINT fails and rolls back if any duplicates exist.
DROP INDEX IF EXISTS library_processing_rules_library_uidx;

ALTER TABLE library_processing_rules
    ADD CONSTRAINT library_processing_rules_library_path_excl
    EXCLUDE USING hash (((instance_id::text || root_path) COLLATE "C") WITH =)
    WHERE (scope = 'library');

-- Hash exclusion cannot serve instance-scoped lookups, so keep an efficient
-- partial btree on instance_id. The existing query is unchanged.
CREATE INDEX library_processing_rules_library_instance_idx
    ON library_processing_rules (instance_id)
    WHERE scope = 'library';

COMMIT;
