-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

CREATE TABLE queue_output_cursors (
    instance_id UUID PRIMARY KEY REFERENCES instances (id) ON DELETE CASCADE,
    last_parent TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
