// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { APP_KINDS, ARR_KINDS, isArrKind, isLegacyKind, kindLabel, kindName } from './kinds.js';

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
	it('positive: maps every non-legacy arr kind to its display name', () => {
		expect(kindLabel('sonarr')).toBe('Sonarr');
		expect(kindLabel('radarr')).toBe('Radarr');
		expect(kindLabel('lidarr')).toBe('Lidarr');
		expect(kindLabel('whisparr_v2')).toBe('Whisparr v2');
		expect(kindLabel('whisparr_v3')).toBe('Whisparr v3');
	});

	it('negative: returns unknown kind as given', () => {
		expect(kindLabel('bookshelf')).toBe('bookshelf');
		expect(kindLabel('')).toBe('');
		expect(kindLabel('custom_kind')).toBe('custom_kind');
	});

	it('boundary: marks legacy readarr with legacy suffix and preserves localized Stash', () => {
		expect(kindLabel('readarr')).toBe('Readarr (legacy)');
		expect(kindLabel('stash')).toBe('Stash');
	});
});

describe('kindName', () => {
	it('positive: returns the display name for each known kind', () => {
		expect(kindName('sonarr')).toBe('Sonarr');
		expect(kindName('radarr')).toBe('Radarr');
		expect(kindName('lidarr')).toBe('Lidarr');
		expect(kindName('readarr')).toBe('Readarr');
		expect(kindName('whisparr_v2')).toBe('Whisparr v2');
		expect(kindName('whisparr_v3')).toBe('Whisparr v3');
	});

	it('negative: returns unknown kind as given', () => {
		expect(kindName('bookshelf')).toBe('bookshelf');
		expect(kindName('')).toBe('');
	});

	it('boundary: preserves localized Stash for stash', () => {
		expect(kindName('stash')).toBe('Stash');
	});
});
