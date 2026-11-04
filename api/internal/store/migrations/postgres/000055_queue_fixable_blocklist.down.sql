-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

-- No-op: after the up migration a stored true cannot be told apart from a true the user
-- chose, so stepping down keeps every import_fixable blocklist value as it is.
SELECT 1;
