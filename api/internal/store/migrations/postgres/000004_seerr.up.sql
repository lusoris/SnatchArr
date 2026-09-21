-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Seerr: request-driven priority snatching (read-only; SnatchArr never writes to Seerr).

ALTER TABLE instances DROP CONSTRAINT instances_source_check;
ALTER TABLE instances ADD CONSTRAINT instances_source_check CHECK (source IN ('manual', 'configarr', 'seerr'));

CREATE TABLE seerr_links (
    id                 UUID PRIMARY KEY,
    name               TEXT NOT NULL UNIQUE,
    base_url           TEXT NOT NULL,
    api_key_enc        BYTEA NOT NULL,
    enabled            BOOLEAN NOT NULL DEFAULT TRUE,
    -- Fallback instances for requests whose Seerr server cannot be matched by URL.
    sonarr_instance_id UUID REFERENCES instances (id) ON DELETE SET NULL,
    radarr_instance_id UUID REFERENCES instances (id) ON DELETE SET NULL,
    last_seen_version  TEXT,
    last_check_at      TIMESTAMPTZ,
    last_error         TEXT,
    last_sync_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL
);

CREATE TABLE seerr_requests (
    link_id         UUID NOT NULL REFERENCES seerr_links (id) ON DELETE CASCADE,
    request_id      INTEGER NOT NULL,
    media_type      TEXT NOT NULL CHECK (media_type IN ('movie', 'tv')),
    tmdb_id         INTEGER NOT NULL DEFAULT 0,
    tvdb_id         INTEGER NOT NULL DEFAULT 0,
    title           TEXT NOT NULL DEFAULT '',
    request_status  INTEGER NOT NULL,
    media_status    INTEGER NOT NULL,
    is_4k           BOOLEAN NOT NULL DEFAULT FALSE,
    requested_by    TEXT NOT NULL DEFAULT '',
    seasons         INTEGER[] NOT NULL DEFAULT '{}',
    seerr_server_id INTEGER,
    instance_id     UUID REFERENCES instances (id) ON DELETE SET NULL,
    entity_id       BIGINT,
    requested_at    TIMESTAMPTZ,
    last_snatched_at TIMESTAMPTZ,
    last_seen_at    TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (link_id, request_id)
);
CREATE INDEX seerr_requests_instance_idx ON seerr_requests (instance_id, entity_id);

-- A quickie for one request focuses the run on that entity (movie) or group (series).
ALTER TABLE snatch_runs
    ADD COLUMN focus_entity_id BIGINT,
    ADD COLUMN focus_group_id  BIGINT;
