-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

ALTER TABLE settings ALTER COLUMN user_agent SET DEFAULT 'SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)';
UPDATE settings SET user_agent = 'SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)' WHERE user_agent = '';
