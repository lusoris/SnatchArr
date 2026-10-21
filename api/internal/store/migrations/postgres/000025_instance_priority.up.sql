-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

-- Per-instance search and download priority ranks (ADR-0022). Lower numbers run first (1..100).
ALTER TABLE settings
    ADD COLUMN search_priority_preset JSONB NOT NULL
        DEFAULT '{"sonarr":10,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40,"whisparr_v3":40}'::jsonb,
    ADD COLUMN download_priority_preset JSONB NOT NULL
        DEFAULT '{"sonarr":10,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40,"whisparr_v3":40}'::jsonb;

ALTER TABLE instances
    ADD COLUMN search_priority INTEGER
        CHECK (search_priority IS NULL OR search_priority BETWEEN 1 AND 100),
    ADD COLUMN download_priority INTEGER
        CHECK (download_priority IS NULL OR download_priority BETWEEN 1 AND 100);

-- LeaseRun LEFT JOINs the settings singleton for the preset; without the row every un-overridden instance sorts NULLS LAST behind any override.
INSERT INTO settings (singleton, updated_at) VALUES (TRUE, now()) ON CONFLICT (singleton) DO NOTHING;
