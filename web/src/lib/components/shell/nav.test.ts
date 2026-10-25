// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { getNavItems, isRouteActive } from './nav.js';

describe('nav model', () => {
	it('positive: returns exactly 6 primary nav items in ADR-0029 order', () => {
		const items = getNavItems();
		expect(items).toHaveLength(6);
		expect(items.map((i) => i.href)).toEqual([
			'/',
			'/activity',
			'/instances',
			'/automation',
			'/connections',
			'/system'
		]);
	});

	it('positive: every nav item carries its designated icon from ADR-0029 route table', () => {
		const items = getNavItems();
		expect(items.map((i) => i.icon)).toEqual([
			'layout-dashboard',
			'activity',
			'server',
			'clock',
			'link',
			'settings'
		]);
	});

	describe('isRouteActive', () => {
		it('positive: matches exact path', () => {
			expect(isRouteActive('/', '/')).toBe(true);
			expect(isRouteActive('/activity', '/activity')).toBe(true);
			expect(isRouteActive('/instances', '/instances')).toBe(true);
			expect(isRouteActive('/automation', '/automation')).toBe(true);
			expect(isRouteActive('/connections', '/connections')).toBe(true);
			expect(isRouteActive('/system', '/system')).toBe(true);
		});

		it('positive: /connections/* is active on Connections', () => {
			expect(isRouteActive('/connections/download-clients', '/connections')).toBe(true);
			expect(isRouteActive('/connections/seerr', '/connections')).toBe(true);
			expect(isRouteActive('/connections/prowlarr', '/connections')).toBe(true);
			expect(isRouteActive('/connections/configarr', '/connections')).toBe(true);
		});

		it('positive: /system sub-paths active on System', () => {
			expect(isRouteActive('/system', '/system')).toBe(true);
		});

		it('positive: /activity active on Activity', () => {
			expect(isRouteActive('/activity', '/activity')).toBe(true);
		});

		it('negative: does not match distinct path or unrelated prefix', () => {
			expect(isRouteActive('/instances', '/activity')).toBe(false);
			expect(isRouteActive('/instances-custom', '/instances')).toBe(false);
			expect(isRouteActive('/connections', '/system')).toBe(false);
			expect(isRouteActive('/instances', '/')).toBe(false);
			expect(isRouteActive('/activity', '/')).toBe(false);
		});

		it('boundary: active match on nested instance route /instances/1', () => {
			expect(isRouteActive('/instances/1', '/instances')).toBe(true);
			expect(isRouteActive('/instances/1/policy', '/instances')).toBe(true);
			expect(isRouteActive('/instances/1', '/')).toBe(false);
		});
	});
});
