-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: CreateInstance :one
INSERT INTO instances (id, kind, name, base_url, api_key_enc, enabled, source, configarr_key, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
RETURNING *;

-- name: GetInstance :one
SELECT * FROM instances WHERE id = $1;

-- name: GetInstanceByConfigarrKey :one
SELECT * FROM instances WHERE configarr_key = $1;

-- name: ListInstances :many
SELECT * FROM instances ORDER BY kind, name;

-- name: ListEnabledInstances :many
SELECT * FROM instances WHERE enabled AND kind <> 'stash' ORDER BY kind, name;


-- name: GetInstancePriority :one
SELECT id, kind, search_priority, download_priority FROM instances WHERE id = $1;

-- name: UpdateInstancePriority :one
UPDATE instances
SET search_priority = sqlc.arg(search_priority),
    download_priority = sqlc.arg(download_priority),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING id, kind, search_priority, download_priority;

-- name: ListInstancesToPlan :many
-- Instances never evaluated come first, then the one evaluated longest ago (#176).
SELECT * FROM instances WHERE enabled AND kind <> 'stash' ORDER BY planned_at NULLS FIRST, kind, name;

-- name: MarkInstancesPlanned :exec
UPDATE instances SET planned_at = sqlc.arg(planned_at)::timestamptz WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: UpdateInstance :one
UPDATE instances
SET kind = $2, name = $3, base_url = $4, api_key_enc = $5, enabled = $6, updated_at = $7,
    -- The product was seen at the old kind and address: a change of either forgets it.
    last_seen_product = CASE WHEN kind = $2 AND base_url = $4 THEN last_seen_product END
WHERE id = $1
RETURNING *;

-- name: UpsertConfigarrInstance :one
INSERT INTO instances (id, kind, name, base_url, api_key_enc, enabled, source, configarr_key, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'configarr', $7, $8, $8)
ON CONFLICT (configarr_key) DO UPDATE
SET kind = EXCLUDED.kind, name = EXCLUDED.name, base_url = EXCLUDED.base_url,
    api_key_enc = EXCLUDED.api_key_enc, enabled = EXCLUDED.enabled, updated_at = EXCLUDED.updated_at,
    configarr_orphaned_at = NULL,
    -- As in UpdateInstance: a change of kind or address forgets the product.
    last_seen_product = CASE
        WHEN instances.kind = EXCLUDED.kind AND instances.base_url = EXCLUDED.base_url THEN instances.last_seen_product
    END
RETURNING *, (xmax = 0) AS inserted;

-- name: DisableConfigarrInstancesNotIn :execrows
UPDATE instances
SET enabled = FALSE, last_error = 'removed from the Configarr configuration',
    updated_at = sqlc.arg(updated_at), configarr_orphaned_at = sqlc.arg(updated_at)
WHERE source = 'configarr' AND configarr_orphaned_at IS NULL AND NOT (configarr_key = ANY(sqlc.arg(keep_keys)::text[]));

-- name: CountInstancesByBaseURLAndNotSource :one
SELECT count(*) FROM instances WHERE lower(base_url) = lower($1) AND source <> $2;

-- name: RecordInstanceCheck :exec
UPDATE instances
SET last_seen_version = sqlc.narg(last_seen_version), last_check_at = sqlc.narg(last_check_at),
    last_error = sqlc.narg(last_error), updated_at = sqlc.narg(last_check_at),
    -- A failed check keeps the product: an instance that does not answer is still that product.
    last_seen_product = CASE
        WHEN sqlc.narg(last_error)::text IS NULL THEN sqlc.narg(last_seen_product)
        ELSE last_seen_product
    END
WHERE id = sqlc.arg(id);

-- name: DeleteInstanceUnlessDeclared :execrows
DELETE FROM instances WHERE id = $1 AND (source <> 'configarr' OR configarr_orphaned_at IS NOT NULL);

-- name: CountInstancesByBaseURL :one
SELECT count(*) FROM instances WHERE lower(base_url) = lower($1) AND id <> $2;
