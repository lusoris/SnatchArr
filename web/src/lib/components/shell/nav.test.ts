// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import {
	filterByModule,
	getNavItems,
	isRouteActive,
	isRouteDisabled,
	moduleForRoute,
	MODULE_ROUTES
} from './nav.js';

describe('nav model', () => {
	const expectedOrder = ['/', '/activity', '/instances', '/automation', '/connections', '/system'];

	it('positive: returns exactly 6 primary nav items in ADR-0029 order by default', () => {
		const items = getNavItems();
		expect(items).toHaveLength(6);
		expect(items.map((i) => i.href)).toEqual(expectedOrder);
	});

	it('positive: production getNavItems returns the 6 core entries when adult module is off, and includes adult entries when adult module is active', () => {
		expect(getNavItems(new Set()).map((i) => i.href)).toEqual(expectedOrder);
		expect(getNavItems(new Set(['fixture_mod'])).map((i) => i.href)).toEqual(expectedOrder);
		expect(getNavItems(new Set(['unknown_mod', 'fixture_mod'])).map((i) => i.href)).toEqual(
			expectedOrder
		);
		expect(getNavItems(new Set(['adult'])).map((i) => i.href)).toEqual([
			'/',
			'/activity',
			'/instances',
			'/automation',
			'/connections',
			'/adult/sources',
			'/adult/performers',
			'/adult/studios',
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

	describe('filterByModule', () => {
		interface FixtureItem {
			id: string;
			module?: string;
		}

		const fixtureItems: readonly FixtureItem[] = [
			{ id: 'core-overview' },
			{ id: 'core-system' },
			{ id: 'mod-item', module: 'fixture_mod' },
			{ id: 'other-mod-item', module: 'other_mod' }
		];

		it('positive: shows items whose module is active', () => {
			const res = filterByModule(fixtureItems, new Set(['fixture_mod']));
			expect(res.map((i) => i.id)).toEqual(['core-overview', 'core-system', 'mod-item']);
		});

		it('negative: drops items whose module is inactive', () => {
			const res = filterByModule(fixtureItems, new Set());
			expect(res.map((i) => i.id)).toEqual(['core-overview', 'core-system']);
			expect(res.map((i) => i.id)).not.toContain('mod-item');
			expect(res.map((i) => i.id)).not.toContain('other-mod-item');
		});

		it('boundary: empty set retains only non-module items', () => {
			const res = filterByModule(fixtureItems, new Set());
			expect(res).toHaveLength(2);
			for (const item of res) {
				expect(item.module).toBeUndefined();
			}
		});

		it('boundary: unknown module id in active set is ignored for non-matching items', () => {
			const res = filterByModule(fixtureItems, new Set(['unknown_mod']));
			expect(res.map((i) => i.id)).toEqual(['core-overview', 'core-system']);
		});

		it('boundary: items without module are always retained', () => {
			const coreItems = [{ id: '1' }, { id: '2' }, { id: '3' }];
			expect(filterByModule(coreItems, new Set())).toEqual(coreItems);
			expect(filterByModule(coreItems, new Set(['fixture_mod']))).toEqual(coreItems);
		});
	});

	describe('moduleForRoute and isRouteDisabled', () => {
		const fixtureRoutes: Readonly<Record<string, string>> = {
			'/fixture-prefix': 'fixture_mod'
		};

		it('positive: identifies module for route and subpaths using injected map', () => {
			expect(moduleForRoute('/fixture-prefix', fixtureRoutes)).toBe('fixture_mod');
			expect(moduleForRoute('/fixture-prefix/', fixtureRoutes)).toBe('fixture_mod');
			expect(moduleForRoute('/fixture-prefix/detail', fixtureRoutes)).toBe('fixture_mod');
		});

		it('positive: returns undefined for non-module route using injected map', () => {
			expect(moduleForRoute('/', fixtureRoutes)).toBeUndefined();
			expect(moduleForRoute('/system', fixtureRoutes)).toBeUndefined();
			expect(moduleForRoute('/instances', fixtureRoutes)).toBeUndefined();
		});

		it('positive: route is not disabled when module is active', () => {
			const res = isRouteDisabled('/fixture-prefix', new Set(['fixture_mod']), fixtureRoutes);
			expect(res.disabled).toBe(false);
			expect(res.moduleId).toBe('fixture_mod');
		});

		it('negative: route is disabled when module is inactive', () => {
			const res = isRouteDisabled('/fixture-prefix', new Set(), fixtureRoutes);
			expect(res.disabled).toBe(true);
			expect(res.moduleId).toBe('fixture_mod');
		});

		it('boundary: non-module route is never disabled', () => {
			expect(isRouteDisabled('/', new Set(), fixtureRoutes).disabled).toBe(false);
			expect(isRouteDisabled('/system', new Set(), fixtureRoutes).disabled).toBe(false);
			expect(isRouteDisabled('/unknown', new Set(), fixtureRoutes).disabled).toBe(false);
		});

		it('boundary: unknown module in active set does not enable route', () => {
			const res = isRouteDisabled('/fixture-prefix', new Set(['unknown_mod']), fixtureRoutes);
			expect(res.disabled).toBe(true);
			expect(res.moduleId).toBe('fixture_mod');
		});

		it('boundary: production MODULE_ROUTES maps /adult to adult module and leaves unmapped routes enabled', () => {
			expect(MODULE_ROUTES['/adult']).toBe('adult');
			expect(moduleForRoute('/adult/sources')).toBe('adult');
			expect(isRouteDisabled('/adult/sources', new Set()).disabled).toBe(true);
			expect(isRouteDisabled('/adult/sources', new Set(['adult'])).disabled).toBe(false);
			expect(moduleForRoute('/watchaware')).toBeUndefined();
			expect(isRouteDisabled('/watchaware', new Set()).disabled).toBe(false);
		});
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
	});
});
