-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE adult_image_items ADD COLUMN file_mtime TIMESTAMPTZ;
CREATE UNIQUE INDEX idx_adult_image_items_set_path ON adult_image_items(image_set_id, file_path);
