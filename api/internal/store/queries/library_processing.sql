-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

-- name: CreateLibraryProcessingRule :one
INSERT INTO library_processing_rules (
    id,
    scope,
    instance_id,
    item_id,
    user_language,
    user_language_code,
    root_path,
    media_type,
    rule,
    created_at,
    updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: GetLibraryProcessingRule :one
SELECT * FROM library_processing_rules
WHERE id = $1;

-- name: ListLibraryProcessingRulesKeyset :many
SELECT lpr.*
FROM library_processing_rules AS lpr
CROSS JOIN LATERAL (
    SELECT
        CASE lpr.scope
            WHEN 'item_override' THEN 1
            WHEN 'user_language' THEN 2
            WHEN 'library' THEN 3
            WHEN 'instance' THEN 4
            WHEN 'media_type' THEN 5
            WHEN 'global_default' THEN 6
            ELSE 7
        END AS scope_rank
) AS r
WHERE (
    NOT sqlc.arg(use_cursor)::boolean
    OR (r.scope_rank, lpr.created_at, lpr.id) > (
        sqlc.arg(after_rank)::integer,
        sqlc.arg(after_created_at)::timestamptz,
        sqlc.arg(after_id)::uuid
    )
)
ORDER BY
    r.scope_rank ASC,
    lpr.created_at ASC,
    lpr.id ASC
LIMIT sqlc.arg(page_limit)::integer;

-- name: UpdateLibraryProcessingRule :one
UPDATE library_processing_rules
SET scope = $2,
    instance_id = $3,
    item_id = $4,
    user_language = $5,
    user_language_code = $6,
    root_path = $7,
    media_type = $8,
    rule = $9,
    updated_at = $10
WHERE id = $1
RETURNING *;

-- name: DeleteLibraryProcessingRule :execrows
DELETE FROM library_processing_rules
WHERE id = $1;

-- name: ListMatchingLibraryProcessingRules :many
SELECT * FROM library_processing_rules
WHERE (scope = 'item_override'
       AND sqlc.narg(instance_id)::uuid IS NOT NULL
       AND sqlc.narg(item_id)::text IS NOT NULL
       AND instance_id = sqlc.narg(instance_id)::uuid
       AND item_id = sqlc.narg(item_id)::text)
   OR (scope = 'user_language'
       AND sqlc.narg(user_language_code)::text IS NOT NULL
       AND user_language_code = sqlc.narg(user_language_code)::text)
   OR (scope = 'library'
       AND sqlc.narg(instance_id)::uuid IS NOT NULL
       AND sqlc.narg(root_path)::text IS NOT NULL
       AND instance_id = sqlc.narg(instance_id)::uuid
       AND root_path = sqlc.narg(root_path)::text)
   OR (scope = 'instance'
       AND sqlc.narg(instance_id)::uuid IS NOT NULL
       AND instance_id = sqlc.narg(instance_id)::uuid)
   OR (scope = 'media_type'
       AND sqlc.narg(media_type)::text IS NOT NULL
       AND media_type = sqlc.narg(media_type)::text)
   OR (scope = 'global_default')
LIMIT 6;
