-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: ListPriorityOwnershipByClientIDs :many
SELECT client_id, item_id, last_written_priority, paused_by_snatcharr, pending_action, pending_priority, updated_at
FROM download_client_priority_ownership
WHERE client_id = ANY(@client_ids::uuid[])
ORDER BY client_id, item_id;

-- name: UpsertPriorityOwnershipPriority :exec
INSERT INTO download_client_priority_ownership (
    client_id, item_id, last_written_priority, paused_by_snatcharr, updated_at
) VALUES (
    $1, $2, $3, false, $4
)
ON CONFLICT (client_id, item_id) DO UPDATE
SET pending_action = '', pending_priority = NULL,
    last_written_priority = EXCLUDED.last_written_priority,
    updated_at = EXCLUDED.updated_at;

-- name: SetPriorityOwnershipPaused :exec
INSERT INTO download_client_priority_ownership (
    client_id, item_id, last_written_priority, paused_by_snatcharr, updated_at
) VALUES (
    $1, $2, NULL, true, $3
)
ON CONFLICT (client_id, item_id) DO UPDATE
SET pending_action = '', pending_priority = NULL,
    paused_by_snatcharr = true,
    updated_at = EXCLUDED.updated_at;

-- name: ClearPriorityOwnershipPaused :exec
UPDATE download_client_priority_ownership
SET pending_action = '', pending_priority = NULL,
    paused_by_snatcharr = false,
    updated_at = $3
WHERE client_id = $1 AND item_id = $2;

-- name: DeletePriorityOwnershipNotIn :execrows
DELETE FROM download_client_priority_ownership
WHERE client_id = $1
  AND NOT (item_id = ANY(@keep_item_ids::text[]));

-- name: PreparePriorityIntent :exec
INSERT INTO download_client_priority_ownership (client_id, item_id, pending_action, pending_priority, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (client_id, item_id) DO UPDATE
SET pending_action = EXCLUDED.pending_action,
    pending_priority = EXCLUDED.pending_priority,
    updated_at = EXCLUDED.updated_at;

-- name: CancelPriorityIntent :exec
UPDATE download_client_priority_ownership
SET pending_action = '', pending_priority = NULL, updated_at = $3
WHERE client_id = $1 AND item_id = $2;
