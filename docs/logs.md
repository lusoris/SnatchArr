<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
-->

# Logs

The **Logs** page shows the API's recent log records and tails new ones live, so a problem can be
read without shell access to the container. Only administrators see the page and its menu entry;
the API refuses everyone else with `403`.

## Using the page

- **Minimum level** shows records at that level and above (`debug`, `info`, `warn`, `error`).
- **Search** matches the message and every attribute key and value, ignoring case.
- **Source** narrows to one producer, for example `api`. Empty means all sources.

The page loads the newest 500 matching records, then adds each new record from the live stream on
top. The line above the list says how many records the server keeps and how many older ones were
already overwritten.

## What is kept

Records live in a fixed-size ring in the API's memory (`api/internal/logring`). The ring holds
`APP_SNATCHARR_LOGS_RING_SIZE` records (default 2000, clamped to 100 to 20000; see
[Configuration](configuration.md)). Nothing is written to disk: a restart empties the ring, and each
replica keeps its own. The regular log output (`APP_LOG_FORMAT`) is unchanged; the ring keeps a
copy.

Before a record enters the ring, secrets are replaced with `[REDACTED]`:

- attributes whose last key segment is a secret name (`key`, `api_key`, `apikey`, `x-api-key`,
  `password`, `token`, `secret`, `authorization`, `cookie`, `webhook_url`, ...) or ends in one
  (`_token`, `_password`, `_api_key`, ...);
- credential query parameters (`apikey=`, `api_key=`, `token=`, `access_token=`, `password=`,
  `secret=`), `X-Api-Key:` and `Bearer` values, and URL user info in any message or value;
- any run of 32 or more hexadecimal characters, the shape of an *arr API key (`trace_id` and
  `span_id` keep theirs).

`api/internal/logring/redact_test.go` plants one secret of each shape and fails if any reaches
the ring.

## API

| Call | Purpose |
| --- | --- |
| `GET /api/v1/logs?level=&q=&source=&limit=` | Newest matching records first, plus ring capacity and the overwritten count ([API](api.md)) |
| `GET /api/v1/logs/stream` | Server-Sent Events: each new record as one default `message` event, `id` = record sequence number, data = the record as JSON |

The stream sends every record; the page applies its filters to streamed records the same way the
list call does.
