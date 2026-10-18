-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Rule id_matched (ADR-0013): per-sweep import bound and the import read-back.

ALTER TABLE queue_policies
    ADD COLUMN max_imports_per_sweep INTEGER NOT NULL DEFAULT 25
        CHECK (max_imports_per_sweep BETWEEN 1 AND 100);

ALTER TABLE queue_strikes
    ADD COLUMN imported_at TIMESTAMPTZ,
    ADD COLUMN import_refused BOOLEAN NOT NULL DEFAULT FALSE;
