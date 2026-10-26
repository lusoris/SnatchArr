-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
-- SPDX-License-Identifier: EUPL-1.2

CREATE TABLE user_identities (
    provider TEXT NOT NULL CHECK (provider IN ('jellyfin', 'proxy')),
    subject TEXT NOT NULL CHECK (subject <> ''),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (provider, subject)
);

CREATE UNIQUE INDEX user_identities_user_provider_idx ON user_identities (user_id, provider);
