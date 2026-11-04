-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

CREATE TABLE download_client_priority_ownership (
    client_id UUID NOT NULL REFERENCES download_clients(id) ON DELETE CASCADE,
    item_id TEXT NOT NULL,
    last_written_priority INT,
    paused_by_snatcharr BOOLEAN NOT NULL DEFAULT false,
    -- Write-ahead intent: recorded before a remote write, confirmed or cancelled by the next observation.
    pending_action TEXT NOT NULL DEFAULT '' CHECK (pending_action IN ('', 'priority', 'pause', 'resume')),
    pending_priority INT,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (client_id, item_id)
);
