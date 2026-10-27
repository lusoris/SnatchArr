// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { APP_KINDS, ARR_KINDS, isArrKind, isLegacyKind, kindLabel } from './kinds.js';

describe('APP_KINDS', () => {
	it('lists every kind once, in the order of the form', () => {
		expect(APP_KINDS).toEqual([
			'sonarr',
			'radarr',
			'lidarr',
			'readarr',
			'whisparr_v2',
			'whisparr_v3',
			'stash'
		]);
	});
});

describe('ARR_KINDS and isArrKind', () => {
	it('includes only the six *arr kinds and excludes stash', () => {
		expect(ARR_KINDS).toEqual([
			'sonarr',
			'radarr',
			'lidarr',
			'readarr',
			'whisparr_v2',
			'whisparr_v3'
		]);
		expect(APP_KINDS.filter(isArrKind)).toEqual(ARR_KINDS);
		expect(isArrKind('stash')).toBe(false);
		expect(isArrKind('plex')).toBe(false);
		expect(isArrKind('')).toBe(false);
	});
});

describe('isLegacyKind', () => {
	it('is true for Readarr alone', () => {
		expect(APP_KINDS.filter(isLegacyKind)).toEqual(['readarr']);
	});

	it('is false for a kind this build does not know', () => {
		expect(isLegacyKind('bookshelf')).toBe(false);
		expect(isLegacyKind('')).toBe(false);
		expect(isLegacyKind('toString')).toBe(false);
	});
});

describe('kindLabel', () => {
	it('marks the legacy kind and localizes Stash', () => {
		expect(kindLabel('readarr')).toBe('readarr (legacy)');
		expect(kindLabel('stash')).toBe('Stash');
	});

	it('shows every other kind as it is', () => {
		for (const kind of APP_KINDS.filter((k) => k !== 'readarr' && k !== 'stash')) {
			expect(kindLabel(kind)).toBe(kind);
		}
		expect(kindLabel('bookshelf')).toBe('bookshelf');
	});
});
