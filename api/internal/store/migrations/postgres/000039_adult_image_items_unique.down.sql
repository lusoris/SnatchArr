-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

DROP INDEX IF EXISTS idx_adult_image_items_set_path;
ALTER TABLE adult_image_items DROP COLUMN IF EXISTS file_mtime;
