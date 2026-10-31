-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

CREATE TABLE runtime_settings (
    key text PRIMARY KEY,
    value text NOT NULL,
    updated_by uuid NULL,
    updated_at timestamptz NOT NULL
);
