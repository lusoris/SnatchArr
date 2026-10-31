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
	'/system/modules': [],
	'/settings/runtime': [],
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

	it('renders environment tab when tab=environment', async () => {
		pageMock.url = new URL('http://localhost/system?tab=environment');
		renderWithQuery(SystemPage);

		expect(
			await screen.findByRole('tab', { name: m.system_tab_environment(), selected: true })
		).toBeTruthy();
		expect(screen.getByRole('heading', { level: 2, name: m.environment_title() })).toBeTruthy();
	});

	it('renders modules tab when tab=modules', async () => {
		pageMock.url = new URL('http://localhost/system?tab=modules');
		renderWithQuery(SystemPage);

		expect(
			await screen.findByRole('tab', { name: m.system_tab_modules(), selected: true })
		).toBeTruthy();
		expect(screen.getByRole('heading', { level: 2, name: m.modules_title() })).toBeTruthy();
	});

	it('renders notifications tab when tab=notifications', async () => {
		pageMock.url = new URL('http://localhost/system?tab=notifications');
		renderWithQuery(SystemPage);

		expect(
			await screen.findByRole('tab', { name: m.system_tab_notifications(), selected: true })
		).toBeTruthy();
		expect(screen.getByRole('heading', { level: 2, name: m.notifications_title() })).toBeTruthy();
	});

	it('renders about tab when tab=about', async () => {
		pageMock.url = new URL('http://localhost/system?tab=about');
		renderWithQuery(SystemPage);

		expect(
			await screen.findByRole('tab', { name: m.system_tab_about(), selected: true })
		).toBeTruthy();
		expect(screen.getByRole('heading', { level: 2, name: m.about_app_heading() })).toBeTruthy();
	});

	it('falls back to general tab when tab is unknown', async () => {
		pageMock.url = new URL('http://localhost/system?tab=unknown');
		renderWithQuery(SystemPage);

		expect(
			await screen.findByRole('tab', { name: m.system_tab_general(), selected: true })
		).toBeTruthy();
		expect(screen.getByRole('heading', { level: 2, name: m.settings_title() })).toBeTruthy();
	});

	it('clicking tab calls goto without replaceState for back button history', async () => {
		renderWithQuery(SystemPage);

		const modulesTab = screen.getByRole('tab', { name: m.system_tab_modules() });
		await fireEvent.click(modulesTab);

		expect(navMocks.goto).toHaveBeenCalledWith('/system?tab=modules');
	});
});
