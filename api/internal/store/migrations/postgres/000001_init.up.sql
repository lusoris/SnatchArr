-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Timestamps are supplied by the application (golusoris core/clock), never by NOW(),
-- so every query is reproducible under a fake clock.

CREATE TABLE instances (
    id                UUID PRIMARY KEY,
    kind              TEXT NOT NULL CHECK (kind IN ('sonarr', 'radarr', 'lidarr', 'readarr', 'whisparr_v2', 'whisparr_v3')),
    name              TEXT NOT NULL UNIQUE,
    base_url          TEXT NOT NULL,
    api_key_enc       BYTEA NOT NULL,
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    source            TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'configarr')),
    configarr_key     TEXT UNIQUE,
    last_seen_version TEXT,
    last_check_at     TIMESTAMPTZ,
    last_error        TEXT,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL
);

CREATE TABLE hunt_policies (
    instance_id          UUID PRIMARY KEY REFERENCES instances (id) ON DELETE CASCADE,
    missing_per_cycle    INTEGER NOT NULL DEFAULT 1 CHECK (missing_per_cycle BETWEEN 0 AND 100),
    upgrade_per_cycle    INTEGER NOT NULL DEFAULT 0 CHECK (upgrade_per_cycle BETWEEN 0 AND 100),
    cycle_interval_s     INTEGER NOT NULL DEFAULT 900 CHECK (cycle_interval_s >= 60),
    hourly_cap           INTEGER NOT NULL DEFAULT 20 CHECK (hourly_cap BETWEEN 1 AND 500),
    selection            TEXT NOT NULL DEFAULT 'random' CHECK (selection IN ('random', 'sequential')),
    monitored_only       BOOLEAN NOT NULL DEFAULT TRUE,
    skip_future_releases BOOLEAN NOT NULL DEFAULT TRUE,
    radarr_release_type  TEXT NOT NULL DEFAULT 'physical' CHECK (radarr_release_type IN ('physical', 'digital', 'cinema')),
    sonarr_missing_mode  TEXT NOT NULL DEFAULT 'episodes' CHECK (sonarr_missing_mode IN ('episodes', 'season_packs', 'shows')),
    sonarr_upgrade_mode  TEXT NOT NULL DEFAULT 'episodes' CHECK (sonarr_upgrade_mode IN ('episodes', 'season_packs')),
    lidarr_missing_mode  TEXT NOT NULL DEFAULT 'artist' CHECK (lidarr_missing_mode IN ('artist', 'album')),
    processed_ttl_h      INTEGER NOT NULL DEFAULT 168 CHECK (processed_ttl_h BETWEEN 1 AND 8760),
    max_queue_size       INTEGER NOT NULL DEFAULT -1 CHECK (max_queue_size >= -1),
    await_command        BOOLEAN NOT NULL DEFAULT FALSE,
    page_size            INTEGER NOT NULL DEFAULT 100 CHECK (page_size BETWEEN 10 AND 1000),
    cursor_missing       TEXT NOT NULL DEFAULT '',
    cursor_upgrade       TEXT NOT NULL DEFAULT '',
    updated_at           TIMESTAMPTZ NOT NULL
);

CREATE TABLE hunt_runs (
    id               UUID PRIMARY KEY,
    instance_id      UUID NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    kind             TEXT NOT NULL CHECK (kind IN ('missing', 'upgrade')),
    status           TEXT NOT NULL CHECK (status IN ('queued', 'leased', 'done', 'failed', 'cancelled')),
    leased_by        TEXT,
    lease_expires_at TIMESTAMPTZ,
    queued_at        TIMESTAMPTZ NOT NULL,
    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,
    searched_count   INTEGER NOT NULL DEFAULT 0,
    error            TEXT
);
CREATE INDEX hunt_runs_queue_idx ON hunt_runs (status, queued_at) WHERE status IN ('queued', 'leased');
CREATE INDEX hunt_runs_instance_idx ON hunt_runs (instance_id, finished_at DESC);

CREATE TABLE processed_items (
    instance_id UUID NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('missing', 'upgrade')),
    entity_type TEXT NOT NULL,
    entity_id   BIGINT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (instance_id, kind, entity_type, entity_id)
);
CREATE INDEX processed_items_expiry_idx ON processed_items (expires_at);

CREATE TABLE rate_buckets (
    instance_id  UUID NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    window_start TIMESTAMPTZ NOT NULL,
    used         INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance_id, window_start)
);

CREATE TABLE hunt_events (
    id          BIGSERIAL PRIMARY KEY,
    run_id      UUID REFERENCES hunt_runs (id) ON DELETE SET NULL,
    instance_id UUID NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    ts          TIMESTAMPTZ NOT NULL,
    level       TEXT NOT NULL CHECK (level IN ('debug', 'info', 'warn', 'error')),
    type        TEXT NOT NULL,
    entity_type TEXT NOT NULL DEFAULT '',
    entity_id   BIGINT NOT NULL DEFAULT 0,
    title       TEXT NOT NULL DEFAULT '',
    detail      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX hunt_events_instance_ts_idx ON hunt_events (instance_id, ts DESC);
CREATE INDEX hunt_events_ts_idx ON hunt_events (ts);

CREATE TABLE schedules (
    id          UUID PRIMARY KEY,
    instance_id UUID REFERENCES instances (id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    days_mask   INTEGER NOT NULL CHECK (days_mask BETWEEN 1 AND 127),
    start_time  TIME NOT NULL,
    end_time    TIME NOT NULL,
    tz          TEXT NOT NULL DEFAULT 'UTC',
    action      TEXT NOT NULL CHECK (action IN ('pause', 'cap_override')),
    cap_value   INTEGER CHECK (cap_value IS NULL OR cap_value BETWEEN 1 AND 500),
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

CREATE TABLE settings (
    singleton              BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    history_retention_days INTEGER NOT NULL DEFAULT 90 CHECK (history_retention_days BETWEEN 1 AND 3650),
    user_agent             TEXT NOT NULL DEFAULT 'SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)',
    updated_at             TIMESTAMPTZ NOT NULL
);

CREATE TABLE users (
    id            UUID PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'admin' CHECK (role IN ('admin', 'viewer')),
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL
);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    data       JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_expiry_idx ON sessions (expires_at);

CREATE TABLE api_keys (
    id         TEXT PRIMARY KEY,
    owner_id   UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    scopes     TEXT[] NOT NULL DEFAULT '{}',
    hash       BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);
CREATE INDEX api_keys_owner_idx ON api_keys (owner_id) WHERE revoked_at IS NULL;
