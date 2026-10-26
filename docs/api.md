<!--
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: CC-BY-SA-4.0
-->

# API

The HTTP API is an OpenAPI 3.1 contract at
[`api/openapi/openapi.yaml`](https://github.com/lusoris/SnatchArr/blob/main/api/openapi/openapi.yaml),
served under `/api/v1`. The Go server is generated from it with ogen; the web UI's client
types are generated from the same file. Errors are RFC 9457 problem details
(`application/problem+json`). The contract is append-only (HISS-14); breaking changes are
checked against the base revision with `oasdiff` (`make api-openapi-breaking`) and require a
breaking conventional-commit subject (`!` prefix, e.g. `feat(api)!: ...`) and a `Migration:` footer.

## Authentication

- **Browser sessions**: `POST /api/v1/auth/login` sets the `snatcharr_session` cookie.
  Unsafe methods from a browser must be same-origin: a request whose `Sec-Fetch-Site` or
  `Origin` header names another site gets `403` problem+json. The `csrf_token` that
  `GET /api/v1/auth/session` returns is kept for clients that send `X-CSRF-Token`; the API does
  not check it.
- **API keys**: `Authorization: Bearer sk_...` on any request; the origin check does not apply.

The Server-Sent Events streams `GET /api/v1/events/stream` and `GET /api/v1/logs/stream` are
served outside the generated router and take the same session cookie or API key; without either
they answer 401, and the log stream answers 403 to a non-admin.

The first start has no users; `GET /api/v1/auth/setup/status` says so and
`POST /api/v1/auth/setup` creates the admin.

## Resources

| Path | What |
| --- | --- |
| `/instances`, `/instances/{id}/test`, `/instances/test` | *arr instances and connectivity probes |
| `/instances/{id}/policy` | Snatch policy |
| `/instances/{id}/runs` | Quickie (manual snatch) |
| `/runs`, `/runs/{id}/cancel` | Snatch runs |
| `/events`, `/events/stream` | History and Server-Sent Events |
| `/logs`, `/logs/stream` | Recent redacted log records and their live tail, admin only ([Logs](logs.md)) |
| `/hourly-caps` | Stamina per instance |
| `/state/reset` | End every afterglow |
| `/schedules` | Pause and cap-override windows |
| `/download-clients`, `/download-clients/status`, `/instances/{id}/download-clients/discover` | Download clients |
| `/system/status` | Version and build info; `worker_last_contact_at`, the newest LeaseRun or Heartbeat of any worker (absent before the first) |

## Worker contract

Workers talk gRPC (`proto/snatcharr/v1/worker.proto`): `LeaseRun`, `Heartbeat`,
`FilterCandidates`, `AcquireBudget`, `ReportEvents`, `CompleteRun`. The API is the only
source of truth for stamina and afterglow; a worker never decides on its own.

`LeaseRun` is a long-poll: with no run queued, the API checks the queue once a second until the
wait the worker asked for (at most 30 s) has passed, then answers empty. A run queued meanwhile
reaches a waiting worker within about a second, and each run reaches exactly one worker.
`api/internal/workergrpc/latency_integration_test.go` holds both against 64 concurrent
long-polls on one Postgres: an empty poll answers within 2 s after its wait, and a queued run
reaches a worker within 3 s.

A worker retries a `CompleteRun` the API refused with `UNAVAILABLE`, `DEADLINE_EXCEEDED`,
`RESOURCE_EXHAUSTED` or `ABORTED`: three calls at most, the n-th wait a random time
between half and all of 1 s x 2^(n-1). Any other refusal ends the attempt; the run then
fails once its lease expires `MaxLeases` times. `CompleteRun` carries `events_unconfirmed`,
the events the worker emitted for the run that `ReportEvents` did not confirm as accepted.
A non-zero value records an `events_unconfirmed` warning on the run: its history may be
incomplete.
