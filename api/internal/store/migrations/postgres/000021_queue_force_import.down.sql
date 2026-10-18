-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE queue_strikes DROP COLUMN IF EXISTS import_refused;
ALTER TABLE queue_strikes DROP COLUMN IF EXISTS imported_at;
ALTER TABLE queue_policies DROP COLUMN IF EXISTS max_imports_per_sweep;
