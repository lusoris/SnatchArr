-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: UpsertAdultStashboxInstance :one
INSERT INTO adult_stashbox_instances (
    id, name, endpoint, api_key_enc, enabled, sync_favorites, priority, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    endpoint = EXCLUDED.endpoint,
    api_key_enc = EXCLUDED.api_key_enc,
    enabled = EXCLUDED.enabled,
    sync_favorites = EXCLUDED.sync_favorites,
    priority = EXCLUDED.priority,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetAdultStashboxInstance :one
SELECT * FROM adult_stashbox_instances
WHERE id = $1;

-- name: ListSyncableAdultStashboxInstances :many
SELECT * FROM adult_stashbox_instances
WHERE enabled = TRUE AND sync_favorites = TRUE
ORDER BY priority ASC, name ASC;

-- name: ListEnabledAdultStashboxInstances :many
SELECT * FROM adult_stashbox_instances
WHERE enabled = TRUE
ORDER BY priority ASC, name ASC;

-- name: UpsertAdultFavoritePerformer :one
INSERT INTO adult_favorite_performers (
    id, canonical_name, disambiguation, urls, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (id) DO UPDATE SET
    canonical_name = EXCLUDED.canonical_name,
    disambiguation = COALESCE(EXCLUDED.disambiguation, adult_favorite_performers.disambiguation),
    urls = EXCLUDED.urls,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetAdultFavoritePerformer :one
SELECT * FROM adult_favorite_performers
WHERE id = $1;

-- name: GetAdultFavoritePerformerByLink :one
SELECT f.* FROM adult_favorite_performers f
JOIN adult_performer_links l ON l.favorite_id = f.id
WHERE l.instance_id = $1 AND l.foreign_id = $2;

-- name: ListAdultFavoritePerformers :many
SELECT * FROM adult_favorite_performers
ORDER BY canonical_name ASC;

-- name: UpsertAdultPerformerLink :one
INSERT INTO adult_performer_links (
    favorite_id, instance_id, foreign_id, is_favorite, last_synced_at
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (instance_id, foreign_id) DO UPDATE SET
    favorite_id = EXCLUDED.favorite_id,
    is_favorite = EXCLUDED.is_favorite,
    last_synced_at = EXCLUDED.last_synced_at
RETURNING *;

-- name: GetAdultPerformerLink :one
SELECT * FROM adult_performer_links
WHERE instance_id = $1 AND foreign_id = $2;

-- name: ListAdultPerformerLinks :many
SELECT * FROM adult_performer_links
ORDER BY instance_id, foreign_id;

-- name: ListAdultPerformerLinksForFavorite :many
SELECT * FROM adult_performer_links
WHERE favorite_id = $1
ORDER BY instance_id, foreign_id;

-- name: MarkStaleAdultPerformerLinksUnfavorite :execrows
UPDATE adult_performer_links
SET is_favorite = FALSE
WHERE instance_id = $1 AND last_synced_at < $2 AND is_favorite = TRUE;

-- name: UpsertAdultFavoriteStudio :one
INSERT INTO adult_favorite_studios (
    id, canonical_name, urls, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (id) DO UPDATE SET
    canonical_name = EXCLUDED.canonical_name,
    urls = EXCLUDED.urls,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetAdultFavoriteStudio :one
SELECT * FROM adult_favorite_studios
WHERE id = $1;

-- name: GetAdultFavoriteStudioByLink :one
SELECT f.* FROM adult_favorite_studios f
JOIN adult_studio_links l ON l.favorite_id = f.id
WHERE l.instance_id = $1 AND l.foreign_id = $2;

-- name: ListAdultFavoriteStudios :many
SELECT * FROM adult_favorite_studios
ORDER BY canonical_name ASC;

-- name: UpsertAdultStudioLink :one
INSERT INTO adult_studio_links (
    favorite_id, instance_id, foreign_id, is_favorite, last_synced_at
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (instance_id, foreign_id) DO UPDATE SET
    favorite_id = EXCLUDED.favorite_id,
    is_favorite = EXCLUDED.is_favorite,
    last_synced_at = EXCLUDED.last_synced_at
RETURNING *;

-- name: GetAdultStudioLink :one
SELECT * FROM adult_studio_links
WHERE instance_id = $1 AND foreign_id = $2;

-- name: ListAdultStudioLinks :many
SELECT * FROM adult_studio_links
ORDER BY instance_id, foreign_id;

-- name: ListAdultStudioLinksForFavorite :many
SELECT * FROM adult_studio_links
WHERE favorite_id = $1
ORDER BY instance_id, foreign_id;

-- name: MarkStaleAdultStudioLinksUnfavorite :execrows
UPDATE adult_studio_links
SET is_favorite = FALSE
WHERE instance_id = $1 AND last_synced_at < $2 AND is_favorite = TRUE;

-- name: UpsertAdultImageSet :one
INSERT INTO adult_image_sets (
    id, title, directory_path, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (directory_path) DO UPDATE SET
    title = EXCLUDED.title,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetAdultImageSetByDirectory :one
SELECT * FROM adult_image_sets
WHERE directory_path = $1;

-- name: GetAdultImageSet :one
SELECT * FROM adult_image_sets
WHERE id = $1;

-- name: ListAdultImageSets :many
SELECT * FROM adult_image_sets
ORDER BY created_at DESC;

-- name: UpdateAdultImageSetSummary :one
UPDATE adult_image_sets
SET
    item_count = $2,
    total_size_bytes = $3,
    cover_image_path = $4,
    updated_at = $5
WHERE id = $1
RETURNING *;

-- name: DeleteAdultImageSetsNotInPaths :execrows
DELETE FROM adult_image_sets
WHERE NOT (directory_path = ANY(@keep_paths::text[]));

-- name: DeleteAllAdultImageSets :execrows
DELETE FROM adult_image_sets;

-- name: UpsertAdultImageItem :one
INSERT INTO adult_image_items (
    id, image_set_id, file_path, file_name, file_size_bytes, mime_type, width, height, sha256_checksum, sort_order, created_at, file_mtime
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
ON CONFLICT (image_set_id, file_path) DO UPDATE SET
    file_name = EXCLUDED.file_name,
    file_size_bytes = EXCLUDED.file_size_bytes,
    mime_type = EXCLUDED.mime_type,
    width = EXCLUDED.width,
    height = EXCLUDED.height,
    sha256_checksum = EXCLUDED.sha256_checksum,
    sort_order = EXCLUDED.sort_order,
    file_mtime = EXCLUDED.file_mtime
RETURNING *;

-- name: GetAdultImageItem :one
SELECT * FROM adult_image_items
WHERE id = $1;

-- name: ListAdultImageItemsForSet :many
SELECT * FROM adult_image_items
WHERE image_set_id = $1
ORDER BY sort_order ASC, file_name ASC;

-- name: DeleteAdultImageItemsNotInPaths :execrows
DELETE FROM adult_image_items
WHERE image_set_id = $1 AND NOT (file_path = ANY(@keep_paths::text[]));

-- name: DeleteAllAdultImageItemsForSet :execrows
DELETE FROM adult_image_items
WHERE image_set_id = $1;

