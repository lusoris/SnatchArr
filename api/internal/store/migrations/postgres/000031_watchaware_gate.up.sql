-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

CREATE TABLE watchaware_gate_baselines (
    instance_id  UUID PRIMARY KEY REFERENCES instances (id) ON DELETE CASCADE,
    baselined_at TIMESTAMPTZ NOT NULL,
    series_count INTEGER NOT NULL
);

CREATE TABLE watchaware_gate_series (
    instance_id       UUID NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    series_id         INTEGER NOT NULL,
    tvdb_id           INTEGER NOT NULL DEFAULT 0,
    title             TEXT NOT NULL DEFAULT '',
    gate_season       INTEGER NOT NULL,
    state             TEXT NOT NULL CHECK (state IN ('gated', 'exempt_tag', 'before_gate', 'files_beyond_gate')),
    added_before_gate BOOLEAN NOT NULL,
    held_seasons      INTEGER[] NOT NULL DEFAULT '{}',
    first_seen_at     TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (instance_id, series_id)
);

CREATE TABLE watchaware_gate_decisions (
    id            UUID PRIMARY KEY,
    instance_id   UUID NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    series_id     INTEGER NOT NULL,
    season_number INTEGER NOT NULL,
    action        TEXT NOT NULL CHECK (action IN ('unmonitor', 'promote')),
    would_act     BOOLEAN NOT NULL,
    reason        TEXT NOT NULL,
    decided_at    TIMESTAMPTZ NOT NULL,
    UNIQUE (instance_id, series_id, season_number, action)
);

CREATE INDEX watchaware_gate_decisions_decided_at_idx ON watchaware_gate_decisions (decided_at DESC);
