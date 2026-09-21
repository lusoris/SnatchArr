-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: GetPolicy :one
SELECT * FROM snatch_policies WHERE instance_id = $1;

-- name: EnsureDefaultPolicy :one
INSERT INTO snatch_policies (instance_id, updated_at)
VALUES ($1, $2)
ON CONFLICT (instance_id) DO UPDATE SET instance_id = EXCLUDED.instance_id
RETURNING *;

-- name: UpsertPolicy :one
INSERT INTO snatch_policies (
    instance_id, missing_per_cycle, upgrade_per_cycle, cycle_interval_s, hourly_cap, selection,
    monitored_only, skip_future_releases, radarr_release_type, sonarr_missing_mode, sonarr_upgrade_mode,
    lidarr_missing_mode, processed_ttl_h, max_queue_size, await_command, page_size, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16, $17
)
ON CONFLICT (instance_id) DO UPDATE SET
    missing_per_cycle = EXCLUDED.missing_per_cycle,
    upgrade_per_cycle = EXCLUDED.upgrade_per_cycle,
    cycle_interval_s = EXCLUDED.cycle_interval_s,
    hourly_cap = EXCLUDED.hourly_cap,
    selection = EXCLUDED.selection,
    monitored_only = EXCLUDED.monitored_only,
    skip_future_releases = EXCLUDED.skip_future_releases,
    radarr_release_type = EXCLUDED.radarr_release_type,
    sonarr_missing_mode = EXCLUDED.sonarr_missing_mode,
    sonarr_upgrade_mode = EXCLUDED.sonarr_upgrade_mode,
    lidarr_missing_mode = EXCLUDED.lidarr_missing_mode,
    processed_ttl_h = EXCLUDED.processed_ttl_h,
    max_queue_size = EXCLUDED.max_queue_size,
    await_command = EXCLUDED.await_command,
    page_size = EXCLUDED.page_size,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: SavePolicyCursor :exec
UPDATE snatch_policies
SET cursor_missing = CASE WHEN sqlc.arg(kind)::text = 'missing' THEN sqlc.arg(cursor)::text ELSE cursor_missing END,
    cursor_upgrade = CASE WHEN sqlc.arg(kind)::text = 'upgrade' THEN sqlc.arg(cursor)::text ELSE cursor_upgrade END,
    updated_at = sqlc.arg(updated_at)
WHERE instance_id = sqlc.arg(instance_id);
