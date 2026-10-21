-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Add external_id to users for SSO identity mapping per ADR-0023.

ALTER TABLE users ADD COLUMN external_id TEXT NULL UNIQUE;
