// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import '#lib/testing/sse.js';
import ActivityPage from './+page.svelte';

const navMocks = vi.hoisted(() => ({ goto: vi.fn() }));
vi.mock('$app/navigation', () => ({ goto: navMocks.goto }));

const pageMock = vi.hoisted(() => ({
	url: new URL('http://localhost/activity')
}));
vi.mock('$app/state', () => ({
	page: pageMock
}));

const baseStubs = {
	'/events': [],
	'/instances': [],
	'/schedules': [],
	'/system/status': {
		name: 'SnatchArr',
		version: '0.1.0',
		commit: 'abc1234',
		build_date: '2026-10-20',
		profile: 'dev' as const,
		setup_complete: true
	},
	'/logs': { capacity: 100, dropped: 0, records: [] }
};

describe('activity page tabs', () => {
	beforeEach(() => {
		pageMock.url = new URL('http://localhost/activity');
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		stubGet(baseStubs);
	});

	afterEach(() => {
		session.user = null;
		vi.restoreAllMocks();
	});

	it('renders history tab by default', async () => {
		renderWithQuery(ActivityPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.nav_activity() })).toBeTruthy();
		expect(
			screen.getByRole('tab', { name: m.activity_tab_history(), selected: true })
		).toBeTruthy();
		expect(screen.getByRole('grid', { name: m.history_title() })).toBeTruthy();
	});

	it('renders live tab when source=live', async () => {
		pageMock.url = new URL('http://localhost/activity?source=live');
		renderWithQuery(ActivityPage);

		expect(
			await screen.findByRole('tab', { name: m.activity_tab_live(), selected: true })
		).toBeTruthy();
	});

	it('renders log tab for admin when source=log', async () => {
		pageMock.url = new URL('http://localhost/activity?source=log');
		renderWithQuery(ActivityPage);

		expect(
			await screen.findByRole('tab', { name: m.activity_tab_log(), selected: true })
		).toBeTruthy();
	});

	it('omits log tab and falls back to history for viewer even if source=log', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		pageMock.url = new URL('http://localhost/activity?source=log');
		renderWithQuery(ActivityPage);

		expect(screen.queryByRole('tab', { name: m.activity_tab_log() })).toBeNull();
		expect(
			screen.getByRole('tab', { name: m.activity_tab_history(), selected: true })
		).toBeTruthy();
	});

	it('clicking a tab updates query and calls goto', async () => {
		renderWithQuery(ActivityPage);

		const liveTab = screen.getByRole('tab', { name: m.activity_tab_live() });
		await fireEvent.click(liveTab);

		expect(navMocks.goto).toHaveBeenCalledWith('/activity?source=live', { replaceState: true });
	});
});
