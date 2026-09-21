-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Download clients are observed for backpressure only (ADR-0006). SnatchArr never adds,
-- removes or reorders downloads.

CREATE TABLE download_clients (
    id                   UUID PRIMARY KEY,
    -- NULL applies to every instance; discovered clients belong to the instance that listed them.
    instance_id          UUID REFERENCES instances (id) ON DELETE CASCADE,
    kind                 TEXT NOT NULL CHECK (kind IN ('qbittorrent', 'transmission', 'deluge', 'rtorrent', 'sabnzbd', 'nzbget')),
    name                 TEXT NOT NULL,
    base_url             TEXT NOT NULL,
    username             TEXT NOT NULL DEFAULT '',
    secret_enc           BYTEA NOT NULL,
    enabled              BOOLEAN NOT NULL DEFAULT TRUE,
    source               TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'discovered')),
    remote_id            INTEGER,
    max_active           INTEGER NOT NULL DEFAULT 0 CHECK (max_active BETWEEN 0 AND 1000),
    bandwidth_budget_bps BIGINT NOT NULL DEFAULT 0 CHECK (bandwidth_budget_bps >= 0),
    last_check_at        TIMESTAMPTZ,
    last_error           TEXT,
    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX download_clients_discovered_idx ON download_clients (instance_id, remote_id) WHERE remote_id IS NOT NULL;
CREATE INDEX download_clients_instance_idx ON download_clients (instance_id);
