// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { m } from '#lib/paraglide/messages.js';
import BottomBar from './BottomBar.svelte';

const pageMock = vi.hoisted(() => ({
	url: new URL('http://localhost/')
}));

vi.mock('$app/state', () => ({
	page: pageMock
}));

vi.mock('$app/paths', () => ({
	resolve: (path: string) => path
}));

describe('BottomBar', () => {
	afterEach(() => {
		vi.restoreAllMocks();
		pageMock.url = new URL('http://localhost/');
	});

	it('positive: renders exactly 4 items (3 destinations + More)', () => {
		const { container } = render(BottomBar, { props: { onOpenDrawer: vi.fn() } });
		const links = container.querySelectorAll('nav a');
		const buttons = container.querySelectorAll('nav button');
		expect(links.length).toBe(3);
		expect(buttons.length).toBe(1);
	});

	it('positive: active item matches current route and receives aria-current="page"', async () => {
		pageMock.url = new URL('http://localhost/instances');
		const { container } = render(BottomBar, { props: { onOpenDrawer: vi.fn() } });

		const instancesLink = screen.getByRole('link', { name: new RegExp(m.nav_instances(), 'i') });
		expect(instancesLink.getAttribute('aria-current')).toBe('page');
		expect(instancesLink.className).toContain('text-brand');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('negative: inactive items omit aria-current attribute', () => {
		pageMock.url = new URL('http://localhost/instances');
		render(BottomBar, { props: { onOpenDrawer: vi.fn() } });

		const activityLink = screen.getByRole('link', { name: new RegExp(m.nav_activity(), 'i') });
		expect(activityLink.getAttribute('aria-current')).toBeNull();
		expect(activityLink.className).toContain('text-subtle');
	});

	it('positive: clicking More button opens drawer via onOpenDrawer callback', async () => {
		const onOpenDrawer = vi.fn();
		render(BottomBar, { props: { onOpenDrawer } });

		const moreButton = screen.getByRole('button', { name: m.nav_bottom_more() });
		expect(moreButton).toBeTruthy();

		await fireEvent.click(moreButton);
		expect(onOpenDrawer).toHaveBeenCalledTimes(1);
	});

	it('negative: clicking a nav link does not trigger onOpenDrawer', async () => {
		const onOpenDrawer = vi.fn();
		render(BottomBar, { props: { onOpenDrawer } });

		const overviewLink = screen.getByRole('link', { name: new RegExp(m.nav_overview(), 'i') });
		await fireEvent.click(overviewLink);
		expect(onOpenDrawer).not.toHaveBeenCalled();
	});

	it('positive: hidden on md+ breakpoints via md:hidden class', () => {
		const { container } = render(BottomBar, { props: { onOpenDrawer: vi.fn() } });
		const nav = container.querySelector('nav');
		expect(nav?.className).toContain('md:hidden');
	});

	it('boundary: root route / matches exactly and is not active on /instances', () => {
		pageMock.url = new URL('http://localhost/instances');
		render(BottomBar, { props: { onOpenDrawer: vi.fn() } });

		const overviewLink = screen.getByRole('link', { name: new RegExp(m.nav_overview(), 'i') });
		expect(overviewLink.getAttribute('aria-current')).toBeNull();

		pageMock.url = new URL('http://localhost/');
		const { container: rootContainer } = render(BottomBar, { props: { onOpenDrawer: vi.fn() } });
		const activeDash = rootContainer.querySelector('a[href="/"]');
		expect(activeDash?.getAttribute('aria-current')).toBe('page');
	});
});
