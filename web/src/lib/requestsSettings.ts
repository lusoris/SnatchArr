// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { z } from 'zod';
import type { components } from './api/schema.js';

export type RequestsSettings = components['schemas']['RequestsSettings'];
export type RequestsVotingMode = components['schemas']['RequestsVotingMode'];
export type RequestsVisibility = components['schemas']['RequestsVisibility'];

export const requestsSettingsSchema = z.object({
	voting_mode: z.enum(['upvotes_only', 'net_score']),
	allow_viewer_votes: z.boolean(),
	visibility: z.enum(['public_attributed', 'public_anonymous', 'private_to_requester_and_admin']),
	voting_ttl_days: z.number().int().min(0)
}) satisfies z.ZodType<RequestsSettings>;
