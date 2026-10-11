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
		expect(failed.textContent).toBe('Connection refused (ECONNREFUSED)');
		expect(screen.queryByText(lastCheck)).toBeNull();
		expect(screen.getByText(/^\s*sonarr · /).textContent?.trim()).toBe(
			'sonarr · http://sonarr.local:8989'
		);
	});

	it('shows neither state for an instance no connection test has reached', async () => {
		stubGet({ '/instances': [untestedInstance], '/hourly-caps': [] });
		renderWithQuery(Dashboard);

		expect(await screen.findByRole('link', { name: 'Fresh Radarr' })).toBeTruthy();
		expect(screen.getByText(/^\s*radarr · /).textContent?.trim()).toBe(
			'radarr · http://radarr.local:7878'
		);
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
});
