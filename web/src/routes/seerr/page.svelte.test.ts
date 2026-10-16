// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, waitFor, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { toast } from 'svelte-sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import {
	failedInstance,
	renderWithQuery,
	stubGet,
	untestedInstance
} from '#lib/testing/harness.js';
import SeerrPage from './+page.svelte';

type SeerrRequest = components['schemas']['SeerrRequest'];
type SeerrLink = components['schemas']['SeerrLink'];

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

const noLinks = { '/seerr': [], '/instances': [] };

describe('seerr', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading, table headers, request details and snatch button', async () => {
		stubGet({ ...noLinks, '/seerr/requests': [sampleRequest, unresolvedRequest] });
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
			...noLinks,
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
		stubGet({ ...noLinks, '/seerr/requests': [] });
		renderWithQuery(SeerrPage);

		expect(await screen.findByText(m.seerr_empty())).toBeTruthy();
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('triggers snatch mutation when snatch button is clicked', async () => {
		stubGet({ ...noLinks, '/seerr/requests': [sampleRequest] });
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

const sampleLink: SeerrLink = {
	id: '00000000-0000-0000-0000-000000000030',
	name: 'Main Overseerr',
	base_url: 'http://overseerr.local:5055',
	enabled: true,
	radarr_instance_id: untestedInstance.id,
	last_seen_version: '1.33.2',
	last_check_at: '2026-09-01T12:00:00Z',
	last_sync_at: '2026-09-01T12:05:00Z',
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

const linkStubs = {
	'/seerr': [sampleLink],
	'/instances': [failedInstance, untestedInstance],
	'/seerr/requests': []
};

async function fill(label: string, value: string) {
	await fireEvent.input(screen.getByLabelText(label), { target: { value } });
}

async function linkCard() {
	const heading = await screen.findByRole('heading', { level: 3, name: /Main Overseerr/ });
	return within(heading.closest('li') as HTMLElement);
}

/** Answers `api.POST` per path template, like stubGet does for reads. */
function stubPost(table: Record<string, unknown>) {
	return vi.spyOn(api, 'POST').mockImplementation(async (path: string) => {
		const answer = table[path];
		if (answer instanceof Error) throw answer;
		return { data: answer } as never;
	});
}

describe('seerr links', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('creates a link with its API key and a Sonarr fallback', async () => {
		stubGet(linkStubs);
		const postSpy = stubPost({ '/seerr': sampleLink });
		renderWithQuery(SeerrPage);

		const sonarr = await screen.findByLabelText(m.seerr_sonarr_fallback());
		await within(sonarr).findByRole('option', { name: failedInstance.name });
		expect(within(sonarr).queryByRole('option', { name: untestedInstance.name })).toBeNull();
		const apiKey = screen.getByLabelText(m.instance_api_key()) as HTMLInputElement;
		expect(apiKey.required).toBe(true);
		expect(apiKey.value).toBe('');

		await fill(m.instance_name(), 'Jellyseerr');
		await fill(m.instance_url(), 'http://jellyseerr:5055');
		await fill(m.instance_api_key(), 'seerr-key-123');
		await fireEvent.change(sonarr, { target: { value: failedInstance.id } });
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect(postSpy).toHaveBeenCalledWith('/seerr', {
			body: {
				name: 'Jellyseerr',
				base_url: 'http://jellyseerr:5055',
				enabled: true,
				api_key: 'seerr-key-123',
				sonarr_instance_id: failedInstance.id
			}
		});
	});

	it('edits a link without prefilling or sending the stored API key', async () => {
		stubGet(linkStubs);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({ data: sampleLink } as never);
		renderWithQuery(SeerrPage);

		await fireEvent.click((await linkCard()).getByRole('button', { name: m.action_edit() }));

		expect(
			screen.getByRole('heading', { level: 2, name: m.form_edit_title({ name: sampleLink.name }) })
		).toBeTruthy();
		const apiKey = screen.getByLabelText(m.instance_api_key()) as HTMLInputElement;
		expect(apiKey.value).toBe('');
		expect(apiKey.required).toBe(false);
		await fill(m.instance_url(), 'http://overseerr:5055');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect(putSpy).toHaveBeenCalledWith('/seerr/{linkId}', {
			params: { path: { linkId: sampleLink.id } },
			body: {
				name: sampleLink.name,
				base_url: 'http://overseerr:5055',
				enabled: true,
				radarr_instance_id: untestedInstance.id
			}
		});
	});

	it('deletes a link only after the in-page confirmation', async () => {
		stubGet(linkStubs);
		const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(SeerrPage);

		const card = await linkCard();
		await fireEvent.click(card.getByRole('button', { name: m.action_delete() }));
		expect(deleteSpy).not.toHaveBeenCalled();
		await fireEvent.click(card.getByRole('button', { name: m.confirm_delete_yes() }));

		expect(deleteSpy).toHaveBeenCalledWith('/seerr/{linkId}', {
			params: { path: { linkId: sampleLink.id } }
		});
	});

	it('keeps the link when the confirmation is cancelled', async () => {
		stubGet(linkStubs);
		const deleteSpy = vi.spyOn(api, 'DELETE');
		renderWithQuery(SeerrPage);

		const card = await linkCard();
		await fireEvent.click(card.getByRole('button', { name: m.action_delete() }));
		await fireEvent.click(card.getByRole('button', { name: m.action_cancel() }));

		expect(deleteSpy).not.toHaveBeenCalled();
		expect(card.getByRole('button', { name: m.action_delete() })).toBeTruthy();
	});

	it('tests, syncs and imports instances from a stored link', async () => {
		stubGet(linkStubs);
		const success = vi.spyOn(toast, 'success');
		const postSpy = stubPost({
			'/seerr/{linkId}/test': { app_name: 'Overseerr', version: '1.33.2' },
			'/seerr/{linkId}/sync': { seen: 4, resolved: 3, unresolved: 1, removed: 0 },
			'/seerr/{linkId}/import-instances': { created: ['Sonarr'], skipped: ['Radarr', '4K'] }
		});
		renderWithQuery(SeerrPage);
		const card = await linkCard();
		const path = { params: { path: { linkId: sampleLink.id } } };

		await fireEvent.click(card.getByRole('button', { name: m.action_test() }));
		expect(postSpy).toHaveBeenCalledWith('/seerr/{linkId}/test', path);
		await waitFor(() =>
			expect(success).toHaveBeenCalledWith(
				m.instance_probe_ok({ app: 'Overseerr', version: '1.33.2' })
			)
		);

		await fireEvent.click(card.getByRole('button', { name: m.seerr_sync() }));
		expect(postSpy).toHaveBeenCalledWith('/seerr/{linkId}/sync', path);
		await waitFor(() =>
			expect(success).toHaveBeenCalledWith(
				m.seerr_sync_done({ seen: 4, resolved: 3, unresolved: 1 })
			)
		);

		await fireEvent.click(card.getByRole('button', { name: m.seerr_import() }));
		expect(postSpy).toHaveBeenCalledWith('/seerr/{linkId}/import-instances', path);
		await waitFor(() =>
			expect(success).toHaveBeenCalledWith(m.seerr_import_done({ created: 1, skipped: 2 }))
		);
	});

	it('tests the typed credentials before saving', async () => {
		stubGet(linkStubs);
		const postSpy = stubPost({ '/seerr/test': { app_name: 'Jellyseerr', version: '2.1.0' } });
		renderWithQuery(SeerrPage);

		await screen.findByLabelText(m.seerr_sonarr_fallback());
		await fill(m.instance_name(), 'Jellyseerr');
		await fill(m.instance_url(), 'http://jellyseerr:5055');
		await fill(m.instance_api_key(), 'k');
		const form = within(screen.getByRole('region', { name: m.link_add() }) as HTMLElement);
		await fireEvent.click(form.getByRole('button', { name: m.action_test() }));

		expect(postSpy).toHaveBeenCalledWith('/seerr/test', {
			body: { name: 'Jellyseerr', base_url: 'http://jellyseerr:5055', enabled: true, api_key: 'k' }
		});
	});

	it('shows a 422 validation problem on the form and on the field it names', async () => {
		stubGet(linkStubs);
		stubPost({
			'/seerr': new ProblemError({
				type: 'about:blank',
				status: 422,
				detail: 'request validation failed',
				invalidParams: [{ name: 'name', reason: 'len 0 less than minimum 1' }]
			})
		});
		renderWithQuery(SeerrPage);

		await screen.findByLabelText(m.seerr_sonarr_fallback());
		await fill(m.instance_name(), ' ');
		await fill(m.instance_url(), 'http://seerr:5055');
		await fill(m.instance_api_key(), 'k');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect((await screen.findByRole('alert')).textContent).toContain('request validation failed');
		expect(screen.getByLabelText(m.instance_name()).getAttribute('aria-invalid')).toBe('true');
		expect(screen.getByText('len 0 less than minimum 1')).toBeTruthy();
	});
});
