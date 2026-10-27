-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE settings
    ADD COLUMN brand_mark TEXT NOT NULL DEFAULT 'vulva'
        CHECK (brand_mark IN ('vulva', 'penis', 'hand')),
    ADD COLUMN sfw_mode TEXT NOT NULL DEFAULT 'auto'
        CHECK (sfw_mode IN ('auto', 'on', 'off'));

CREATE TABLE users_prefs (
    user_id    UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    brand_mark TEXT NULL CHECK (brand_mark IN ('vulva', 'penis', 'hand')),
    sfw_mode   TEXT NULL CHECK (sfw_mode IN ('auto', 'on', 'off')),
    updated_at TIMESTAMPTZ NOT NULL
);
