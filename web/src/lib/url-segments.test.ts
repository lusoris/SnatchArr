// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { splitTextUrls, splitUrl } from './url-segments.js';

describe('splitUrl', () => {
	it('positive: splits typical service URL into segments at scheme and delimiters', () => {
		const segments = splitUrl('http://sonarr-anime.media.svc.cluster.local:8989');
		expect(segments).toEqual([
			'http://',
			'sonarr-anime',
			'.media',
			'.svc',
			'.cluster',
			'.local',
			':8989'
		]);
		expect(segments.join('')).toBe('http://sonarr-anime.media.svc.cluster.local:8989');
	});

	it('negative: handles empty string and non-URL text as one segment', () => {
		expect(splitUrl('')).toEqual(['']);
		expect(splitUrl('not-a-url')).toEqual(['not-a-url']);
	});

	it('boundary: preserves very long single label without intermediate splits', () => {
		const longLabel = 'a'.repeat(120);
		const segments = splitUrl(`http://${longLabel}`);
		expect(segments).toEqual(['http://', longLabel]);
	});

	it('boundary: keeps IPv6 bracketed address intact before port', () => {
		expect(splitUrl('http://[::1]:8080')).toEqual(['http://', '[::1]', ':8080']);
		expect(splitUrl('[::1]:8080')).toEqual(['[::1]', ':8080']);
		expect(splitUrl('http://[2001:db8::1]:8080/path')).toEqual([
			'http://',
			'[2001:db8::1]',
			':8080',
			'/path'
		]);
	});

	it('boundary: splits query string and fragment before ?, &, and #', () => {
		const url = 'https://example.com/api/v1?filter=missing&sort=desc#bottom';
		const segments = splitUrl(url);
		expect(segments).toEqual([
			'https://',
			'example',
			'.com',
			'/api',
			'/v1',
			'?filter=missing',
			'&sort=desc',
			'#bottom'
		]);
		expect(segments.join('')).toBe(url);
	});
});

describe('splitTextUrls', () => {
	it('positive: separates URLs from surrounding text and stops at quotes', () => {
		expect(splitTextUrls('get "http://h:1/a/rpc": refused')).toEqual([
			{ text: 'get "', url: false },
			{ text: 'http://h:1/a/rpc', url: true },
			{ text: '": refused', url: false }
		]);
	});

	it('negative: text without a URL is one plain part, empty text no parts', () => {
		expect(splitTextUrls('no link here')).toEqual([{ text: 'no link here', url: false }]);
		expect(splitTextUrls('')).toEqual([]);
	});

	it('boundary: URLs at start, end and back to back', () => {
		const parts = splitTextUrls('https://a.b https://c.d');
		expect(parts).toEqual([
			{ text: 'https://a.b', url: true },
			{ text: ' ', url: false },
			{ text: 'https://c.d', url: true }
		]);
		expect(parts.map((p) => p.text).join('')).toBe('https://a.b https://c.d');
	});
});
