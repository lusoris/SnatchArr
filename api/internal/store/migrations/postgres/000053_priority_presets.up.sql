-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

-- Named snapshots are independent of the active maps in settings (ADR-0022).
CREATE TABLE priority_presets (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40 AND name = btrim(name)),
    search JSONB NOT NULL CHECK (jsonb_typeof(search) = 'object'),
    download JSONB NOT NULL CHECK (jsonb_typeof(download) = 'object'),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT priority_presets_reserved_name CHECK (lower(name) <> 'media_first')
);

CREATE UNIQUE INDEX priority_presets_name_lower_idx ON priority_presets (lower(name));
