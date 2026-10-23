// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { getNavGroups } from './nav.js';
import NavGroups from './NavGroups.svelte';

const testState = vi.hoisted(() => ({
	currentUrl: new URL('http://localhost/')
}));

vi.mock('$app/state', () => ({
	get page() {
		return { url: testState.currentUrl };
	}
}));

vi.mock('$app/paths', () => ({
	resolve: (route: string) => `/app${route}`
}));

const navGroups = getNavGroups(true);

describe('NavGroups', () => {
	afterEach(() => {
		vi.restoreAllMocks();
		testState.currentUrl = new URL('http://localhost/');
	});

	it('positive: renders group labels and active item gets aria-current="page"', async () => {
		testState.currentUrl = new URL('http://localhost/instances');
		const { container } = render(NavGroups, { props: { navGroups } });

		const instancesLink = screen.getByRole('link', { name: /instances/i });
		expect(instancesLink.getAttribute('href')).toBe('/app/instances');
		expect(instancesLink.getAttribute('aria-current')).toBe('page');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('positive: clicking a link invokes onNavigate callback when provided', async () => {
		const onNavigate = vi.fn();
		render(NavGroups, { props: { navGroups, onNavigate } });

		const dashboardLink = screen.getByRole('link', { name: /dashboard/i });
		await fireEvent.click(dashboardLink);
		expect(onNavigate).toHaveBeenCalledTimes(1);
	});

	it('negative: inactive items omit aria-current attribute', () => {
		testState.currentUrl = new URL('http://localhost/instances');
		render(NavGroups, { props: { navGroups } });

		const dashboardLink = screen.getByRole('link', { name: /dashboard/i });
		expect(dashboardLink.getAttribute('aria-current')).toBeNull();
	});

	it('negative: clicking a link does not throw when onNavigate is omitted', async () => {
		render(NavGroups, { props: { navGroups } });

		const dashboardLink = screen.getByRole('link', { name: /dashboard/i });
		await expect(fireEvent.click(dashboardLink)).resolves.toBe(true);
	});

	it('boundary: renders empty structure when navGroups is empty array', () => {
		const { container } = render(NavGroups, { props: { navGroups: [] } });

		expect(screen.queryByRole('link')).toBeNull();
		expect(container.firstElementChild?.children.length).toBe(0);
	});

	it('boundary: applies custom groupLabelClass and itemListClass props', () => {
		const { container } = render(NavGroups, {
			props: {
				navGroups,
				groupLabelClass: 'custom-group-label',
				itemListClass: 'custom-item-list'
			}
		});

		const labelEl = container.querySelector('.custom-group-label');
		expect(labelEl).toBeTruthy();
		const listEl = container.querySelector('.custom-item-list');
		expect(listEl).toBeTruthy();
	});
});
