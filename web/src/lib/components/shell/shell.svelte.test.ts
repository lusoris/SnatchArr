// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen, waitFor } from '@testing-library/svelte';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { installDialogPolyfill, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import { getNavItems } from './nav.js';
import NavDrawer from './NavDrawer.svelte';
import Sidebar from './Sidebar.svelte';

beforeAll(() => {
	installDialogPolyfill();
});

vi.mock('$app/paths', () => ({
	resolve: (path: string) => (path.startsWith('/') ? path : `/${path}`)
}));

vi.mock('$app/state', () => ({
	page: { url: new URL('http://localhost/') }
}));

const navItems = getNavItems();
const prodStatus = {
	name: 'SnatchArr',
	version: '0.1.0',
	commit: 'abcdef1234567890',
	build_date: '2026-10-23T10:00:00Z',
	profile: 'prod' as const,
	setup_complete: true
};

function lastNavLink(container: HTMLElement): HTMLAnchorElement | undefined {
	const links = Array.from(container.querySelectorAll('nav a'));
	return links.at(-1) as HTMLAnchorElement | undefined;
}

function expectKofi(link: HTMLAnchorElement | undefined): void {
	expect(link?.getAttribute('href')).toBe('https://ko-fi.com/lusoris');
	expect(link?.getAttribute('target')).toBe('_blank');
	expect(link?.getAttribute('rel')).toBe('noopener noreferrer');
	expect(link?.getAttribute('aria-label')).toBe(m.nav_kofi_aria());
}

describe('Sidebar and NavDrawer shell components', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('positive: Ko-fi is the last link in Sidebar nav with target and rel set', async () => {
		stubGet({ '/system/status': prodStatus });

		const { container } = renderWithQuery(Sidebar, {
			navItems,
			onLogout: () => {}
		});

		expectKofi(lastNavLink(container));
	});

	it('positive: Ko-fi is the last link in NavDrawer nav with target and rel set', async () => {
		stubGet({ '/system/status': prodStatus });

		const { container } = renderWithQuery(NavDrawer, {
			open: true,
			onClose: () => {},
			onLogout: () => {},
			menuButtonEl: null
		});

		expectKofi(lastNavLink(container));
	});

	function renderSidebar(): ReturnType<typeof renderWithQuery> {
		return renderWithQuery(Sidebar, {
			navItems,
			onLogout: () => {}
		});
	}

	async function expectVersionBadge(expected: string): Promise<void> {
		renderSidebar();
		await waitFor(() => {
			const badge = screen.getByTestId('shell-version');
			expect(badge.textContent?.trim()).toBe(expected);
		});
	}

	it('positive: renders version label with short commit in sidebar footer', async () => {
		stubGet({ '/system/status': prodStatus });
		await expectVersionBadge('v0.1.0 (abcdef1)');
	});

	it('negative: renders no version label on query error', async () => {
		stubGet({
			'/system/status': new Error('network down')
		});

		renderSidebar();

		// Wait a tick for query resolution
		await new Promise((r) => setTimeout(r, 50));
		expect(screen.queryByTestId('shell-version')).toBeNull();
	});

	it('boundary: empty commit renders dev version as-is without commit', async () => {
		stubGet({
			'/system/status': {
				name: 'SnatchArr',
				version: 'dev',
				commit: '',
				build_date: '',
				profile: 'dev',
				setup_complete: true
			}
		});

		await expectVersionBadge('dev');
	});
});
