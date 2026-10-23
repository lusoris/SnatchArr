// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet, type Instance } from '#lib/testing/harness.js';
import CommandDialog from './CommandDialog.svelte';
import { getNavGroups } from './nav.js';

const navGroups = getNavGroups(true);

const navMocks = vi.hoisted(() => ({
	goto: vi.fn().mockResolvedValue(undefined)
}));

vi.mock('$app/navigation', () => ({
	goto: navMocks.goto
}));

vi.mock('$app/paths', () => ({
	resolve: (path: string, params?: Record<string, string>) =>
		params?.id ? path.replace('[id]', params.id) : path
}));

const mockInstance: Instance = {
	id: '00000000-0000-0000-0000-000000000001',
	name: 'Sonarr 4K',
	kind: 'sonarr',
	base_url: 'http://127.0.0.1:8989',
	enabled: true,
	source: 'manual',
	created_at: '2026-10-20T00:00:00Z',
	updated_at: '2026-10-20T00:00:00Z'
};

async function openViaKey(key = 'k', modifier: 'ctrlKey' | 'metaKey' = 'ctrlKey'): Promise<void> {
	window.dispatchEvent(new KeyboardEvent('keydown', { key, [modifier]: true, bubbles: true }));
	await vi.waitFor(() => {
		expect(screen.getByRole('combobox')).toBeTruthy();
	});
}

async function closeViaEscape(): Promise<void> {
	window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
	await vi.waitFor(() => {
		expect(screen.queryByRole('combobox')).toBeNull();
	});
}

describe('CommandDialog', () => {
	beforeEach(() => {
		stubGet({
			'/instances': [mockInstance]
		});
	});

	afterEach(() => {
		vi.restoreAllMocks();
		navMocks.goto.mockReset();
	});

	it('positive: opens on Ctrl+K off Apple platforms, Escape closes', async () => {
		const { container } = renderWithQuery(CommandDialog, { navGroups, open: false });

		expect(screen.queryByRole('combobox')).toBeNull();

		await openViaKey();

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);

		await closeViaEscape();
	});

	it('boundary: on Apple platforms Cmd+K opens and Ctrl+K does not', async () => {
		vi.spyOn(navigator, 'platform', 'get').mockReturnValue('MacIntel');
		renderWithQuery(CommandDialog, { navGroups, open: false });

		window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true }));
		expect(screen.queryByRole('combobox')).toBeNull();

		await openViaKey('k', 'metaKey');
	});

	it('negative: entries follow the given nav groups only', async () => {
		renderWithQuery(CommandDialog, { navGroups: getNavGroups(false), open: true });
		const input = await screen.findByRole('combobox');
		const adminOnly = navGroups
			.flatMap((g) => g.items)
			.filter((i) => !getNavGroups(false).some((g) => g.items.some((j) => j.href === i.href)));
		expect(adminOnly.length).toBeGreaterThan(0);
		for (const item of adminOnly) {
			await fireEvent.input(input, { target: { value: item.label } });
			expect(screen.queryByText(new RegExp(`^${item.label} —`))).toBeNull();
		}
	});

	it('negative: does not open on unrelated keys or extra modifiers', async () => {
		renderWithQuery(CommandDialog, { navGroups, open: false });

		// Ctrl+J does not open
		window.dispatchEvent(new KeyboardEvent('keydown', { key: 'j', ctrlKey: true, bubbles: true }));
		expect(screen.queryByRole('combobox')).toBeNull();

		// Ctrl+Alt+K does not open
		window.dispatchEvent(
			new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, altKey: true, bubbles: true })
		);
		expect(screen.queryByRole('combobox')).toBeNull();

		// Plain 'k' does not open
		window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', bubbles: true }));
		expect(screen.queryByRole('combobox')).toBeNull();
	});

	it('boundary: opens with uppercase K (Ctrl+K)', async () => {
		renderWithQuery(CommandDialog, { navGroups, open: false });

		await openViaKey('K');
	});

	it('positive: Escape closes dialog and returns focus to opener element', async () => {
		const opener = document.createElement('button');
		opener.textContent = 'Open Search';
		document.body.appendChild(opener);
		opener.focus();
		expect(document.activeElement).toBe(opener);

		renderWithQuery(CommandDialog, { navGroups, open: false });

		// Open via Ctrl+K while opener is focused
		await openViaKey();

		// Press Escape to close
		await closeViaEscape();

		// Focus should return to opener button
		expect(document.activeElement).toBe(opener);

		opener.remove();
	});

	it('positive: selecting a nav entry executes goto with resolved route', async () => {
		renderWithQuery(CommandDialog, { navGroups, open: true });

		await vi.waitFor(() => {
			expect(screen.getByRole('combobox')).toBeTruthy();
		});

		// Search for Dashboard
		const input = screen.getByRole('combobox');
		await fireEvent.input(input, { target: { value: m.nav_dashboard() } });

		const dashboardOption = await screen.findByText(new RegExp(m.nav_dashboard(), 'i'));
		await fireEvent.click(dashboardOption);

		expect(navMocks.goto).toHaveBeenCalledWith('/');
	});

	it('positive: includes instances in search and navigates to instance detail', async () => {
		renderWithQuery(CommandDialog, { navGroups, open: true });

		await vi.waitFor(() => {
			expect(screen.getByRole('combobox')).toBeTruthy();
		});

		// Wait for instance query data
		const instanceOption = await screen.findByText(/Sonarr 4K/i);
		expect(instanceOption).toBeTruthy();

		await fireEvent.click(instanceOption);
		expect(navMocks.goto).toHaveBeenCalledWith('/instances/00000000-0000-0000-0000-000000000001');
	});

	it('negative: typing an unknown search term shows empty message', async () => {
		renderWithQuery(CommandDialog, { navGroups, open: true });

		const input = await screen.findByRole('combobox');
		await fireEvent.input(input, { target: { value: 'nonexistent-route-xyz' } });

		expect(await screen.findByText(/no commands found/i)).toBeTruthy();
	});
});
