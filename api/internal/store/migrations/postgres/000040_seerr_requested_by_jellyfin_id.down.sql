-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE seerr_requests
    DROP COLUMN IF EXISTS requested_by_jellyfin_id;
