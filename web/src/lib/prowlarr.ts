// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { z } from 'zod';
import type { components } from '#lib/api/schema.js';

export type ProwlarrLink = components['schemas']['ProwlarrLink'];
export type ProwlarrLinkInput = components['schemas']['ProwlarrLinkInput'];
export type ProwlarrIndexerStatus = components['schemas']['ProwlarrIndexerStatus'];

export const prowlarrLinkInputSchema = z.object({
	name: z.string().min(1).max(64),
	base_url: z.string().min(1),
	api_key: z.string().optional(),
	hourly_cap: z.number().int().min(1).max(500),
	enabled: z.boolean()
}) satisfies z.ZodType<ProwlarrLinkInput>;

export const prowlarrLinkSchema = z.object({
	id: z.string(),
	name: z.string(),
	base_url: z.string(),
	hourly_cap: z.number().int().min(1).max(500),
	enabled: z.boolean(),
	last_seen_version: z.string().optional(),
	last_check_at: z.string().optional(),
	last_error: z.string().optional(),
	created_at: z.string(),
	updated_at: z.string()
}) satisfies z.ZodType<ProwlarrLink>;

export type LimitLevel = 'ok' | 'near' | 'exhausted';

export interface LimitEvaluation {
	hasLimit: boolean;
	used: number;
	limit: number | null | undefined;
	ratio: number;
	percent: number;
	level: LimitLevel;
}

export function evaluateLimit(used: number, limit?: number | null): LimitEvaluation {
	if (limit === undefined || limit === null || limit <= 0) {
		return {
			hasLimit: false,
			used,
			limit,
			ratio: 0,
			percent: 0,
			level: 'ok'
		};
	}
	const ratio = used / limit;
	const level: LimitLevel = ratio >= 1 ? 'exhausted' : ratio >= 0.8 ? 'near' : 'ok';
	return {
		hasLimit: true,
		used,
		limit,
		ratio,
		percent: Math.round(ratio * 100),
		level
	};
}

export function isIndexerExhausted(idx: ProwlarrIndexerStatus): boolean {
	if (idx.exhausted) return true;
	if (evaluateLimit(idx.used_queries, idx.query_limit).level === 'exhausted') return true;
	if (evaluateLimit(idx.used_grabs, idx.grab_limit).level === 'exhausted') return true;
	return false;
}

export function isIndexerNearLimit(idx: ProwlarrIndexerStatus): boolean {
	return (
		evaluateLimit(idx.used_queries, idx.query_limit).level === 'near' ||
		evaluateLimit(idx.used_grabs, idx.grab_limit).level === 'near'
	);
}

export function countExhausted(indexers: ProwlarrIndexerStatus[]): number {
	return indexers.filter(isIndexerExhausted).length;
}

export function countHeadroom(indexers: ProwlarrIndexerStatus[]): number {
	return indexers.length - countExhausted(indexers);
}
