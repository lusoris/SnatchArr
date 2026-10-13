// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import CleanuparrPage from './+page.svelte';

const sampleLink = {
	link_id: '00000000-0000-0000-0000-000000000010',
	name: 'Primary Cleanuparr',
	version: '2.1.0',
	reachable: true,
	checked_at: '2026-09-01T12:00:00Z',
	struck_downloads: 3,
	marked_for_removal: 1,
	removed_downloads: 0,
	media_managers: {},
	recent_strikes: [
		{
			id: 's-1',
			type: 'Stalled',
			created_at: '2026-09-01T12:00:00Z',
			download_id: 'dl-1',
			title: 'Sample Movie 2026',
			dry_run: false
		}
	]
};

describe('cleanuparr', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading, link details, status and recent strikes', async () => {
		stubGet({ '/cleanuparr/status': [sampleLink] });
		renderWithQuery(CleanuparrPage);

		expect(
			await screen.findByRole('heading', { level: 1, name: m.cleanuparr_title() })
		).toBeTruthy();
		expect(
			screen.getByRole('heading', { level: 2, name: /Primary Cleanuparr/ }).textContent
		).toContain('Primary Cleanuparr');
		expect(screen.getByRole('status').textContent).toContain(m.dlclients_reachable());
		expect(
			screen.getByText(
				new RegExp(
					`${m.cleanuparr_struck({ n: sampleLink.struck_downloads })} · ${m.cleanuparr_marked({ n: sampleLink.marked_for_removal })}`
				)
			)
		).toBeTruthy();

		const list = screen.getByRole('list');
		expect(within(list).getByText(/Stalled · Sample Movie 2026/)).toBeTruthy();
	});

	it('announces the problem document when the status query fails', async () => {
		stubGet({
			'/cleanuparr/status': new ProblemError({
				type: 'about:blank',
				status: 503,
				detail: 'cleanuparr service unreachable'
			})
		});
		renderWithQuery(CleanuparrPage);

		expect((await screen.findByRole('alert')).textContent).toContain(
			'cleanuparr service unreachable'
		);
		expect(screen.queryByRole('heading', { level: 2 })).toBeNull();
	});

	it('renders the empty state when no link is configured', async () => {
		stubGet({ '/cleanuparr/status': [] });
		renderWithQuery(CleanuparrPage);

		expect(await screen.findByText(m.cleanuparr_empty())).toBeTruthy();
		expect(screen.queryByRole('heading', { level: 2 })).toBeNull();
	});
});
