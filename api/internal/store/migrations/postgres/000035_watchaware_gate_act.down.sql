-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

UPDATE watchaware_gate_series SET state = 'gated' WHERE state = 'released';
ALTER TABLE watchaware_gate_series DROP CONSTRAINT watchaware_gate_series_state_check;
ALTER TABLE watchaware_gate_series ADD CONSTRAINT watchaware_gate_series_state_check CHECK (state IN ('gated', 'exempt_tag', 'before_gate', 'files_beyond_gate'));
ALTER TABLE watchaware_gate_series DROP COLUMN IF EXISTS released_at;
ALTER TABLE watchaware_gate_decisions DROP COLUMN IF EXISTS applied_at;
