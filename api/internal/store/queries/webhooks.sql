-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

-- name: SaveWebhookEndpoint :one
INSERT INTO webhook_endpoints (id, url, secret_enc, events, active, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $6)
ON CONFLICT (id) DO UPDATE SET
  url = EXCLUDED.url,
  secret_enc = EXCLUDED.secret_enc,
  events = EXCLUDED.events,
  active = EXCLUDED.active,
  updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: FindWebhookEndpoint :one
SELECT * FROM webhook_endpoints WHERE id = $1;

-- name: ListWebhookEndpoints :many
SELECT * FROM webhook_endpoints
WHERE active AND ($1::text = '' OR $1 = ANY(events) OR cardinality(events) = 0)
ORDER BY id;

-- name: DeleteWebhookEndpoint :execrows
DELETE FROM webhook_endpoints WHERE id = $1;

-- name: SaveWebhookDelivery :one
INSERT INTO webhook_deliveries (id, endpoint_id, event, payload, attempts, status, status_code, error, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO UPDATE SET
  attempts = EXCLUDED.attempts,
  status = EXCLUDED.status,
  status_code = EXCLUDED.status_code,
  error = EXCLUDED.error,
  updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: FindWebhookDelivery :one
SELECT * FROM webhook_deliveries WHERE id = $1;

-- name: ListDeadLetterDeliveries :many
SELECT * FROM webhook_deliveries WHERE status = 'failed' ORDER BY created_at DESC;
