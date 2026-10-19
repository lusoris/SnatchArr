-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Configarr download client provisioning.

ALTER TABLE download_clients DROP CONSTRAINT download_clients_source_check;
ALTER TABLE download_clients ADD CONSTRAINT download_clients_source_check CHECK (source IN ('manual', 'discovered', 'configarr'));

ALTER TABLE download_clients ADD COLUMN configarr_key TEXT;
CREATE UNIQUE INDEX download_clients_configarr_key_idx ON download_clients (configarr_key) WHERE configarr_key IS NOT NULL;
