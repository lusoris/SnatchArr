// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import SeerrPage from './+page.svelte';

type SeerrRequest = components['schemas']['SeerrRequest'];

const sampleRequest: SeerrRequest = {
	link_id: '00000000-0000-0000-0000-000000000030',
	link_name: 'Main Overseerr',
	request_id: 42,
	media_type: 'movie',
	tmdb_id: 1234,
	tvdb_id: 0,
	title: 'Inception',
	request_status: 2,
	media_status: 3,
	is_4k: false,
	requested_by: 'alice',
	seasons: [],
	instance_id: '00000000-0000-0000-0000-000000000001',
	instance_name: 'Library Radarr',
	entity_id: 99,
	resolved: true,
	last_snatched_at: '2026-09-01T12:00:00Z',
	last_seen_at: '2026-09-01T12:00:00Z'
};

const unresolvedRequest: SeerrRequest = {
	link_id: '00000000-0000-0000-0000-000000000030',
	link_name: 'Main Overseerr',
	request_id: 43,
	media_type: 'movie',
	tmdb_id: 5678,
	tvdb_id: 0,
	title: 'Interstellar',
	request_status: 1,
	media_status: 1,
	is_4k: false,
	requested_by: 'bob',
	seasons: [],
	resolved: false,
	unresolved_reason: 'no matching instance',
	last_seen_at: '2026-09-01T12:00:00Z'
};

describe('seerr', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading, table headers, request details and snatch button', async () => {
		stubGet({ '/seerr/requests': [sampleRequest, unresolvedRequest] });
		renderWithQuery(SeerrPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.seerr_title() })).toBeTruthy();

		const table = await screen.findByRole('table');
		expect(within(table).getByRole('columnheader', { name: m.history_title_col() })).toBeTruthy();
		expect(within(table).getByRole('columnheader', { name: m.seerr_requested_by() })).toBeTruthy();
		expect(within(table).getByRole('columnheader', { name: m.instance_name() })).toBeTruthy();
		expect(within(table).getByRole('columnheader', { name: m.seerr_resolved() })).toBeTruthy();

		expect(within(table).getByText('Inception')).toBeTruthy();
		expect(within(table).getByText('alice')).toBeTruthy();
		expect(within(table).getByText('Library Radarr')).toBeTruthy();
		expect(within(table).getByText('✓')).toBeTruthy();

		expect(within(table).getByText('Interstellar')).toBeTruthy();
		expect(within(table).getByText('bob')).toBeTruthy();
		expect(within(table).getByText('no matching instance').textContent).toBe(
			'no matching instance'
		);

		const buttons = within(table).getAllByRole('button', {
			name: m.seerr_snatch_now()
		}) as HTMLButtonElement[];
		expect(buttons[0].disabled).toBe(false);
		expect(buttons[1].disabled).toBe(true);
	});

	it('announces the problem document when the requests query fails', async () => {
		stubGet({
			'/seerr/requests': new ProblemError({
				type: 'about:blank',
				status: 502,
				detail: 'seerr gateway unreachable'
			})
		});
		renderWithQuery(SeerrPage);

		expect((await screen.findByRole('alert')).textContent).toContain('seerr gateway unreachable');
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('renders the empty state card when there are no requests', async () => {
		stubGet({ '/seerr/requests': [] });
		renderWithQuery(SeerrPage);

		expect(await screen.findByText(m.seerr_empty())).toBeTruthy();
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('triggers snatch mutation when snatch button is clicked', async () => {
		stubGet({ '/seerr/requests': [sampleRequest] });
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: {} } as never);
		renderWithQuery(SeerrPage);

		const button = await screen.findByRole('button', { name: m.seerr_snatch_now() });
		await fireEvent.click(button);

		expect(postSpy).toHaveBeenCalledWith('/seerr/requests/{linkId}/{requestId}/snatch', {
			params: {
				path: {
					linkId: sampleRequest.link_id,
					requestId: sampleRequest.request_id
				}
			}
		});
	});
});
