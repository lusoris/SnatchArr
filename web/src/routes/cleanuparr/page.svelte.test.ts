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
		stubGet({ '/cleanuparr': [], '/cleanuparr/status': [sampleLink] });
		renderWithQuery(CleanuparrPage);

		expect(
			await screen.findByRole('heading', { level: 1, name: m.cleanuparr_title() })
		).toBeTruthy();
		expect(
			screen.getByRole('heading', { level: 3, name: /Primary Cleanuparr/ }).textContent
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
			'/cleanuparr': [],
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
		expect(screen.queryByRole('heading', { level: 3 })).toBeNull();
	});

	it('renders the empty state when no link is configured', async () => {
		stubGet({ '/cleanuparr': [], '/cleanuparr/status': [] });
		renderWithQuery(CleanuparrPage);

		expect(await screen.findByText(m.cleanuparr_empty())).toBeTruthy();
		expect(screen.queryByRole('heading', { level: 3 })).toBeNull();
	});
});

type CleanuparrLink = components['schemas']['CleanuparrLink'];

const storedLink: CleanuparrLink = {
	id: '00000000-0000-0000-0000-000000000010',
	name: 'Primary Cleanuparr',
	base_url: 'http://cleanuparr.local:11011',
	enabled: true,
	last_seen_version: '2.1.0',
	last_check_at: '2026-09-01T12:00:00Z',
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

const linkStubs = { '/cleanuparr': [storedLink], '/cleanuparr/status': [] };

async function fill(label: string, value: string) {
	await fireEvent.input(screen.getByLabelText(label), { target: { value } });
}

async function linkCard() {
	const heading = await screen.findByRole('heading', { level: 3, name: /Primary Cleanuparr/ });
	return within(heading.closest('li') as HTMLElement);
}

describe('cleanuparr links', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('states that SnatchArr only reads from Cleanuparr', async () => {
		stubGet(linkStubs);
		renderWithQuery(CleanuparrPage);

		expect(await screen.findByText(m.cleanuparr_readonly_hint())).toBeTruthy();
	});

	it('creates a link with its API key', async () => {
		stubGet({ '/cleanuparr': [], '/cleanuparr/status': [] });
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: storedLink } as never);
		renderWithQuery(CleanuparrPage);

		await screen.findByText(m.cleanuparr_links_empty());
		const apiKey = screen.getByLabelText(m.instance_api_key()) as HTMLInputElement;
		expect(apiKey.required).toBe(true);
		await fill(m.instance_name(), 'Cleanuparr');
		await fill(m.instance_url(), 'http://cleanuparr:11011');
		await fill(m.instance_api_key(), 'cleanuparr-key');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect(postSpy).toHaveBeenCalledWith('/cleanuparr', {
			body: {
				name: 'Cleanuparr',
				base_url: 'http://cleanuparr:11011',
				enabled: true,
				api_key: 'cleanuparr-key'
			}
		});
	});

	it('edits a link without prefilling or sending the stored API key', async () => {
		stubGet(linkStubs);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({ data: storedLink } as never);
		renderWithQuery(CleanuparrPage);

		await fireEvent.click((await linkCard()).getByRole('button', { name: m.action_edit() }));
		const apiKey = screen.getByLabelText(m.instance_api_key()) as HTMLInputElement;
		expect(apiKey.value).toBe('');
		expect(apiKey.required).toBe(false);
		await fireEvent.click(screen.getByLabelText(m.form_enabled()));
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect(putSpy).toHaveBeenCalledWith('/cleanuparr/{linkId}', {
			params: { path: { linkId: storedLink.id } },
			body: { name: storedLink.name, base_url: storedLink.base_url, enabled: false }
		});
	});

	it('returns to the create form when an edit is cancelled', async () => {
		stubGet(linkStubs);
		renderWithQuery(CleanuparrPage);

		await fireEvent.click((await linkCard()).getByRole('button', { name: m.action_edit() }));
		await fireEvent.click(screen.getByRole('button', { name: m.action_cancel() }));

		expect(screen.getByRole('heading', { level: 2, name: m.link_add() })).toBeTruthy();
		expect((screen.getByLabelText(m.instance_name()) as HTMLInputElement).value).toBe('');
	});

	it('deletes a link only after the in-page confirmation', async () => {
		stubGet(linkStubs);
		const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(CleanuparrPage);

		const card = await linkCard();
		await fireEvent.click(card.getByRole('button', { name: m.action_delete() }));
		expect(deleteSpy).not.toHaveBeenCalled();
		await fireEvent.click(card.getByRole('button', { name: m.confirm_delete_yes() }));

		expect(deleteSpy).toHaveBeenCalledWith('/cleanuparr/{linkId}', {
			params: { path: { linkId: storedLink.id } }
		});
	});

	it('keeps the link when the confirmation is cancelled', async () => {
		stubGet(linkStubs);
		const deleteSpy = vi.spyOn(api, 'DELETE');
		renderWithQuery(CleanuparrPage);

		const card = await linkCard();
		await fireEvent.click(card.getByRole('button', { name: m.action_delete() }));
		await fireEvent.click(card.getByRole('button', { name: m.action_cancel() }));

		expect(deleteSpy).not.toHaveBeenCalled();
		expect(card.getByRole('button', { name: m.action_delete() })).toBeTruthy();
	});

	it('tests a stored link and reports a failure', async () => {
		stubGet(linkStubs);
		const failure = vi.spyOn(toast, 'error');
		const postSpy = vi
			.spyOn(api, 'POST')
			.mockRejectedValue(
				new ProblemError({ type: 'about:blank', status: 502, detail: 'connection refused' })
			);
		renderWithQuery(CleanuparrPage);

		await fireEvent.click((await linkCard()).getByRole('button', { name: m.action_test() }));

		expect(postSpy).toHaveBeenCalledWith('/cleanuparr/{linkId}/test', {
			params: { path: { linkId: storedLink.id } }
		});
		await waitFor(() =>
			expect(failure).toHaveBeenCalledWith(m.probe_failed({ detail: 'connection refused' }))
		);
	});

	it('tests the typed credentials before saving', async () => {
		stubGet({ '/cleanuparr': [], '/cleanuparr/status': [] });
		const postSpy = vi
			.spyOn(api, 'POST')
			.mockResolvedValue({ data: { app_name: 'Cleanuparr', version: '2.1.0' } } as never);
		renderWithQuery(CleanuparrPage);

		await screen.findByText(m.cleanuparr_links_empty());
		await fill(m.instance_name(), 'Cleanuparr');
		await fill(m.instance_url(), 'http://cleanuparr:11011');
		await fill(m.instance_api_key(), 'k');
		await fireEvent.click(screen.getByRole('button', { name: m.action_test() }));

		expect(postSpy).toHaveBeenCalledWith('/cleanuparr/test', {
			body: { name: 'Cleanuparr', base_url: 'http://cleanuparr:11011', enabled: true, api_key: 'k' }
		});
	});

	it('shows a 422 validation problem on the form and on the field it names', async () => {
		stubGet({ '/cleanuparr': [], '/cleanuparr/status': [] });
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 422,
				detail: 'request validation failed',
				invalidParams: [{ name: 'base_url', reason: 'base URL must be http or https' }]
			})
		);
		renderWithQuery(CleanuparrPage);

		await screen.findByText(m.cleanuparr_links_empty());
		await fill(m.instance_name(), 'Cleanuparr');
		await fill(m.instance_url(), 'ftp://cleanuparr');
		await fill(m.instance_api_key(), 'key');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect((await screen.findByRole('alert')).textContent).toContain('request validation failed');
		expect(screen.getByLabelText(m.instance_url()).getAttribute('aria-invalid')).toBe('true');
		expect(screen.getByText('base URL must be http or https')).toBeTruthy();
	});
});
