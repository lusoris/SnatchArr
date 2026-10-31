// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { instanceKindLabel, probeMessage, productName } from './products.js';

describe('productName', () => {
	it.each([
		['readarr', 'Readarr'],
		['bookshelf', 'Bookshelf'],
		['chaptarr', 'Chaptarr'],
		['readarr_faustvii', 'Readarr (Faustvii fork)'],
		['readarr_rresurrected', 'Readarr (rresurrected)'],
		['readarr_fork', 'Readarr fork (unidentified)']
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

describe('instanceKindLabel', () => {
	it.each([
		['readarr', 'bookshelf', 'Readarr · Bookshelf'],
		['readarr', 'chaptarr', 'Readarr · Chaptarr'],
		['readarr', 'readarr_faustvii', 'Readarr · Readarr (Faustvii fork)'],
		['readarr', 'readarr_rresurrected', 'Readarr · Readarr (rresurrected)'],
		['readarr', 'readarr_fork', 'Readarr · Readarr fork (unidentified)']
	])('names the fork behind %s: %s', (kind, product, label) => {
		expect(instanceKindLabel(kind, product)).toBe(label);
	});

	it('keeps the legacy label for Readarr itself', () => {
		expect(instanceKindLabel('readarr', 'readarr')).toBe('Readarr (legacy)');
	});

	it('keeps the label of the kind while no test has named a product', () => {
		expect(instanceKindLabel('readarr', undefined)).toBe('Readarr (legacy)');
		expect(instanceKindLabel('sonarr', undefined)).toBe('Sonarr');
	});

	it.each(['unknown', 'a_later_product', ''])('does not call "%s" Readarr', (product) => {
		expect(instanceKindLabel('readarr', product)).toBe('Readarr · unknown product');
	});
});
