// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { APP_KINDS, isLegacyKind, kindLabel } from './kinds.js';

describe('APP_KINDS', () => {
	it('lists every kind once, in the order of the form', () => {
		expect(APP_KINDS).toEqual([
			'sonarr',
			'radarr',
			'lidarr',
			'readarr',
			'whisparr_v2',
			'whisparr_v3'
		]);
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
	it('marks the legacy kind', () => {
		expect(kindLabel('readarr')).toBe('readarr (legacy)');
	});

	it('shows every other kind as it is', () => {
		for (const kind of APP_KINDS.filter((k) => k !== 'readarr')) {
			expect(kindLabel(kind)).toBe(kind);
		}
		expect(kindLabel('bookshelf')).toBe('bookshelf');
	});
});
