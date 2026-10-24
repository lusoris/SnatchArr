-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

CREATE TABLE modules (
    id VARCHAR(64) PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL
);

INSERT INTO modules (id, enabled, updated_at)
SELECT 'llm', llm_enabled, updated_at
FROM settings
WHERE singleton
ON CONFLICT (id) DO UPDATE SET enabled = EXCLUDED.enabled;
