// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { toast } from 'svelte-sonner';
import { m } from '#lib/paraglide/messages.js';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import {
	assertCancelDelete,
	cardFor,
	confirmDeleteInCard,
	fill,
	renderWithQuery,
	stubGet
} from '#lib/testing/harness.js';
import ProwlarrPage from './+page.svelte';

type ProwlarrLink = components['schemas']['ProwlarrLink'];
type ProwlarrStatus = components['schemas']['ProwlarrStatus'];

const sampleStatus: ProwlarrStatus = {
	link_id: '00000000-0000-0000-0000-000000000020',
	name: 'Primary Prowlarr',
	version: '1.30.0',
	reachable: true,
	checked_at: '2026-09-01T12:00:00Z',
	indexers: [
		{
			id: 1,
			name: 'Day Tracker',
			query_limit: 1000,
			grab_limit: 100,
			limits_unit: 0,
			used_queries: 250,
			used_grabs: 10,
			exhausted: false
		},
		{
			id: 2,
			name: 'Hour Tracker',
			query_limit: 100,
			grab_limit: 20,
			limits_unit: 1,
			used_queries: 85,
			used_grabs: 4,
			exhausted: false
		}
	]
};

const boundaryStatus: ProwlarrStatus = {
	link_id: '00000000-0000-0000-0000-000000000021',
	name: 'Boundary Prowlarr',
	version: '1.30.0',
	reachable: true,
	checked_at: '2026-09-01T12:00:00Z',
	indexers: [
		{
			id: 10,
			name: 'Exact Limit Indexer',
			query_limit: 100,
			limits_unit: 0,
			used_queries: 100,
			used_grabs: 0,
			exhausted: true,
			exhausted_by: 'query_limit'
		},
		{
			id: 11,
			name: 'Unlimited Indexer',
			limits_unit: 0,
			used_queries: 42,
			used_grabs: 0,
			exhausted: false
		},
		{
			id: 12,
			name: 'Disabled Till Indexer',
			query_limit: 100,
			limits_unit: 0,
			used_queries: 10,
			used_grabs: 0,
			disabled_till: '2026-09-01T15:00:00Z',
			exhausted: true,
			exhausted_by: 'disabled'
		},
		{
			id: 13,
			name: 'Grab Exhausted Indexer',
			query_limit: 100,
			grab_limit: 50,
			limits_unit: 0,
			used_queries: 10,
			used_grabs: 50,
			exhausted: true,
			exhausted_by: 'grab_limit'
		}
	]
};

const storedLink: ProwlarrLink = {
	id: '00000000-0000-0000-0000-000000000020',
	name: 'Primary Prowlarr',
	base_url: 'http://prowlarr.local:9696',
	enabled: true,
	last_seen_version: '1.30.0',
	last_check_at: '2026-09-01T12:00:00Z',
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

const linkStubs = { '/prowlarr': [storedLink], '/prowlarr/status': [] };
const linkCard = () => cardFor(/Primary Prowlarr/);

describe('prowlarr', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading, link details, status and indexer rows', async () => {
		stubGet({ '/prowlarr': [], '/prowlarr/status': [sampleStatus] });
		renderWithQuery(ProwlarrPage);

		expect(await screen.findByRole('heading', { level: 1, name: m.prowlarr_title() })).toBeTruthy();
		expect(
			screen.getByRole('heading', { level: 3, name: /Primary Prowlarr/ }).textContent
		).toContain('Primary Prowlarr');
		expect(screen.getByRole('status').textContent).toContain(m.dlclients_reachable());

		const table = screen.getByRole('table');
		expect(table).toBeTruthy();
		expect(within(table).getByText('Day Tracker')).toBeTruthy();
		expect(
			within(table).getByText(new RegExp(`250 / 1000.*${m.prowlarr_unit_day()}`))
		).toBeTruthy();
		expect(within(table).getByText(new RegExp(`10 / 100.*${m.prowlarr_unit_day()}`))).toBeTruthy();
		expect(within(table).getByText('25 %')).toBeTruthy();
		expect(within(table).getByText('10 %')).toBeTruthy();
		expect(within(table).getByText(m.prowlarr_state_ok())).toBeTruthy();

		expect(within(table).getByText('Hour Tracker')).toBeTruthy();
		expect(within(table).getByText(new RegExp(`85 / 100.*${m.prowlarr_unit_hour()}`))).toBeTruthy();
		expect(within(table).getByText(new RegExp(`4 / 20.*${m.prowlarr_unit_hour()}`))).toBeTruthy();
		expect(within(table).getByText('85 %')).toBeTruthy();
		expect(within(table).getByText('20 %')).toBeTruthy();
		expect(within(table).getByText(m.prowlarr_state_near_limit())).toBeTruthy();
	});

	it('handles boundaries: exact limit shows exhausted, unlimited shows no limit, disabled-until shows disabled', async () => {
		stubGet({ '/prowlarr': [], '/prowlarr/status': [boundaryStatus] });
		renderWithQuery(ProwlarrPage);

		const table = await screen.findByRole('table');
		const exactRow = within(table).getByText('Exact Limit Indexer').closest('tr');
		expect(exactRow).toBeTruthy();
		expect(within(exactRow as HTMLElement).getByText(m.prowlarr_state_exhausted())).toBeTruthy();
		expect(
			within(exactRow as HTMLElement).getByText(m.prowlarr_exhausted_by_query_limit())
		).toBeTruthy();
		expect(within(exactRow as HTMLElement).getByText('100 %')).toBeTruthy();
		expect(
			within(exactRow as HTMLElement).getAllByText(m.prowlarr_no_limit()).length
		).toBeGreaterThan(0);

		const unlimitedRow = within(table).getByText('Unlimited Indexer').closest('tr');
		expect(unlimitedRow).toBeTruthy();
		expect(
			within(unlimitedRow as HTMLElement).getAllByText(m.prowlarr_no_limit()).length
		).toBeGreaterThan(0);
		expect(within(unlimitedRow as HTMLElement).getByText(m.prowlarr_state_ok())).toBeTruthy();

		const disabledRow = within(table).getByText('Disabled Till Indexer').closest('tr');
		expect(disabledRow).toBeTruthy();
		expect(
			within(disabledRow as HTMLElement).getByText(
				new RegExp(m.prowlarr_state_disabled_until({ when: '.*' }).replace(/\{when\}|\.\*/g, '.*'))
			)
		).toBeTruthy();

		const grabRow = within(table).getByText('Grab Exhausted Indexer').closest('tr');
		expect(grabRow).toBeTruthy();
		expect(within(grabRow as HTMLElement).getByText(m.prowlarr_state_exhausted())).toBeTruthy();
		expect(
			within(grabRow as HTMLElement).getByText(m.prowlarr_exhausted_by_grab_limit())
		).toBeTruthy();
		expect(
			within(grabRow as HTMLElement).getByText(new RegExp(`50 / 50.*${m.prowlarr_unit_day()}`))
		).toBeTruthy();
	});

	it('announces the problem document when the status query fails', async () => {
		stubGet({
			'/prowlarr': [],
			'/prowlarr/status': new ProblemError({
				type: 'about:blank',
				status: 503,
				detail: 'prowlarr service unreachable'
			})
		});
		renderWithQuery(ProwlarrPage);

		expect((await screen.findByRole('alert')).textContent).toContain(
			'prowlarr service unreachable'
		);
		expect(screen.queryByRole('heading', { level: 3 })).toBeNull();
	});

	it('renders the empty state when no link is configured', async () => {
		stubGet({ '/prowlarr': [], '/prowlarr/status': [] });
		renderWithQuery(ProwlarrPage);

		expect(await screen.findByText(m.prowlarr_empty())).toBeTruthy();
		expect(screen.queryByRole('heading', { level: 3 })).toBeNull();
	});
});

describe('prowlarr links', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('states that SnatchArr only reads from Prowlarr', async () => {
		stubGet(linkStubs);
		renderWithQuery(ProwlarrPage);

		expect(await screen.findByText(m.prowlarr_readonly_hint())).toBeTruthy();
	});

	it('creates a link with its API key', async () => {
		stubGet({ '/prowlarr': [], '/prowlarr/status': [] });
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: storedLink } as never);
		renderWithQuery(ProwlarrPage);

		await screen.findByText(m.prowlarr_links_empty());
		const apiKey = screen.getByLabelText(m.instance_api_key()) as HTMLInputElement;
		expect(apiKey.required).toBe(true);
		await fill(m.instance_name(), 'Prowlarr');
		await fill(m.instance_url(), 'http://prowlarr:9696');
		await fill(m.instance_api_key(), 'prowlarr-key');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect(postSpy).toHaveBeenCalledWith('/prowlarr', {
			body: {
				name: 'Prowlarr',
				base_url: 'http://prowlarr:9696',
				enabled: true,
				api_key: 'prowlarr-key'
			}
		});
	});

	it('edits a link without prefilling or sending the stored API key', async () => {
		stubGet(linkStubs);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({ data: storedLink } as never);
		renderWithQuery(ProwlarrPage);

		await fireEvent.click((await linkCard()).getByRole('button', { name: m.action_edit() }));
		const apiKey = screen.getByLabelText(m.instance_api_key()) as HTMLInputElement;
		expect(apiKey.value).toBe('');
		expect(apiKey.required).toBe(false);
		await fireEvent.click(screen.getByLabelText(m.form_enabled()));
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect(putSpy).toHaveBeenCalledWith('/prowlarr/{linkId}', {
			params: { path: { linkId: storedLink.id } },
			body: { name: storedLink.name, base_url: storedLink.base_url, enabled: false }
		});
	});

	it('returns to the create form when an edit is cancelled', async () => {
		stubGet(linkStubs);
		renderWithQuery(ProwlarrPage);

		await fireEvent.click((await linkCard()).getByRole('button', { name: m.action_edit() }));
		await fireEvent.click(screen.getByRole('button', { name: m.action_cancel() }));

		expect(screen.getByRole('heading', { level: 2, name: m.link_add() })).toBeTruthy();
		expect((screen.getByLabelText(m.instance_name()) as HTMLInputElement).value).toBe('');
	});

	it('deletes a link only after the in-page confirmation', async () => {
		stubGet(linkStubs);
		const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(ProwlarrPage);

		const card = await linkCard();
		expect(deleteSpy).not.toHaveBeenCalled();
		await confirmDeleteInCard(card);

		expect(deleteSpy).toHaveBeenCalledWith('/prowlarr/{linkId}', {
			params: { path: { linkId: storedLink.id } }
		});
	});

	it('keeps the link when the confirmation is cancelled', async () => {
		stubGet(linkStubs);
		const deleteSpy = vi.spyOn(api, 'DELETE');
		renderWithQuery(ProwlarrPage);

		await assertCancelDelete(await linkCard(), deleteSpy);
	});

	it('tests a stored link and reports a failure', async () => {
		stubGet(linkStubs);
		const failure = vi.spyOn(toast, 'error');
		const postSpy = vi
			.spyOn(api, 'POST')
			.mockRejectedValue(
				new ProblemError({ type: 'about:blank', status: 502, detail: 'connection refused' })
			);
		renderWithQuery(ProwlarrPage);

		await fireEvent.click((await linkCard()).getByRole('button', { name: m.action_test() }));

		expect(postSpy).toHaveBeenCalledWith('/prowlarr/{linkId}/test', {
			params: { path: { linkId: storedLink.id } }
		});
		await waitFor(() =>
			expect(failure).toHaveBeenCalledWith(m.probe_failed({ detail: 'connection refused' }))
		);
	});

	it('tests the typed credentials before saving', async () => {
		stubGet({ '/prowlarr': [], '/prowlarr/status': [] });
		const postSpy = vi
			.spyOn(api, 'POST')
			.mockResolvedValue({ data: { app_name: 'Prowlarr', version: '1.30.0' } } as never);
		renderWithQuery(ProwlarrPage);

		await screen.findByText(m.prowlarr_links_empty());
		await fill(m.instance_name(), 'Prowlarr');
		await fill(m.instance_url(), 'http://prowlarr:9696');
		await fill(m.instance_api_key(), 'k');
		await fireEvent.click(screen.getByRole('button', { name: m.action_test() }));

		expect(postSpy).toHaveBeenCalledWith('/prowlarr/test', {
			body: { name: 'Prowlarr', base_url: 'http://prowlarr:9696', enabled: true, api_key: 'k' }
		});
	});

	it('shows a 422 validation problem on the form and on the field it names', async () => {
		stubGet({ '/prowlarr': [], '/prowlarr/status': [] });
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 422,
				detail: 'request validation failed',
				invalidParams: [{ name: 'base_url', reason: 'base URL must be http or https' }]
			})
		);
		renderWithQuery(ProwlarrPage);

		await screen.findByText(m.prowlarr_links_empty());
		await fill(m.instance_name(), 'Prowlarr');
		await fill(m.instance_url(), 'ftp://prowlarr');
		await fill(m.instance_api_key(), 'key');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect((await screen.findByRole('alert')).textContent).toContain('request validation failed');
		expect(screen.getByLabelText(m.instance_url()).getAttribute('aria-invalid')).toBe('true');
		expect(screen.getByText('base URL must be http or https')).toBeTruthy();
	});
});
