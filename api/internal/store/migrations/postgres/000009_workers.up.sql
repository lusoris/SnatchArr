-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- When each worker last reached the control plane (LeaseRun or Heartbeat), so the status shows
-- whether the data plane is alive (gap audit A1-01). The retention janitor drops a worker that
-- stayed silent past history_retention_days.
CREATE TABLE workers (
    id              TEXT PRIMARY KEY CHECK (id <> ''),
    last_contact_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX workers_last_contact_at_idx ON workers (last_contact_at);
