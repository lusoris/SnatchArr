// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import SystemPage from './+page.svelte';

const navMocks = vi.hoisted(() => ({ goto: vi.fn() }));
vi.mock('$app/navigation', () => ({ goto: navMocks.goto }));

const pageMock = vi.hoisted(() => ({
	url: new URL('http://localhost/system')
}));
vi.mock('$app/state', () => ({
	page: pageMock
}));

const baseStubs = {
	'/settings': {
		history_retention_days: 30,
		user_agent: 'SnatchArr/1.0',
		global_hourly_cap: 100,
		search_priority_preset: {},
		download_priority_preset: {},
		llm_enabled: false,
		llm_write_tools: []
	},
	'/priority-presets': [],
	'/notifications/channels': [],
	'/notifications/rules': [],
	'/system/status': {
		name: 'SnatchArr',
		version: '0.1.0',
		commit: 'abc1234',
		build_date: '2026-10-20',
		profile: 'dev' as const,
		setup_complete: true
	}
};

describe('system page tabs', () => {
	beforeEach(() => {
		pageMock.url = new URL('http://localhost/system');
		stubGet(baseStubs);
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders general tab by default', async () => {
		renderWithQuery(SystemPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.nav_system() })).toBeTruthy();
		expect(screen.getByRole('tab', { name: m.system_tab_general(), selected: true })).toBeTruthy();
		expect(screen.getByRole('heading', { level: 2, name: m.settings_title() })).toBeTruthy();
	});

	it('renders about tab when tab=about', async () => {
		pageMock.url = new URL('http://localhost/system?tab=about');
		renderWithQuery(SystemPage);

		expect(
			await screen.findByRole('tab', { name: m.system_tab_about(), selected: true })
		).toBeTruthy();
		expect(screen.getByRole('heading', { level: 2, name: m.about_app_heading() })).toBeTruthy();
	});

	it('clicking about tab calls goto with tab=about', async () => {
		renderWithQuery(SystemPage);

		const aboutTab = screen.getByRole('tab', { name: m.system_tab_about() });
		await fireEvent.click(aboutTab);

		expect(navMocks.goto).toHaveBeenCalledWith('/system?tab=about', { replaceState: true });
	});
});
