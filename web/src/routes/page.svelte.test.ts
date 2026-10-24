// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import {
	failedInstance,
	forkInstance,
	renderWithQuery,
	stubGet,
	untestedInstance
} from '#lib/testing/harness.js';
import Dashboard from './+page.svelte';

const lastCheck = new RegExp(m.instance_last_check());

describe('dashboard', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('shows name, kind with the fork, address and the passed connection test', async () => {
		stubGet({ '/instances': [forkInstance], '/hourly-caps': [] });
		renderWithQuery(Dashboard);

		const link = await screen.findByRole('link', { name: 'Library Readarr' });
		expect(link.getAttribute('href')).toBe(`/instances/${forkInstance.id}`);
		expect(screen.getByText(/readarr · Bookshelf/).textContent?.trim()).toBe(
			'readarr · Bookshelf · http://readarr.local:8787'
		);
		expect(screen.getByText(lastCheck).textContent).toContain('0.4.21.182');
		expect(screen.queryByRole('status')).toBeNull();
	});

	it('shows the error of a failed connection test, not a passed one', async () => {
		stubGet({ '/instances': [failedInstance], '/hourly-caps': [] });
		renderWithQuery(Dashboard);

		const failed = await screen.findByRole('status');
		expect(failed.textContent?.replace(/\s+/g, ' ').trim()).toBe(
			`${m.instance_check_failed()}: Connection refused (ECONNREFUSED)`
		);
		expect(screen.queryByText(lastCheck)).toBeNull();
		expect(screen.getByText(/^\s*sonarr ·/).textContent?.trim()).toBe(
			'sonarr · http://sonarr.local:8989'
		);
	});

	it('shows never tested state for an instance no connection test has reached', async () => {
		stubGet({ '/instances': [untestedInstance], '/hourly-caps': [] });
		renderWithQuery(Dashboard);

		expect(await screen.findByRole('link', { name: 'Fresh Radarr' })).toBeTruthy();
		expect(screen.getByText(/^\s*radarr ·/).textContent?.trim()).toBe(
			'radarr · http://radarr.local:7878'
		);
		expect(screen.getByText(m.instance_never_tested())).toBeTruthy();
		expect(screen.queryByRole('status')).toBeNull();
		expect(screen.queryByText(lastCheck)).toBeNull();
	});

	it('points to the instance list when there is no instance', async () => {
		stubGet({ '/instances': [], '/hourly-caps': [] });
		renderWithQuery(Dashboard);

		const empty = await screen.findByText(new RegExp(m.empty_instances()));
		expect(empty.querySelector('a')?.getAttribute('href')).toBe('/instances');
		expect(screen.queryByRole('list')).toBeNull();
	});

	it('announces the problem document when the instance list is refused', async () => {
		stubGet({
			'/instances': new ProblemError({ type: 'about:blank', status: 503, detail: 'store down' }),
			'/hourly-caps': []
		});
		renderWithQuery(Dashboard);

		expect((await screen.findByRole('alert')).textContent).toContain('store down');
		expect(screen.queryByRole('list')).toBeNull();
	});

	it('shows Prowlarr card with headroom and exhausted counts when links are present', async () => {
		stubGet({
			'/instances': [forkInstance],
			'/hourly-caps': [],
			'/prowlarr/status': [
				{
					link_id: '00000000-0000-0000-0000-000000000020',
					name: 'Primary Prowlarr',
					reachable: true,
					checked_at: '2026-09-01T12:00:00Z',
					indexers: [
						{
							id: 1,
							name: 'Fast Indexer',
							query_limit: 1000,
							grab_limit: 100,
							limits_unit: 0,
							used_queries: 250,
							used_grabs: 10,
							exhausted: false
						},
						{
							id: 2,
							name: 'Capped Indexer',
							query_limit: 100,
							grab_limit: 50,
							limits_unit: 1,
							used_queries: 100,
							used_grabs: 20,
							exhausted: true
						}
					]
				}
			]
		});
		renderWithQuery(Dashboard);

		expect(
			await screen.findByRole('heading', { level: 2, name: m.prowlarr_dashboard_title() })
		).toBeTruthy();
		expect(screen.getByText('Primary Prowlarr')).toBeTruthy();
		expect(
			screen.getByText(
				new RegExp(
					`${m.prowlarr_headroom_count({ count: 1, total: 2 })} · ${m.prowlarr_exhausted_count({ count: 1 })}`
				)
			)
		).toBeTruthy();
		const meter = screen.getByRole('meter', {
			name: m.prowlarr_headroom_label({ name: 'Primary Prowlarr' })
		});
		expect(meter.getAttribute('aria-valuenow')).toBe('1');
		expect(meter.getAttribute('aria-valuemax')).toBe('2');
	});

	it('hides Prowlarr card when no Prowlarr links are configured', async () => {
		stubGet({
			'/instances': [forkInstance],
			'/hourly-caps': [],
			'/prowlarr/status': []
		});
		renderWithQuery(Dashboard);

		await screen.findByRole('link', { name: 'Library Readarr' });
		expect(
			screen.queryByRole('heading', { level: 2, name: m.prowlarr_dashboard_title() })
		).toBeNull();
	});
});
