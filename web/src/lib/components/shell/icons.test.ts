// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { shellChromeIcons, shellIconLoader } from './icons.js';
import { getNavGroups } from './nav.js';

describe('shell icon loader', () => {
	it('positive: every nav entry icon resolves to a bundled component', () => {
		const names = getNavGroups(true).flatMap((g) => g.items.map((i) => i.icon));
		const missing = names.filter((n) => shellIconLoader(n) === undefined);
		expect(missing).toEqual([]);
	});

	it('positive: every shell chrome icon resolves', () => {
		const missing = shellChromeIcons.filter((n) => shellIconLoader(n) === undefined);
		expect(missing).toEqual([]);
	});

	it('negative: an unmapped name returns undefined, never a promise', () => {
		expect(shellIconLoader('not-an-icon')).toBeUndefined();
	});

	it('boundary: kebab and Pascal spellings resolve to the same component', () => {
		expect(shellIconLoader('layout-dashboard')).toBe(shellIconLoader('LayoutDashboard'));
	});
});
