-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- When the planner last evaluated an instance (#176): a tick plans the instances it
-- evaluated longest ago first, so the bound per tick never leaves the same ones out.

ALTER TABLE instances ADD COLUMN planned_at TIMESTAMPTZ;
