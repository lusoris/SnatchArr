-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE download_clients
    ADD COLUMN max_queued INTEGER NOT NULL DEFAULT 0 CHECK (max_queued BETWEEN 0 AND 1000);
