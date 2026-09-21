-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Cleanuparr: read-only status and strike visibility (ADR-0004).

CREATE TABLE cleanuparr_links (
    id                UUID PRIMARY KEY,
    name              TEXT NOT NULL UNIQUE,
    base_url          TEXT NOT NULL,
    api_key_enc       BYTEA NOT NULL,
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    last_seen_version TEXT,
    last_check_at     TIMESTAMPTZ,
    last_error        TEXT,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL
);
