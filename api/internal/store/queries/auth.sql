-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: CreateUser :one
INSERT INTO users (id, username, password_hash, role, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5)
RETURNING *;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users ORDER BY username;

-- name: LoadSession :one
SELECT data FROM sessions WHERE id = $1 AND expires_at > $2;

-- name: SaveSession :exec
INSERT INTO sessions (id, data, expires_at) VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, expires_at = EXCLUDED.expires_at;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = $1;

-- name: PurgeExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= $1;

-- name: SaveAPIKey :exec
INSERT INTO api_keys (id, owner_id, scopes, hash, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetAPIKey :one
SELECT * FROM api_keys WHERE id = $1;

-- name: RevokeAPIKey :execrows
UPDATE api_keys SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: ListAPIKeysByOwner :many
SELECT * FROM api_keys WHERE owner_id = $1 AND revoked_at IS NULL ORDER BY created_at;
