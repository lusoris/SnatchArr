-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

DELETE FROM notification_rules WHERE event_kind = 'watchaware_gate_decided';
ALTER TABLE notification_rules DROP CONSTRAINT notification_rules_event_kind_check;
ALTER TABLE notification_rules ADD CONSTRAINT notification_rules_event_kind_check CHECK (
    event_kind IN (
        'run_failure_streak',
        'circuit_opened',
        'instance_unreachable',
        'stamina_exhausted',
        'seerr_request_snatched'
    )
);
