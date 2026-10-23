// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { m } from '#lib/paraglide/messages.js';
import NavFooter from './NavFooter.svelte';

const testState = vi.hoisted(() => ({
	currentUrl: new URL('http://localhost/')
}));

vi.mock('$app/state', () => ({
	get page() {
		return { url: testState.currentUrl };
	}
}));

vi.mock('$app/paths', () => ({
	resolve: (route: string) => `/app/${route.replace(/^\//, '')}`
}));

vi.mock('#lib/queries/system.js', () => ({
	formatVersionLabel: vi.fn(),
	systemStatusQuery: () => ({
		data: undefined
	})
}));

describe('NavFooter', () => {
	afterEach(() => {
		vi.restoreAllMocks();
		testState.currentUrl = new URL('http://localhost/');
	});

	it('positive: renders select with localeId, account link, logout', async () => {
		const onLogout = vi.fn();
		const { container } = render(NavFooter, {
			props: {
				localeId: 'desktop-lang',
				onLogout
			}
		});

		const select = screen.getByRole('combobox');
		expect(select.getAttribute('id')).toBe('desktop-lang');

		const accountLink = screen.getByRole('link', { name: new RegExp(m.nav_account(), 'i') });
		expect(accountLink.getAttribute('href')).toBe('/app/account');
		expect(accountLink.getAttribute('aria-current')).toBeNull();

		const logoutBtn = screen.getByRole('button', { name: new RegExp(m.nav_logout(), 'i') });
		expect(logoutBtn).toBeTruthy();

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('negative: logout click calls onNavigate before onLogout', async () => {
		const callOrder: string[] = [];
		const onNavigate = vi.fn(() => {
			callOrder.push('navigate');
		});
		const onLogout = vi.fn(() => {
			callOrder.push('logout');
		});

		render(NavFooter, {
			props: {
				localeId: 'mobile-lang',
				onLogout,
				onNavigate
			}
		});

		const logoutBtn = screen.getByRole('button', { name: new RegExp(m.nav_logout(), 'i') });
		await fireEvent.click(logoutBtn);

		expect(onNavigate).toHaveBeenCalledTimes(1);
		expect(onLogout).toHaveBeenCalledTimes(1);
		expect(callOrder).toEqual(['navigate', 'logout']);
	});

	it('negative: clicking logout without onNavigate does not throw', async () => {
		const onLogout = vi.fn();
		render(NavFooter, {
			props: {
				localeId: 'desktop-lang',
				onLogout
			}
		});

		const logoutBtn = screen.getByRole('button', { name: new RegExp(m.nav_logout(), 'i') });
		await expect(fireEvent.click(logoutBtn)).resolves.toBe(true);
		expect(onLogout).toHaveBeenCalledTimes(1);
	});

	it('boundary: account click calls onNavigate once', async () => {
		const onNavigate = vi.fn();
		const onLogout = vi.fn();
		render(NavFooter, {
			props: {
				localeId: 'mobile-lang',
				onLogout,
				onNavigate
			}
		});

		const accountLink = screen.getByRole('link', { name: new RegExp(m.nav_account(), 'i') });
		await fireEvent.click(accountLink);

		expect(onNavigate).toHaveBeenCalledTimes(1);
		expect(onLogout).not.toHaveBeenCalled();
	});

	it('boundary: active route sets aria-current="page" on account link', () => {
		testState.currentUrl = new URL('http://localhost/account');
		render(NavFooter, {
			props: {
				localeId: 'desktop-lang',
				onLogout: vi.fn()
			}
		});

		const accountLink = screen.getByRole('link', { name: new RegExp(m.nav_account(), 'i') });
		expect(accountLink.getAttribute('aria-current')).toBe('page');
	});
});
