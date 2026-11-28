-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

BEGIN;

-- Build the old btree first. If any row cannot fit in a btree entry, this fails,
-- the whole transaction rolls back, and both the rows and the exclusion
-- constraint remain intact.
CREATE UNIQUE INDEX library_processing_rules_library_uidx
    ON library_processing_rules (instance_id, root_path)
    WHERE scope = 'library';

DROP INDEX IF EXISTS library_processing_rules_library_instance_idx;

ALTER TABLE library_processing_rules
    DROP CONSTRAINT IF EXISTS library_processing_rules_library_path_excl;

COMMIT;
