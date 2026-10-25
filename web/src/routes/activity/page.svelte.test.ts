// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
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

describe('activity page', () => {
	beforeEach(() => {
		pageMock.url = new URL('http://localhost/activity');
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
		stubGet(baseStubs);
	});

	afterEach(() => {
		session.user = null;
		vi.restoreAllMocks();
	});

	it('renders heading and ActivityFeed with events tab active by default', async () => {
		renderWithQuery(ActivityPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.nav_activity() })).toBeTruthy();
		expect(
			screen.getByRole('tab', { name: m.activity_source_events(), selected: true })
		).toBeTruthy();
	});

	it('renders log tab for admin when source=log', async () => {
		pageMock.url = new URL('http://localhost/activity?source=log');
		renderWithQuery(ActivityPage);

		expect(
			await screen.findByRole('tab', { name: m.activity_source_log(), selected: true })
		).toBeTruthy();
	});

	it('omits log tab for viewer even if source=log', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		pageMock.url = new URL('http://localhost/activity?source=log');
		renderWithQuery(ActivityPage);

		expect(screen.queryByRole('tab', { name: m.activity_source_log() })).toBeNull();
	});
});
