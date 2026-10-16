-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

CREATE TABLE notification_channels (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('discord', 'slack', 'telegram', 'ntfy', 'gotify', 'teams', 'webhook')),
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    config_enc BYTEA NOT NULL,
    has_secret BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX notification_channels_name_idx ON notification_channels (name);

CREATE TABLE notification_rules (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL,
    event_kind  TEXT NOT NULL CHECK (event_kind IN ('run_failure_streak', 'circuit_opened', 'instance_unreachable', 'stamina_exhausted', 'seerr_request_snatched')),
    threshold   INTEGER NOT NULL DEFAULT 1 CHECK (threshold >= 1),
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    channel_ids UUID[] NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX notification_rules_event_kind_idx ON notification_rules (event_kind);

CREATE TABLE webhook_endpoints (
    id          TEXT PRIMARY KEY,
    url         TEXT NOT NULL,
    secret_enc  BYTEA NOT NULL DEFAULT '',
    events      TEXT[] NOT NULL DEFAULT '{}',
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE webhook_deliveries (
    id          TEXT PRIMARY KEY,
    endpoint_id TEXT NOT NULL REFERENCES webhook_endpoints (id) ON DELETE CASCADE,
    event       TEXT NOT NULL,
    payload     BYTEA NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL CHECK (status IN ('pending', 'delivered', 'failed')),
    status_code INTEGER NOT NULL DEFAULT 0,
    error       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX webhook_deliveries_endpoint_idx ON webhook_deliveries (endpoint_id, created_at DESC);
CREATE INDEX webhook_deliveries_status_idx ON webhook_deliveries (status) WHERE status = 'failed';
