// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { legacyTarget } from './legacyRedirect.js';

describe('legacyTarget', () => {
	const tableCases: [string, string][] = [
		['/history', '/activity'],
		['/live', '/activity?source=live'],
		['/logs', '/activity?source=log'],
		['/schedules', '/automation?tab=schedules'],
		['/download-clients', '/connections/download-clients'],
		['/seerr', '/connections/seerr'],
		['/prowlarr', '/connections/prowlarr'],
		['/configarr', '/connections/configarr'],
		['/settings', '/system?tab=general'],
		['/about', '/system?tab=about']
	];

	it.each(tableCases)(
		'positive: maps legacy row %s to target %s with empty query',
		(from, expected) => {
			const result = legacyTarget(from, `http://localhost${from}`);
			expect(result).toBe(expected);
		}
	);

	it('positive: preserves incoming query parameters', () => {
		const result = legacyTarget('/history', 'http://localhost/history?instance=sonarr-1&sort=desc');
		expect(result).toBe('/activity?instance=sonarr-1&sort=desc');
	});

	it('positive: preserves incoming query parameters when target has its own query', () => {
		const result = legacyTarget('/live', 'http://localhost/live?instance=sonarr-1&filter=error');
		expect(result).toBe('/activity?instance=sonarr-1&filter=error&source=live');
	});

	it('negative: target query parameter wins over incoming duplicate', () => {
		// Target has source=live; incoming specifies source=history
		const result = legacyTarget('/live', 'http://localhost/live?source=history&instance=sonarr-1');
		expect(result).toBe('/activity?source=live&instance=sonarr-1');
	});

	it('negative: target tab parameter wins over incoming duplicate on settings', () => {
		// Target has tab=general; incoming specifies tab=about
		const result = legacyTarget('/settings', 'http://localhost/settings?tab=about&extra=1');
		expect(result).toBe('/system?tab=general&extra=1');
	});

	it('boundary: empty query returns clean target without question mark', () => {
		expect(legacyTarget('/history', 'http://localhost/history')).toBe('/activity');
		expect(legacyTarget('/download-clients', 'http://localhost/download-clients')).toBe(
			'/connections/download-clients'
		);
	});

	it('boundary: trailing slash in from path is normalized', () => {
		expect(legacyTarget('/history/', 'http://localhost/history/')).toBe('/activity');
		expect(legacyTarget('/live/', 'http://localhost/live/?foo=bar')).toBe(
			'/activity?foo=bar&source=live'
		);
	});

	it('boundary: incoming URL with trailing empty param', () => {
		const result = legacyTarget('/history', 'http://localhost/history?param1=&param2=val');
		expect(result).toBe('/activity?param1=&param2=val');
	});

	it('boundary: unknown route returns itself', () => {
		expect(legacyTarget('/custom', 'http://localhost/custom?a=1')).toBe('/custom?a=1');
	});
});
