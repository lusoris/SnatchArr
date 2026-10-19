-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

DELETE FROM download_clients WHERE source = 'configarr';

DROP INDEX IF EXISTS download_clients_configarr_key_idx;
ALTER TABLE download_clients DROP COLUMN IF EXISTS configarr_key;

ALTER TABLE download_clients DROP CONSTRAINT download_clients_source_check;
ALTER TABLE download_clients ADD CONSTRAINT download_clients_source_check CHECK (source IN ('manual', 'discovered'));
