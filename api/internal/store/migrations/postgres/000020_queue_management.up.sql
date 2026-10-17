-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Queue management (#48, #362): per-instance queue policies and strike state.

CREATE TABLE queue_policies (
    instance_id             UUID PRIMARY KEY REFERENCES instances (id) ON DELETE CASCADE,
    enabled                 BOOLEAN NOT NULL DEFAULT FALSE,
    rules                   JSONB NOT NULL DEFAULT '{}',
    grace_s                 INTEGER NOT NULL DEFAULT 900 CHECK (grace_s BETWEEN 0 AND 86400),
    min_bytes_per_second    BIGINT NOT NULL DEFAULT 0 CHECK (min_bytes_per_second >= 0),
    min_custom_format_score INTEGER,
    max_removals_per_sweep  INTEGER NOT NULL DEFAULT 10 CHECK (max_removals_per_sweep BETWEEN 1 AND 100),
    updated_at              TIMESTAMPTZ NOT NULL
);

CREATE TABLE queue_strikes (
    instance_id    UUID NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    download_id    TEXT NOT NULL,
    queue_id       INTEGER NOT NULL,
    rule           TEXT NOT NULL DEFAULT '',
    count          INTEGER NOT NULL DEFAULT 0,
    last_size_left BIGINT NOT NULL,
    last_seen      TIMESTAMPTZ NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (instance_id, download_id)
);
CREATE INDEX queue_strikes_expires_at_idx ON queue_strikes (expires_at);
