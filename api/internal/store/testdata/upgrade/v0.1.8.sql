-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Representative database fixture for SnatchArr v0.1.8 (schema version 9).
-- Contains at least one row in every table present in the v9 schema.

INSERT INTO instances (
    id, kind, name, base_url, api_key_enc, enabled, source, configarr_key,
    last_seen_version, last_check_at, last_error, created_at, updated_at, last_seen_product
) VALUES
    ('11111111-1111-1111-1111-111111111111', 'sonarr', 'sonarr-main', 'http://sonarr.local:8989', '\x01020304', TRUE, 'manual', NULL, '4.0.0.700', '2026-10-14 10:00:00+00', NULL, '2026-10-14 09:00:00+00', '2026-10-14 09:00:00+00', NULL),
    ('22222222-2222-2222-2222-222222222222', 'radarr', 'radarr-4k', 'http://radarr.local:7878', '\x01020304', TRUE, 'configarr', 'radarr_4k', '5.0.0.800', '2026-10-14 10:00:00+00', NULL, '2026-10-14 09:00:00+00', '2026-10-14 09:00:00+00', NULL),
    ('33333333-3333-3333-3333-333333333333', 'lidarr', 'lidarr-music', 'http://lidarr.local:8686', '\x01020304', TRUE, 'manual', NULL, '2.0.0.100', '2026-10-14 10:00:00+00', NULL, '2026-10-14 09:00:00+00', '2026-10-14 09:00:00+00', NULL),
    ('44444444-4444-4444-4444-444444444444', 'readarr', 'readarr-books', 'http://readarr.local:8787', '\x01020304', TRUE, 'manual', NULL, '0.3.0.100', '2026-10-14 10:00:00+00', NULL, '2026-10-14 09:00:00+00', '2026-10-14 09:00:00+00', 'readarr'),
    ('55555555-5555-5555-5555-555555555555', 'whisparr_v2', 'whisparr-v2', 'http://whisparrv2.local:6969', '\x01020304', TRUE, 'manual', NULL, '2.0.0.50', '2026-10-14 10:00:00+00', NULL, '2026-10-14 09:00:00+00', '2026-10-14 09:00:00+00', NULL),
    ('66666666-6666-6666-6666-666666666666', 'whisparr_v3', 'whisparr-v3', 'http://whisparrv3.local:6969', '\x01020304', TRUE, 'manual', NULL, '3.0.0.10', '2026-10-14 10:00:00+00', NULL, '2026-10-14 09:00:00+00', '2026-10-14 09:00:00+00', NULL);

INSERT INTO users (
    id, username, password_hash, role, created_at, updated_at
) VALUES (
    'c1111111-1111-1111-1111-111111111111', 'admin', '$2a$10$placeholderpasswordhash', 'admin', '2026-10-14 09:00:00+00', '2026-10-14 09:00:00+00'
);

INSERT INTO seerr_links (
    id, name, base_url, api_key_enc, enabled, sonarr_instance_id, radarr_instance_id,
    last_seen_version, last_check_at, last_error, last_sync_at, created_at, updated_at
) VALUES (
    'e1111111-1111-1111-1111-111111111111', 'overseerr-main', 'http://overseerr.local:5055', '\x01020304', TRUE,
    '11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222222',
    '1.33.2', '2026-10-14 10:00:00+00', NULL, '2026-10-14 10:00:00+00', '2026-10-14 09:00:00+00', '2026-10-14 09:00:00+00'
);

INSERT INTO cleanuparr_links (
    id, name, base_url, api_key_enc, enabled, last_seen_version, last_check_at, last_error, created_at, updated_at
) VALUES (
    'f1111111-1111-1111-1111-111111111111', 'cleanuparr-main', 'http://cleanuparr.local:9090', '\x01020304', TRUE,
    '0.2.1', '2026-10-14 10:00:00+00', NULL, '2026-10-14 09:00:00+00', '2026-10-14 09:00:00+00'
);

INSERT INTO snatch_policies (
    instance_id, missing_per_cycle, upgrade_per_cycle, cycle_interval_s, hourly_cap,
    selection, monitored_only, skip_future_releases, radarr_release_type,
    sonarr_missing_mode, sonarr_upgrade_mode, lidarr_missing_mode,
    processed_ttl_h, max_queue_size, await_command, page_size,
    cursor_missing, cursor_upgrade, updated_at, recent_grab_window_h, afterglow_max_h
) VALUES (
    '11111111-1111-1111-1111-111111111111', 5, 2, 900, 25,
    'recent', TRUE, TRUE, 'physical',
    'episodes', 'episodes', 'artist',
    168, -1, FALSE, 100,
    'cursor-m', 'cursor-u', '2026-10-14 09:00:00+00', 24, 720
);

INSERT INTO snatch_runs (
    id, instance_id, kind, status, leased_by, lease_expires_at,
    queued_at, started_at, finished_at, searched_count, error,
    focus_entity_id, focus_group_id, lease_count
) VALUES (
    'a1111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111',
    'missing', 'queued', NULL, NULL,
    '2026-10-14 10:00:00+00', NULL, NULL, 0, NULL,
    42, 10, 0
);

INSERT INTO snatch_events (
    id, run_id, instance_id, ts, level, type, entity_type, entity_id, title, detail
) VALUES (
    1, 'a1111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111',
    '2026-10-14 10:01:00+00', 'info', 'search_dispatched', 'episode', 1001,
    'Test Series S01E01', 'Dispatched test search'
);

INSERT INTO processed_items (
    instance_id, kind, entity_type, entity_id, expires_at, attempts, last_snatched_at
) VALUES (
    '11111111-1111-1111-1111-111111111111', 'missing', 'episode', 1001,
    '2026-10-21 10:00:00+00', 1, '2026-10-14 10:00:00+00'
);

INSERT INTO rate_buckets (
    instance_id, window_start, used
) VALUES (
    '11111111-1111-1111-1111-111111111111', '2026-10-14 10:00:00+00', 5
);

INSERT INTO global_rate_buckets (
    window_start, used
) VALUES (
    '2026-10-14 10:00:00+00', 12
);

INSERT INTO schedules (
    id, instance_id, name, days_mask, start_time, end_time, tz, action, cap_value, enabled, created_at, updated_at
) VALUES (
    'b1111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111',
    'Nightly Pause', 127, '02:00:00', '05:00:00', 'UTC', 'pause', NULL, TRUE,
    '2026-10-14 09:00:00+00', '2026-10-14 09:00:00+00'
);

INSERT INTO settings (
    singleton, history_retention_days, user_agent, updated_at, global_hourly_cap
) VALUES (
    TRUE, 90, 'SnatchArr/1.0', '2026-10-14 09:00:00+00', 100
);

INSERT INTO sessions (
    id, data, expires_at
) VALUES (
    'sess-token-12345', '{"user_id": "c1111111-1111-1111-1111-111111111111"}'::jsonb, '2026-10-21 10:00:00+00'
);

INSERT INTO api_keys (
    id, owner_id, scopes, hash, created_at, expires_at, revoked_at
) VALUES (
    'snatcharr_key_abc123', 'c1111111-1111-1111-1111-111111111111',
    ARRAY['admin', 'read'], '\x0102030405060708', '2026-10-14 09:00:00+00', '2026-11-14 09:00:00+00', NULL
);

INSERT INTO download_clients (
    id, instance_id, kind, name, base_url, username, secret_enc, enabled,
    source, remote_id, max_active, bandwidth_budget_bps, last_check_at, last_error, created_at, updated_at
) VALUES (
    'd1111111-1111-1111-1111-111111111111', '11111111-1111-1111-1111-111111111111',
    'qbittorrent', 'qbit-local', 'http://qbit.local:8080', 'admin', '\x01020304', TRUE,
    'manual', NULL, 5, 10000000, '2026-10-14 10:00:00+00', NULL, '2026-10-14 09:00:00+00', '2026-10-14 09:00:00+00'
);

INSERT INTO seerr_requests (
    link_id, request_id, media_type, tmdb_id, tvdb_id, title, request_status, media_status,
    is_4k, requested_by, seasons, seerr_server_id, instance_id, entity_id, requested_at,
    last_snatched_at, last_seen_at, updated_at
) VALUES (
    'e1111111-1111-1111-1111-111111111111', 101, 'tv', 1399, 121361, 'Game of Thrones',
    2, 3, FALSE, 'user1', ARRAY[1, 2], 1, '11111111-1111-1111-1111-111111111111', 1001,
    '2026-10-14 08:00:00+00', NULL, '2026-10-14 10:00:00+00', '2026-10-14 10:00:00+00'
);

INSERT INTO workers (
    id, last_contact_at
) VALUES (
    'worker-node-alpha', '2026-10-14 10:05:00+00'
);
