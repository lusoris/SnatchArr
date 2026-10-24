-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

UPDATE settings
SET llm_enabled = COALESCE((SELECT enabled FROM modules WHERE id = 'llm'), llm_enabled)
WHERE singleton;

DROP TABLE modules;
