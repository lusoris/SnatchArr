-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

CREATE TABLE adult_stashbox_instances (
    id UUID PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    endpoint VARCHAR(512) NOT NULL,
    api_key_enc BYTEA NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sync_favorites BOOLEAN NOT NULL DEFAULT TRUE,
    priority INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE adult_favorite_performers (
    id UUID PRIMARY KEY,
    canonical_name VARCHAR(255) NOT NULL,
    disambiguation VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE adult_performer_links (
    favorite_id UUID NOT NULL REFERENCES adult_favorite_performers(id) ON DELETE CASCADE,
    instance_id UUID NOT NULL REFERENCES adult_stashbox_instances(id) ON DELETE CASCADE,
    foreign_id VARCHAR(64) NOT NULL,
    is_favorite BOOLEAN NOT NULL DEFAULT TRUE,
    last_synced_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (instance_id, foreign_id)
);

CREATE INDEX idx_adult_performer_links_favorite ON adult_performer_links(favorite_id);

CREATE TABLE adult_favorite_studios (
    id UUID PRIMARY KEY,
    canonical_name VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE adult_studio_links (
    favorite_id UUID NOT NULL REFERENCES adult_favorite_studios(id) ON DELETE CASCADE,
    instance_id UUID NOT NULL REFERENCES adult_stashbox_instances(id) ON DELETE CASCADE,
    foreign_id VARCHAR(64) NOT NULL,
    is_favorite BOOLEAN NOT NULL DEFAULT TRUE,
    last_synced_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (instance_id, foreign_id)
);

CREATE INDEX idx_adult_studio_links_favorite ON adult_studio_links(favorite_id);

CREATE TABLE adult_image_sets (
    id UUID PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    directory_path VARCHAR(1024) NOT NULL UNIQUE,
    whisparr_scene_foreign_id VARCHAR(64),
    performer_id UUID REFERENCES adult_favorite_performers(id) ON DELETE SET NULL,
    item_count INT NOT NULL DEFAULT 0 CHECK (item_count >= 0),
    total_size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (total_size_bytes >= 0),
    cover_image_path VARCHAR(1024),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE adult_image_items (
    id UUID PRIMARY KEY,
    image_set_id UUID NOT NULL REFERENCES adult_image_sets(id) ON DELETE CASCADE,
    file_path VARCHAR(1024) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    file_size_bytes BIGINT NOT NULL CHECK (file_size_bytes >= 0),
    mime_type VARCHAR(64) NOT NULL,
    width INT,
    height INT,
    sha256_checksum CHAR(64) NOT NULL CHECK (sha256_checksum ~ '^[0-9a-f]{64}$'),
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_adult_image_items_set ON adult_image_items(image_set_id, sort_order);
