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

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = $2, updated_at = $3
WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users ORDER BY username;

-- name: LoadSession :one
SELECT data FROM sessions WHERE id = $1 AND expires_at > $2;

-- name: SaveSession :exec
INSERT INTO sessions (id, data, expires_at) VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, expires_at = EXCLUDED.expires_at;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = $1;

-- data->>'user_id' couples to sessionUserKey in api/internal/httpapi/security.go.
-- name: DeleteUserSessionsExcept :execrows
DELETE FROM sessions
WHERE data->>'user_id' = sqlc.arg(user_id)::text
  AND id != sqlc.arg(current_session_id)::text;

-- name: PurgeExpiredSessions :execrows
DELETE FROM sessions
WHERE id IN (SELECT id FROM sessions WHERE expires_at <= sqlc.arg(now)::timestamptz LIMIT sqlc.arg(batch)::int);

-- name: SaveAPIKey :exec
INSERT INTO api_keys (id, owner_id, scopes, hash, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetAPIKey :one
SELECT * FROM api_keys WHERE id = $1;

-- name: RevokeAPIKey :execrows
UPDATE api_keys SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: ListAPIKeysByOwner :many
SELECT * FROM api_keys WHERE owner_id = $1 AND revoked_at IS NULL ORDER BY created_at;

-- name: RecordLoginLockoutFailure :exec
INSERT INTO login_lockouts (key, fails, first_fail_at, locked_until)
VALUES (
    sqlc.arg(key),
    1,
    sqlc.arg(now)::timestamptz,
    CASE WHEN 1 >= sqlc.arg(max_fails)::int
         THEN sqlc.arg(now)::timestamptz + make_interval(secs => sqlc.arg(cooldown_s)::double precision)
         ELSE NULL
    END
)
ON CONFLICT (key) DO UPDATE SET
    fails = CASE
        WHEN login_lockouts.first_fail_at < sqlc.arg(now)::timestamptz - make_interval(secs => sqlc.arg(window_s)::double precision)
        THEN 1
        ELSE login_lockouts.fails + 1
    END,
    first_fail_at = CASE
        WHEN login_lockouts.first_fail_at < sqlc.arg(now)::timestamptz - make_interval(secs => sqlc.arg(window_s)::double precision)
        THEN sqlc.arg(now)::timestamptz
        ELSE login_lockouts.first_fail_at
    END,
    locked_until = CASE
        WHEN (
            CASE
                WHEN login_lockouts.first_fail_at < sqlc.arg(now)::timestamptz - make_interval(secs => sqlc.arg(window_s)::double precision)
                THEN 1
                ELSE login_lockouts.fails + 1
            END
        ) >= sqlc.arg(max_fails)::int
        THEN sqlc.arg(now)::timestamptz + make_interval(secs => sqlc.arg(cooldown_s)::double precision)
        ELSE NULL
    END;

-- name: GetLoginLockout :one
SELECT key, fails, first_fail_at, locked_until
FROM login_lockouts
WHERE key = $1;

-- name: DeleteLoginLockout :exec
DELETE FROM login_lockouts
WHERE key = $1;

-- name: PurgeExpiredLoginLockouts :execrows
DELETE FROM login_lockouts
WHERE key IN (
    SELECT key FROM login_lockouts
    WHERE (locked_until IS NULL OR locked_until < sqlc.arg(now)::timestamptz)
      AND first_fail_at < sqlc.arg(now)::timestamptz - make_interval(secs => sqlc.arg(window_s)::double precision)
    LIMIT sqlc.arg(batch)::int
);

