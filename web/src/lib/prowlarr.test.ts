// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import {
	countExhausted,
	countHeadroom,
	evaluateLimit,
	isIndexerExhausted,
	isIndexerNearLimit,
	prowlarrLinkInputSchema,
	prowlarrLinkSchema
} from './prowlarr.js';

describe('prowlarrLinkInputSchema', () => {
	it('positive: valid input parses correctly', () => {
		const res = prowlarrLinkInputSchema.safeParse({
			name: 'Main Prowlarr',
			base_url: 'http://prowlarr:9696',
			api_key: 'my-key',
			hourly_cap: 15,
			enabled: true
		});
		expect(res.success).toBe(true);
	});

	it('boundary: cap 1 accepted', () => {
		const res = prowlarrLinkInputSchema.safeParse({
			name: 'Main Prowlarr',
			base_url: 'http://prowlarr:9696',
			hourly_cap: 1,
			enabled: true
		});
		expect(res.success).toBe(true);
	});

	it('boundary: cap 500 accepted', () => {
		const res = prowlarrLinkInputSchema.safeParse({
			name: 'Main Prowlarr',
			base_url: 'http://prowlarr:9696',
			hourly_cap: 500,
			enabled: true
		});
		expect(res.success).toBe(true);
	});

	it('negative: cap 0 rejected', () => {
		const res = prowlarrLinkInputSchema.safeParse({
			name: 'Main Prowlarr',
			base_url: 'http://prowlarr:9696',
			hourly_cap: 0,
			enabled: true
		});
		expect(res.success).toBe(false);
	});

	it('negative: cap 501 rejected', () => {
		const res = prowlarrLinkInputSchema.safeParse({
			name: 'Main Prowlarr',
			base_url: 'http://prowlarr:9696',
			hourly_cap: 501,
			enabled: true
		});
		expect(res.success).toBe(false);
	});

	it('negative: non-integer cap rejected', () => {
		const res = prowlarrLinkInputSchema.safeParse({
			name: 'Main Prowlarr',
			base_url: 'http://prowlarr:9696',
			hourly_cap: 15.5,
			enabled: true
		});
		expect(res.success).toBe(false);
	});
});

describe('prowlarrLinkSchema', () => {
	it('positive: full stored link parses', () => {
		const res = prowlarrLinkSchema.safeParse({
			id: '00000000-0000-0000-0000-000000000001',
			name: 'Stored Prowlarr',
			base_url: 'http://prowlarr:9696',
			hourly_cap: 15,
			enabled: true,
			created_at: '2026-10-26T12:00:00Z',
			updated_at: '2026-10-26T12:00:00Z'
		});
		expect(res.success).toBe(true);
	});
});

describe('indexer limit evaluations', () => {
	it('evaluates limits and exhaustion levels', () => {
		expect(evaluateLimit(10, 100).level).toBe('ok');
		expect(evaluateLimit(80, 100).level).toBe('near');
		expect(evaluateLimit(100, 100).level).toBe('exhausted');
		expect(evaluateLimit(10, null).hasLimit).toBe(false);
	});

	it('computes indexer exhausted state', () => {
		expect(
			isIndexerExhausted({
				id: 1,
				name: 'i',
				limits_unit: 0,
				used_queries: 0,
				used_grabs: 0,
				exhausted: true
			})
		).toBe(true);
		expect(
			isIndexerNearLimit({
				id: 1,
				name: 'i',
				limits_unit: 0,
				used_queries: 85,
				query_limit: 100,
				used_grabs: 0,
				exhausted: false
			})
		).toBe(true);
		const indexers = [
			{ id: 1, name: 'i', limits_unit: 0, used_queries: 0, used_grabs: 0, exhausted: true },
			{ id: 2, name: 'j', limits_unit: 0, used_queries: 0, used_grabs: 0, exhausted: false }
		];
		expect(countExhausted(indexers)).toBe(1);
		expect(countHeadroom(indexers)).toBe(1);
	});
});
