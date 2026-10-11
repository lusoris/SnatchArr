// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { probeMessage, productName } from './products.js';

describe('productName', () => {
	it.each([
		['readarr', 'Readarr'],
		['bookshelf', 'Bookshelf'],
		['chaptarr', 'Chaptarr'],
		['readarr_faustvii', 'Readarr (Faustvii fork)']
	])('names %s', (product, name) => {
		expect(productName(product)).toBe(name);
	});

	it.each(['unknown', '', 'sonarr', 'toString', 'Bookshelf'])('has no name for "%s"', (product) => {
		expect(productName(product)).toBeUndefined();
	});
});

describe('probeMessage', () => {
	it('names the application for a kind that one product serves', () => {
		expect(probeMessage({ app_name: 'Sonarr', version: '4.0.10.2544' })).toBe(
			'Sonarr 4.0.10.2544 answered'
		);
	});

	it('names the product where the application reports another name', () => {
		expect(probeMessage({ app_name: 'Readarr', version: '0.4.21.182', product: 'bookshelf' })).toBe(
			'Bookshelf 0.4.21.182 answered'
		);
		expect(
			probeMessage({ app_name: 'Readarr', version: '0.10.0.0', product: 'readarr_faustvii' })
		).toBe('Readarr (Faustvii fork) 0.10.0.0 answered');
	});

	it('says so when the probe could not tell what answered', () => {
		expect(probeMessage({ app_name: 'Readarr', version: '0.5.0.0', product: 'unknown' })).toBe(
			'Readarr 0.5.0.0 answered. It is neither Readarr nor a fork SnatchArr knows.'
		);
	});

	it('survives an answer without a body', () => {
		expect(probeMessage(undefined)).toBe('  answered');
	});
});
