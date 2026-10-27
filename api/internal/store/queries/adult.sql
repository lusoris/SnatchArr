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

-- name: ListAdultStashboxInstances :many
SELECT * FROM adult_stashbox_instances
ORDER BY priority ASC, name ASC;

-- name: DeleteAdultStashboxInstance :execrows
DELETE FROM adult_stashbox_instances
WHERE id = $1;

-- name: RecordAdultStashboxSyncResult :exec
UPDATE adult_stashbox_instances
SET
    last_sync_at = $2,
    last_sync_error = $3,
    last_performers_synced = $4,
    last_studios_synced = $5
WHERE id = $1;

-- name: UpsertAdultPerformer :one
INSERT INTO adult_performers (
    id, canonical_name, disambiguation, urls, is_favorite, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (id) DO UPDATE SET
    canonical_name = EXCLUDED.canonical_name,
    disambiguation = COALESCE(EXCLUDED.disambiguation, adult_performers.disambiguation),
    urls = EXCLUDED.urls,
    is_favorite = EXCLUDED.is_favorite,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetAdultPerformer :one
SELECT * FROM adult_performers
WHERE id = $1;

-- name: GetAdultPerformerByExternalRef :one
SELECT p.* FROM adult_performers p
JOIN adult_external_refs r ON r.entity_type = 'performer' AND r.entity_id = p.id
WHERE r.source_kind = $1
  AND r.source_instance_id IS NOT DISTINCT FROM $2
  AND r.foreign_id = $3;

-- name: ListAdultPerformers :many
SELECT * FROM adult_performers
ORDER BY canonical_name ASC;

-- name: ListAdultFavoritePerformers :many
SELECT * FROM adult_performers
WHERE is_favorite = TRUE
ORDER BY canonical_name ASC;

-- name: ClearUnfavoritedAdultPerformers :execrows
UPDATE adult_performers p
SET is_favorite = FALSE,
    updated_at = $1
WHERE p.is_favorite = TRUE
  AND NOT EXISTS (
      SELECT 1 FROM adult_external_refs r
      WHERE r.entity_type = 'performer'
        AND r.entity_id = p.id
        AND r.is_favorite = TRUE
  );

-- name: UpsertAdultStudio :one
INSERT INTO adult_studios (
    id, canonical_name, urls, is_favorite, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (id) DO UPDATE SET
    canonical_name = EXCLUDED.canonical_name,
    urls = EXCLUDED.urls,
    is_favorite = EXCLUDED.is_favorite,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: GetAdultStudio :one
SELECT * FROM adult_studios
WHERE id = $1;

-- name: GetAdultStudioByExternalRef :one
SELECT s.* FROM adult_studios s
JOIN adult_external_refs r ON r.entity_type = 'studio' AND r.entity_id = s.id
WHERE r.source_kind = $1
  AND r.source_instance_id IS NOT DISTINCT FROM $2
  AND r.foreign_id = $3;

-- name: ListAdultStudios :many
SELECT * FROM adult_studios
ORDER BY canonical_name ASC;

-- name: ListAdultFavoriteStudios :many
SELECT * FROM adult_studios
WHERE is_favorite = TRUE
ORDER BY canonical_name ASC;

-- name: ClearUnfavoritedAdultStudios :execrows
UPDATE adult_studios s
SET is_favorite = FALSE,
    updated_at = $1
WHERE s.is_favorite = TRUE
  AND NOT EXISTS (
      SELECT 1 FROM adult_external_refs r
      WHERE r.entity_type = 'studio'
        AND r.entity_id = s.id
        AND r.is_favorite = TRUE
  );

-- name: UpsertAdultExternalRef :one
INSERT INTO adult_external_refs (
    id, entity_type, entity_id, source_kind, source_instance_id,
    foreign_id, secondary_id, canonical_url, extra_urls,
    is_favorite, confidence_score, match_method, fetched_at, last_seen_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12, $13, $14
)
ON CONFLICT (source_kind, source_instance_id, entity_type, foreign_id) DO UPDATE SET
    entity_id = EXCLUDED.entity_id,
    secondary_id = COALESCE(EXCLUDED.secondary_id, adult_external_refs.secondary_id),
    canonical_url = COALESCE(EXCLUDED.canonical_url, adult_external_refs.canonical_url),
    extra_urls = EXCLUDED.extra_urls,
    is_favorite = EXCLUDED.is_favorite,
    confidence_score = COALESCE(EXCLUDED.confidence_score, adult_external_refs.confidence_score),
    match_method = COALESCE(EXCLUDED.match_method, adult_external_refs.match_method),
    fetched_at = EXCLUDED.fetched_at,
    last_seen_at = EXCLUDED.last_seen_at
RETURNING *;

-- name: GetAdultExternalRef :one
SELECT * FROM adult_external_refs
WHERE source_kind = $1
  AND source_instance_id IS NOT DISTINCT FROM $2
  AND entity_type = $3
  AND foreign_id = $4;

-- name: ListAdultExternalRefsByEntityType :many
SELECT * FROM adult_external_refs
WHERE entity_type = $1
ORDER BY source_kind ASC, source_instance_id ASC, foreign_id ASC;

-- name: ListAdultExternalRefsByEntity :many
SELECT * FROM adult_external_refs
WHERE entity_type = $1 AND entity_id = $2
ORDER BY source_kind ASC, source_instance_id ASC, foreign_id ASC;

-- name: MarkStaleAdultExternalRefsUnfavorite :execrows
UPDATE adult_external_refs
SET is_favorite = FALSE
WHERE source_kind = 'stashbox'
  AND source_instance_id = $1
  AND entity_type = $2
  AND last_seen_at < $3
  AND is_favorite = TRUE;

-- name: DeleteAdultExternalRefsByEntity :execrows
DELETE FROM adult_external_refs
WHERE entity_type = $1 AND entity_id = $2;

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

-- name: CountAdultImageSets :one
SELECT count(*) FROM adult_image_sets;

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
