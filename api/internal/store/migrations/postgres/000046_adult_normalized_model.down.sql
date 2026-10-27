-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

DROP TRIGGER IF EXISTS trg_adult_studios_delete_refs ON adult_studios;
DROP TRIGGER IF EXISTS trg_adult_performers_delete_refs ON adult_performers;
DROP FUNCTION IF EXISTS delete_adult_external_refs_on_entity_delete();

ALTER TABLE adult_studios
    DROP CONSTRAINT IF EXISTS chk_adult_studios_not_self_network,
    DROP CONSTRAINT IF EXISTS chk_adult_studios_not_self_parent,
    DROP CONSTRAINT IF EXISTS chk_adult_studios_not_self_merged,
    DROP COLUMN network_studio_id,
    DROP COLUMN parent_studio_id,
    DROP COLUMN merged_into_id,
    DROP COLUMN is_favorite;

ALTER TABLE adult_studios RENAME TO adult_favorite_studios;

ALTER TABLE adult_performers
    DROP CONSTRAINT IF EXISTS chk_adult_performers_not_self_merged,
    DROP COLUMN merged_into_id,
    DROP COLUMN is_favorite;

ALTER TABLE adult_performers RENAME TO adult_favorite_performers;

CREATE TABLE adult_performer_links (
    favorite_id UUID NOT NULL REFERENCES adult_favorite_performers(id) ON DELETE CASCADE,
    instance_id UUID NOT NULL REFERENCES adult_stashbox_instances(id) ON DELETE CASCADE,
    foreign_id VARCHAR(64) NOT NULL,
    is_favorite BOOLEAN NOT NULL DEFAULT TRUE,
    last_synced_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (instance_id, foreign_id)
);

CREATE INDEX idx_adult_performer_links_favorite ON adult_performer_links(favorite_id);

INSERT INTO adult_performer_links (
    favorite_id, instance_id, foreign_id, is_favorite, last_synced_at
)
SELECT
    r.entity_id, r.source_instance_id, r.foreign_id, r.is_favorite, r.last_seen_at
FROM adult_external_refs r
JOIN adult_favorite_performers p ON p.id = r.entity_id
WHERE r.entity_type = 'performer'
  AND r.source_kind = 'stashbox'
  AND r.source_instance_id IS NOT NULL;

CREATE TABLE adult_studio_links (
    favorite_id UUID NOT NULL REFERENCES adult_favorite_studios(id) ON DELETE CASCADE,
    instance_id UUID NOT NULL REFERENCES adult_stashbox_instances(id) ON DELETE CASCADE,
    foreign_id VARCHAR(64) NOT NULL,
    is_favorite BOOLEAN NOT NULL DEFAULT TRUE,
    last_synced_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (instance_id, foreign_id)
);

CREATE INDEX idx_adult_studio_links_favorite ON adult_studio_links(favorite_id);

INSERT INTO adult_studio_links (
    favorite_id, instance_id, foreign_id, is_favorite, last_synced_at
)
SELECT
    r.entity_id, r.source_instance_id, r.foreign_id, r.is_favorite, r.last_seen_at
FROM adult_external_refs r
JOIN adult_favorite_studios s ON s.id = r.entity_id
WHERE r.entity_type = 'studio'
  AND r.source_kind = 'stashbox'
  AND r.source_instance_id IS NOT NULL;

DROP TABLE adult_external_refs;
