-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

CREATE TABLE watchaware_sources (
    id           UUID PRIMARY KEY,
    kind         TEXT NOT NULL CHECK (kind IN ('jellyfin', 'emby')),
    name         TEXT NOT NULL UNIQUE,
    url          TEXT NOT NULL,
    api_key_enc  BYTEA NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    last_sync_at TIMESTAMPTZ,
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL
);

CREATE TABLE watchaware_item_watches (
    source_id         UUID NOT NULL REFERENCES watchaware_sources (id) ON DELETE CASCADE,
    user_id           TEXT NOT NULL,
    item_id           TEXT NOT NULL,
    user_name         TEXT NOT NULL DEFAULT '',
    item_type         TEXT NOT NULL DEFAULT '',
    series_id         TEXT NOT NULL DEFAULT '',
    season_number     INTEGER NOT NULL DEFAULT 0,
    episode_number    INTEGER NOT NULL DEFAULT 0,
    played            BOOLEAN NOT NULL DEFAULT FALSE,
    played_percentage DOUBLE PRECISION,
    resume_position   BIGINT NOT NULL DEFAULT 0,
    last_played_at    TEXT NOT NULL DEFAULT '',
    synced_at         TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (source_id, user_id, item_id)
);
