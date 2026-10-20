// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from '#lib/api/schema.js';

export type ProwlarrIndexerStatus = components['schemas']['ProwlarrIndexerStatus'];

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
