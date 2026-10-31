// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import type { NavItem } from './nav.js';
import NavList from './NavList.svelte';

let currentPathname = '/';

vi.mock('$app/state', () => ({
	get page() {
		return { url: new URL(`http://localhost${currentPathname}`) };
	}
}));

vi.mock('$app/paths', () => ({
	resolve: (target: string) => target
}));

const testNavEntries: readonly NavItem[] = [
	{ href: '/system', label: 'System Settings', icon: 'settings' },
	{ href: '/account', label: 'User Account', icon: 'user' }
];

describe('NavList', () => {
	afterEach(() => {
		vi.restoreAllMocks();
		currentPathname = '/';
	});

	it('positive: renders nav item list and passes WCAG 2.2 AA', async () => {
		const { container } = render(NavList, {
			props: { items: testNavEntries }
		});

		expect(screen.getByRole('link', { name: /System Settings/i })).toBeTruthy();
		expect(screen.getByRole('link', { name: /User Account/i })).toBeTruthy();

		const { violations } = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(violations).toHaveLength(0);
	});

	it('positive: marks active route with aria-current="page" and invokes onItemClick callback', async () => {
		currentPathname = '/system';
		const handleClick = vi.fn();
		render(NavList, {
			props: { items: testNavEntries, onItemClick: handleClick }
		});

		const activeLink = screen.getByRole('link', { name: /System Settings/i });
		expect(activeLink.getAttribute('aria-current')).toBe('page');

		await fireEvent.click(activeLink);
		expect(handleClick).toHaveBeenCalledTimes(1);
	});

	it('negative: inactive routes omit aria-current attribute and do not require onItemClick', async () => {
		currentPathname = '/system';
		render(NavList, {
			props: { items: testNavEntries }
		});

		const inactiveLink = screen.getByRole('link', { name: /User Account/i });
		expect(inactiveLink.getAttribute('aria-current')).toBeNull();

		await fireEvent.click(inactiveLink);
	});

	it('boundary: renders empty list when items array is empty', () => {
		const { container } = render(NavList, {
			props: { items: [] }
		});

		const list = container.querySelector('ul');
		expect(list).toBeTruthy();
		expect(list?.children.length).toBe(0);
	});

	it('boundary: applies custom gapClass', () => {
		const { container } = render(NavList, {
			props: { items: testNavEntries, gapClass: 'gap-1' }
		});

		const list = container.querySelector('ul');
		expect(list?.className).toContain('gap-1');
	});
});
