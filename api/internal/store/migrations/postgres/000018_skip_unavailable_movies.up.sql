-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2
--
-- Radarr minimum availability (#350): skip movies Radarr reports as not yet available.

ALTER TABLE snatch_policies
    ADD COLUMN skip_unavailable_movies BOOLEAN NOT NULL DEFAULT TRUE;
