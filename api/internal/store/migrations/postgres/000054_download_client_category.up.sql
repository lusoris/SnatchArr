-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE download_clients
    ADD COLUMN category TEXT,
    ADD COLUMN priority_mode TEXT NOT NULL DEFAULT 'observe'
        CONSTRAINT download_clients_priority_mode_check CHECK (priority_mode IN ('observe', 'act'));
