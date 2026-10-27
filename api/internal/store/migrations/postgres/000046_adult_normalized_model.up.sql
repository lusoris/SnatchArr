-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE adult_favorite_performers RENAME TO adult_performers;

ALTER TABLE adult_performers
    ADD COLUMN is_favorite BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN merged_into_id UUID REFERENCES adult_performers(id) ON DELETE SET NULL,
    ADD CONSTRAINT chk_adult_performers_not_self_merged CHECK (merged_into_id <> id);

UPDATE adult_performers p
SET is_favorite = TRUE
WHERE EXISTS (
    SELECT 1 FROM adult_performer_links l
    WHERE l.favorite_id = p.id AND l.is_favorite = TRUE
);

ALTER TABLE adult_favorite_studios RENAME TO adult_studios;

ALTER TABLE adult_studios
    ADD COLUMN is_favorite BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN merged_into_id UUID REFERENCES adult_studios(id) ON DELETE SET NULL,
    ADD COLUMN parent_studio_id UUID REFERENCES adult_studios(id) ON DELETE SET NULL,
    ADD COLUMN network_studio_id UUID REFERENCES adult_studios(id) ON DELETE SET NULL,
    ADD CONSTRAINT chk_adult_studios_not_self_merged CHECK (merged_into_id <> id),
    ADD CONSTRAINT chk_adult_studios_not_self_parent CHECK (parent_studio_id <> id),
    ADD CONSTRAINT chk_adult_studios_not_self_network CHECK (network_studio_id <> id);

UPDATE adult_studios s
SET is_favorite = TRUE
WHERE EXISTS (
    SELECT 1 FROM adult_studio_links l
    WHERE l.favorite_id = s.id AND l.is_favorite = TRUE
);

CREATE TABLE adult_external_refs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type VARCHAR(32) NOT NULL CHECK (entity_type IN ('performer', 'studio', 'scene', 'image_set', 'tag')),
    entity_id UUID NOT NULL,
    source_kind VARCHAR(32) NOT NULL CHECK (source_kind IN ('stashbox', 'tpdb_rest', 'whisparr_v3', 'whisparr_v2', 'stash_local', 'local_fs')),
    source_instance_id UUID REFERENCES adult_stashbox_instances(id) ON DELETE CASCADE,
    foreign_id VARCHAR(64) NOT NULL,
    secondary_id VARCHAR(128),
    canonical_url VARCHAR(1024),
    extra_urls TEXT[] NOT NULL DEFAULT '{}',
    is_favorite BOOLEAN NOT NULL DEFAULT FALSE,
    confidence_score SMALLINT CHECK (confidence_score >= 0 AND confidence_score <= 100),
    match_method VARCHAR(64),
    fetched_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    UNIQUE NULLS NOT DISTINCT (source_kind, source_instance_id, entity_type, foreign_id)
);

CREATE INDEX idx_adult_external_refs_entity ON adult_external_refs(entity_type, entity_id);

INSERT INTO adult_external_refs (
    id, entity_type, entity_id, source_kind, source_instance_id,
    foreign_id, is_favorite, match_method, fetched_at, last_seen_at
)
SELECT
    gen_random_uuid(), 'performer', favorite_id, 'stashbox', instance_id,
    foreign_id, is_favorite, 'migrated', last_synced_at, last_synced_at
FROM adult_performer_links;

INSERT INTO adult_external_refs (
    id, entity_type, entity_id, source_kind, source_instance_id,
    foreign_id, is_favorite, match_method, fetched_at, last_seen_at
)
SELECT
    gen_random_uuid(), 'studio', favorite_id, 'stashbox', instance_id,
    foreign_id, is_favorite, 'migrated', last_synced_at, last_synced_at
FROM adult_studio_links;

DROP TABLE adult_performer_links;
DROP TABLE adult_studio_links;

CREATE FUNCTION delete_adult_external_refs_on_entity_delete() RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM adult_external_refs
    WHERE entity_type = TG_ARGV[0] AND entity_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_adult_performers_delete_refs
    AFTER DELETE ON adult_performers
    FOR EACH ROW EXECUTE FUNCTION delete_adult_external_refs_on_entity_delete('performer');

CREATE TRIGGER trg_adult_studios_delete_refs
    AFTER DELETE ON adult_studios
    FOR EACH ROW EXECUTE FUNCTION delete_adult_external_refs_on_entity_delete('studio');
