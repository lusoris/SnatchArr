-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Prowlarr link hourly cap and rate buckets (#777).

ALTER TABLE prowlarr_links
    ADD COLUMN hourly_cap INTEGER NOT NULL DEFAULT 15 CHECK (hourly_cap BETWEEN 1 AND 500);

CREATE TABLE prowlarr_rate_buckets (
    link_id      UUID NOT NULL REFERENCES prowlarr_links (id) ON DELETE CASCADE,
    window_start TIMESTAMPTZ NOT NULL,
    used         INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (link_id, window_start)
);
