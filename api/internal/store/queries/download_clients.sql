-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: CreateDownloadClient :one
INSERT INTO download_clients (
    id, instance_id, kind, name, base_url, username, secret_enc, enabled, source, remote_id,
    max_active, bandwidth_budget_bps, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $13
)
RETURNING *;

-- name: GetDownloadClient :one
SELECT * FROM download_clients WHERE id = $1;

-- name: ListDownloadClients :many
SELECT * FROM download_clients ORDER BY name, created_at;

-- name: ListEnabledDownloadClients :many
SELECT * FROM download_clients WHERE enabled ORDER BY name, created_at;

-- name: ListDownloadClientsForInstance :many
SELECT * FROM download_clients
WHERE enabled AND (instance_id IS NULL OR instance_id = $1)
ORDER BY name, created_at;

-- name: UpdateDownloadClient :one
UPDATE download_clients
SET instance_id = $2, kind = $3, name = $4, base_url = $5, username = $6, secret_enc = $7,
    enabled = $8, max_active = $9, bandwidth_budget_bps = $10, updated_at = $11
WHERE id = $1
RETURNING *;

-- name: UpsertDiscoveredDownloadClient :one
INSERT INTO download_clients (
    id, instance_id, kind, name, base_url, username, secret_enc, enabled, source, remote_id, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, 'discovered', $9, $10, $10
)
ON CONFLICT (instance_id, remote_id) WHERE remote_id IS NOT NULL DO UPDATE
SET kind = EXCLUDED.kind, name = EXCLUDED.name, base_url = EXCLUDED.base_url, username = EXCLUDED.username,
    secret_enc = EXCLUDED.secret_enc, enabled = EXCLUDED.enabled, updated_at = EXCLUDED.updated_at
RETURNING *, (xmax = 0) AS inserted;

-- name: DeleteStaleDiscoveredDownloadClients :execrows
DELETE FROM download_clients
WHERE instance_id = $1 AND source = 'discovered' AND remote_id IS NOT NULL
  AND NOT (remote_id = ANY(@keep_remote_ids::int[]));

-- name: RecordDownloadClientCheck :exec
UPDATE download_clients
SET last_check_at = $2, last_error = $3, updated_at = $2
WHERE id = $1;

-- name: DeleteDownloadClient :execrows
DELETE FROM download_clients WHERE id = $1;
