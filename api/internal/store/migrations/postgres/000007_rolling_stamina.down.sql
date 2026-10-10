-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Back to clock-hour buckets: one-minute buckets are summed into the hour they fall in.
CREATE TEMPORARY TABLE hourly_rate_buckets AS
SELECT instance_id, date_trunc('hour', window_start, 'UTC') AS window_start, sum(used)::int AS used
FROM rate_buckets GROUP BY 1, 2;
DELETE FROM rate_buckets;
INSERT INTO rate_buckets (instance_id, window_start, used) SELECT instance_id, window_start, used FROM hourly_rate_buckets;
DROP TABLE hourly_rate_buckets;

CREATE TEMPORARY TABLE hourly_global_rate_buckets AS
SELECT date_trunc('hour', window_start, 'UTC') AS window_start, sum(used)::int AS used
FROM global_rate_buckets GROUP BY 1;
DELETE FROM global_rate_buckets;
INSERT INTO global_rate_buckets (window_start, used) SELECT window_start, used FROM hourly_global_rate_buckets;
DROP TABLE hourly_global_rate_buckets;
