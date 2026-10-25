-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE watchaware_gate_decisions ADD COLUMN applied_at TIMESTAMPTZ NULL;

ALTER TABLE watchaware_gate_series DROP CONSTRAINT watchaware_gate_series_state_check;
ALTER TABLE watchaware_gate_series ADD CONSTRAINT watchaware_gate_series_state_check CHECK (state IN ('gated', 'exempt_tag', 'before_gate', 'files_beyond_gate', 'released'));
ALTER TABLE watchaware_gate_series ADD COLUMN released_at TIMESTAMPTZ NULL;
