// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { getNavGroups, isRouteActive } from './nav.js';

describe('nav group model', () => {
	it('positive: includes all groups and admin routes when isAdmin is true', () => {
		const groups = getNavGroups(true);
		expect(groups).toHaveLength(5);

		const fleetGroup = groups.find((g) => g.items.some((i) => i.href === '/'));
		expect(fleetGroup?.items.map((i) => i.href)).toEqual(['/', '/instances']);

		const systemGroup = groups.find((g) => g.items.some((i) => i.href === '/settings'));
		expect(systemGroup?.items.map((i) => i.href)).toEqual(['/settings', '/logs', '/about']);
	});

	it('negative: admin-only entry (/logs) hidden for non-admin', () => {
		const groups = getNavGroups(false);
		const systemGroup = groups.find((g) => g.items.some((i) => i.href === '/settings'));
		expect(systemGroup?.items.map((i) => i.href)).toEqual(['/settings', '/about']);
		expect(groups.some((g) => g.items.some((i) => i.href === '/logs'))).toBe(false);
	});

	it('positive: about route is present for both viewer and admin', () => {
		const viewerGroups = getNavGroups(false);
		const adminGroups = getNavGroups(true);
		expect(viewerGroups.some((g) => g.items.some((i) => i.href === '/about'))).toBe(true);
		expect(adminGroups.some((g) => g.items.some((i) => i.href === '/about'))).toBe(true);
	});

	describe('isRouteActive', () => {
		it('positive: matches exact path', () => {
			expect(isRouteActive('/instances', '/instances')).toBe(true);
			expect(isRouteActive('/history', '/history')).toBe(true);
			expect(isRouteActive('/', '/')).toBe(true);
		});

		it('negative: does not match distinct path or unrelated prefix', () => {
			expect(isRouteActive('/instances', '/history')).toBe(false);
			expect(isRouteActive('/instances-custom', '/instances')).toBe(false);
			expect(isRouteActive('/instances', '/')).toBe(false);
		});

		it('boundary: active match on nested route /instances/1', () => {
			expect(isRouteActive('/instances/1', '/instances')).toBe(true);
			expect(isRouteActive('/instances/1/policy', '/instances')).toBe(true);
			expect(isRouteActive('/instances/1', '/')).toBe(false);
		});
	});
});
