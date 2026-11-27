-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

-- Scoped library processing rules (ADR-0041 section 10, ADR-0050 section 2).
CREATE TABLE library_processing_rules (
    id                 UUID PRIMARY KEY,
    scope              TEXT NOT NULL CHECK (scope IN (
        'item_override',
        'user_language',
        'library',
        'instance',
        'media_type',
        'global_default'
    )),
    instance_id        UUID REFERENCES instances (id) ON DELETE CASCADE,
    item_id            TEXT CHECK (item_id IS NULL OR (char_length(item_id) BETWEEN 1 AND 128 AND btrim(item_id) = item_id)),
    user_language      TEXT CHECK (user_language IS NULL OR char_length(user_language) BETWEEN 2 AND 3),
    user_language_code TEXT CHECK (user_language_code IS NULL OR char_length(user_language_code) = 3),
    root_path          TEXT CHECK (root_path IS NULL OR (octet_length(root_path) BETWEEN 1 AND 4096 AND left(root_path, 1) = '/')),
    media_type         TEXT CHECK (media_type IS NULL OR media_type IN (
        'movie',
        'episode',
        'anime',
        'music',
        'audiobook',
        'stash_scene'
    )),
    rule               JSONB NOT NULL CHECK (
        jsonb_typeof(rule) = 'object'
        AND jsonb_typeof(rule -> 'track') = 'object'
        AND jsonb_typeof(rule -> 'track' -> 'languages') = 'array'
        AND CASE WHEN jsonb_typeof(rule -> 'track' -> 'languages') = 'array' THEN jsonb_array_length(rule -> 'track' -> 'languages') >= 1 ELSE false END
    ),
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    CONSTRAINT library_processing_rules_scope_target_check CHECK (
        (scope = 'item_override'
            AND instance_id IS NOT NULL
            AND item_id IS NOT NULL
            AND user_language IS NULL
            AND user_language_code IS NULL
            AND root_path IS NULL
            AND media_type IS NULL)
        OR (scope = 'user_language'
            AND instance_id IS NULL
            AND item_id IS NULL
            AND user_language IS NOT NULL
            AND user_language_code IS NOT NULL
            AND root_path IS NULL
            AND media_type IS NULL)
        OR (scope = 'library'
            AND instance_id IS NOT NULL
            AND item_id IS NULL
            AND user_language IS NULL
            AND user_language_code IS NULL
            AND root_path IS NOT NULL
            AND media_type IS NULL)
        OR (scope = 'instance'
            AND instance_id IS NOT NULL
            AND item_id IS NULL
            AND user_language IS NULL
            AND user_language_code IS NULL
            AND root_path IS NULL
            AND media_type IS NULL)
        OR (scope = 'media_type'
            AND instance_id IS NULL
            AND item_id IS NULL
            AND user_language IS NULL
            AND user_language_code IS NULL
            AND root_path IS NULL
            AND media_type IS NOT NULL)
        OR (scope = 'global_default'
            AND instance_id IS NULL
            AND item_id IS NULL
            AND user_language IS NULL
            AND user_language_code IS NULL
            AND root_path IS NULL
            AND media_type IS NULL)
    )
);

CREATE UNIQUE INDEX library_processing_rules_item_override_uidx
    ON library_processing_rules (instance_id, item_id)
    WHERE scope = 'item_override';

CREATE UNIQUE INDEX library_processing_rules_user_language_uidx
    ON library_processing_rules (user_language_code)
    WHERE scope = 'user_language';

CREATE UNIQUE INDEX library_processing_rules_library_uidx
    ON library_processing_rules (instance_id, root_path)
    WHERE scope = 'library';

CREATE UNIQUE INDEX library_processing_rules_instance_uidx
    ON library_processing_rules (instance_id)
    WHERE scope = 'instance';

CREATE UNIQUE INDEX library_processing_rules_media_type_uidx
    ON library_processing_rules (media_type)
    WHERE scope = 'media_type';

CREATE UNIQUE INDEX library_processing_rules_global_default_uidx
    ON library_processing_rules (scope)
    WHERE scope = 'global_default';
