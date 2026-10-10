-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Stamina becomes a rolling 60-minute window of one-minute buckets (#16). Rows written
-- before this migration are clock-hour buckets; each moves to the last minute of its hour,
-- so it keeps counting at least as long as any grant it holds and the window never
-- undercounts across the upgrade (it may overcount for up to one hour, once).
UPDATE rate_buckets SET window_start = window_start + interval '59 minutes'
WHERE window_start = date_trunc('hour', window_start, 'UTC');
UPDATE global_rate_buckets SET window_start = window_start + interval '59 minutes'
WHERE window_start = date_trunc('hour', window_start, 'UTC');
