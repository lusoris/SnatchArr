-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Lockout state for throttled login attempts across API replicas.
CREATE TABLE login_lockouts (
    key           TEXT PRIMARY KEY,
    fails         INT NOT NULL,
    first_fail_at TIMESTAMPTZ NOT NULL,
    locked_until  TIMESTAMPTZ
);
