-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: TouchWorker :exec
-- A late write from a slower replica never moves the contact back.
INSERT INTO workers (id, last_contact_at) VALUES (sqlc.arg(id), sqlc.arg(at)::timestamptz)
ON CONFLICT (id) DO UPDATE SET last_contact_at = GREATEST(workers.last_contact_at, EXCLUDED.last_contact_at);

-- name: LastWorkerContact :one
SELECT last_contact_at FROM workers ORDER BY last_contact_at DESC LIMIT 1;

-- name: PurgeWorkersBefore :execrows
DELETE FROM workers
WHERE id IN (SELECT id FROM workers WHERE last_contact_at < sqlc.arg(before)::timestamptz
    LIMIT sqlc.arg(batch)::int);
