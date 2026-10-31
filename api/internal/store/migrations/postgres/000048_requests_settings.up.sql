-- SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
--
-- SPDX-License-Identifier: EUPL-1.2

CREATE TABLE requests_settings (
    singleton          BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    voting_mode        TEXT NOT NULL DEFAULT 'upvotes_only'
        CHECK (voting_mode IN ('upvotes_only', 'net_score')),
    allow_viewer_votes BOOLEAN NOT NULL DEFAULT TRUE,
    visibility         TEXT NOT NULL DEFAULT 'public_attributed'
        CHECK (visibility IN ('public_attributed', 'public_anonymous', 'private_to_requester_and_admin')),
    voting_ttl_days    INTEGER NOT NULL DEFAULT 0
        CHECK (voting_ttl_days >= 0),
    updated_at         TIMESTAMPTZ NOT NULL
);
