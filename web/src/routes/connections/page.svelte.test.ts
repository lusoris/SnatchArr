// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen, waitFor } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import ConnectionsHubPage from './+page.svelte';

vi.mock('$app/paths', () => ({
	resolve: (path: string) => path
}));

function signIn(role: 'admin' | 'viewer') {
	session.user = { id: 'u-1', username: role, role } as never;
}

describe('connections hub page', () => {
	afterEach(() => {
		session.user = null;
		vi.restoreAllMocks();
	});

	it('positive: admin sees all 4 cards with status, counts, and passes WCAG 2.2 AA', async () => {
		signIn('admin');
		stubGet({
			'/download-clients': [
				{
					id: 'c1',
					name: 'qBittorrent',
					kind: 'qbittorrent',
					protocol: 'torrent',
					base_url: 'http://qbit:8080',
					username: 'admin',
					enabled: true,
					source: 'manual',
					max_active: 0,
					max_queued: 0,
					bandwidth_budget_bps: 0,
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z'
				}
			],
			'/download-clients/status': [
				{
					client_id: 'c1',
					name: 'qBittorrent',
					kind: 'qbittorrent',
					reachable: true,
					paused: false,
					active: 1,
					queued: 0,
					download_rate_bps: 1024,
					upload_rate_bps: 0,
					checked_at: '2026-10-25T05:00:00Z'
				}
			],
			'/seerr': [
				{
					id: 's1',
					name: 'Overseerr',
					base_url: 'http://seerr:5055',
					enabled: true,
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z',
					last_check_at: '2026-10-25T05:00:00Z'
				}
			],
			'/prowlarr': [
				{
					id: 'p1',
					name: 'Prowlarr Main',
					base_url: 'http://prowlarr:9696',
					enabled: true,
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z',
					last_check_at: '2026-10-25T05:00:00Z'
				}
			],
			'/prowlarr/status': [
				{
					link_id: 'p1',
					name: 'Prowlarr Main',
					reachable: true,
					checked_at: '2026-10-25T05:00:00Z',
					indexers: []
				}
			],
			'/configarr/status': {
				linked: true,
				watching: true,
				config_path: '/etc/configarr/config.yml',
				last_import_at: '2026-10-25T05:00:00Z'
			}
		});

		const { container } = renderWithQuery(ConnectionsHubPage);

		expect(
			screen.getByRole('heading', { level: 1, name: new RegExp(m.nav_connections(), 'i') })
		).toBeTruthy();

		expect(
			await screen.findByRole('link', { name: new RegExp(m.nav_download_clients(), 'i') })
		).toBeTruthy();
		expect(screen.getByRole('link', { name: new RegExp(m.nav_seerr(), 'i') })).toBeTruthy();
		expect(screen.getByRole('link', { name: new RegExp(m.nav_prowlarr(), 'i') })).toBeTruthy();
		expect(screen.getByRole('link', { name: new RegExp(m.nav_configarr(), 'i') })).toBeTruthy();

		// Check status icons rendered for cards
		await waitFor(() => {
			expect(container.querySelectorAll('[data-status="ok"]').length).toBe(4);
		});

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('negative: viewer sees only allowed cards and no admin request is made', async () => {
		signIn('viewer');
		stubGet({
			'/prowlarr': [
				{
					id: 'p1',
					name: 'Prowlarr Main',
					base_url: 'http://prowlarr:9696',
					enabled: true,
					created_at: '2026-01-01T00:00:00Z',
					updated_at: '2026-01-01T00:00:00Z',
					last_check_at: '2026-10-25T05:00:00Z'
				}
			],
			'/prowlarr/status': []
		});

		const getSpy = vi.spyOn(api, 'GET');

		renderWithQuery(ConnectionsHubPage);

		expect(
			await screen.findByRole('link', { name: new RegExp(m.nav_prowlarr(), 'i') })
		).toBeTruthy();

		// Admin-only cards must not be present
		expect(
			screen.queryByRole('link', { name: new RegExp(m.nav_download_clients(), 'i') })
		).toBeNull();
		expect(screen.queryByRole('link', { name: new RegExp(m.nav_seerr(), 'i') })).toBeNull();
		expect(screen.queryByRole('link', { name: new RegExp(m.nav_configarr(), 'i') })).toBeNull();

		// Verify spy calls: only prowlarr called, zero admin-only calls
		const calledUrls = getSpy.mock.calls.map((call) => call[0]);
		expect(calledUrls).toContain('/prowlarr');
		expect(calledUrls).not.toContain('/download-clients');
		expect(calledUrls).not.toContain('/download-clients/status');
		expect(calledUrls).not.toContain('/seerr');
		expect(calledUrls).not.toContain('/configarr/status');
	});

	it('resilience (UIF1): one service 503 renders ProblemNote on that card while others render', async () => {
		signIn('admin');
		stubGet({
			'/download-clients': [],
			'/download-clients/status': [],
			'/seerr': [],
			'/prowlarr': new ProblemError({
				type: 'about:blank',
				status: 503,
				detail: 'Prowlarr service unavailable'
			}),
			'/prowlarr/status': [],
			'/configarr/status': { linked: false, watching: false }
		});

		renderWithQuery(ConnectionsHubPage);

		// ProblemNote appears on the failing card
		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain('Prowlarr service unavailable');

		// The other cards still render normally
		expect(
			screen.getByRole('link', { name: new RegExp(m.nav_download_clients(), 'i') })
		).toBeTruthy();
		expect(screen.getByRole('link', { name: new RegExp(m.nav_seerr(), 'i') })).toBeTruthy();
		expect(screen.getByRole('link', { name: new RegExp(m.nav_configarr(), 'i') })).toBeTruthy();
	});

	it('boundary: admin sees + Add link call to action on cards with nothing linked', async () => {
		signIn('admin');
		stubGet({
			'/download-clients': [],
			'/download-clients/status': [],
			'/seerr': [],
			'/prowlarr': [],
			'/prowlarr/status': [],
			'/configarr/status': { linked: false, watching: false }
		});

		renderWithQuery(ConnectionsHubPage);

		await waitFor(() => {
			const addLinks = screen.getAllByRole('link', { name: m.connections_add_link() });
			expect(addLinks.length).toBe(4);
		});
	});
});
