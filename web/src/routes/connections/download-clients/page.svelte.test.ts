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
	cancelDeleteInCard,
	fill,
	forkInstance,
	renderWithQuery,
	stubGet
} from '#lib/testing/harness.js';
import DownloadClientsPage from './+page.svelte';

type DownloadClient = components['schemas']['DownloadClient'];
type DownloadClientStatus = components['schemas']['DownloadClientStatus'];

const sampleClient: DownloadClient = {
	id: '00000000-0000-0000-0000-000000000040',
	name: 'qBittorrent Primary',
	kind: 'qbittorrent',
	protocol: 'torrent',
	base_url: 'http://qbittorrent.local:8080',
	username: 'admin',
	enabled: true,
	source: 'manual',
	max_active: 5,
	max_queued: 0,
	bandwidth_budget_bps: 0,
	created_at: '2026-01-01T00:00:00Z',
	updated_at: '2026-01-01T00:00:00Z'
};

const sampleStatus: DownloadClientStatus = {
	client_id: '00000000-0000-0000-0000-000000000040',
	name: 'qBittorrent Primary',
	kind: 'qbittorrent',
	reachable: true,
	paused: false,
	checked_at: '2026-09-01T12:00:00Z',
	active: 2,
	queued: 1,
	download_rate_bps: 204800,
	upload_rate_bps: 102400
};

describe('download-clients', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('renders heading, clients list and client status', async () => {
		stubGet({
			'/download-clients': [sampleClient],
			'/download-clients/status': [sampleStatus],
			'/instances': [forkInstance]
		});
		renderWithQuery(DownloadClientsPage);

		expect(
			await screen.findByRole('heading', { level: 1, name: m.dlclients_title() })
		).toBeTruthy();
		expect(
			screen.getByRole('heading', { level: 2, name: /qBittorrent Primary/ }).textContent
		).toContain('qBittorrent Primary');
		expect(screen.getByText('http://qbittorrent.local:8080')).toBeTruthy();
		const statusText = screen.getByRole('status').textContent;
		expect(statusText).toContain(m.dlclients_reachable());
		expect(statusText).toContain(m.dlclients_active({ active: 2, queued: 1 }));
		expect(statusText).toContain('200 KiB/s');
		const option = await within(screen.getByLabelText(m.dlclients_discover())).findByRole(
			'option',
			{
				name: forkInstance.name
			}
		);
		expect(option.textContent).toBe(forkInstance.name);
	});

	it('announces the problem document when the clients query fails', async () => {
		stubGet({
			'/download-clients': new ProblemError({
				type: 'about:blank',
				status: 500,
				detail: 'download clients unavailable'
			}),
			'/download-clients/status': [],
			'/instances': []
		});
		renderWithQuery(DownloadClientsPage);

		expect((await screen.findByRole('alert')).textContent).toContain(
			'download clients unavailable'
		);
		expect(screen.queryByRole('list')).toBeNull();
	});

	it('renders the empty state when no download clients exist', async () => {
		stubGet({
			'/download-clients': [],
			'/download-clients/status': [],
			'/instances': []
		});
		renderWithQuery(DownloadClientsPage);

		expect(await screen.findByText(m.dlclients_empty())).toBeTruthy();
	});

	it('does not dispatch discover when no instance is selected', async () => {
		stubGet({
			'/download-clients': [],
			'/download-clients/status': [],
			'/instances': [forkInstance]
		});
		const postSpy = vi.spyOn(api, 'POST');
		renderWithQuery(DownloadClientsPage);

		const button = await screen.findByRole('button', { name: m.dlclients_discover() });
		expect((button as HTMLButtonElement).disabled).toBe(true);
		await fireEvent.click(button);
		expect(postSpy).not.toHaveBeenCalled();
	});

	it('dispatches discover with instanceId when submitted', async () => {
		stubGet({
			'/download-clients': [],
			'/download-clients/status': [],
			'/instances': [forkInstance]
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({
			data: { imported: 1, updated: 0, removed: 0 }
		} as never);
		renderWithQuery(DownloadClientsPage);

		const select = await screen.findByLabelText(m.dlclients_discover());
		await fireEvent.change(select, { target: { value: forkInstance.id } });

		const button = screen.getByRole('button', { name: m.dlclients_discover() });
		expect((button as HTMLButtonElement).disabled).toBe(false);
		await fireEvent.click(button);

		expect(postSpy).toHaveBeenCalledWith('/instances/{instanceId}/download-clients/discover', {
			params: { path: { instanceId: forkInstance.id } }
		});
	});
});

const listStubs = {
	'/download-clients': [sampleClient],
	'/download-clients/status': [sampleStatus],
	'/instances': [forkInstance]
};

/** The card of the sample client, where its Edit, Test and Delete buttons live. */
async function sampleCard() {
	const heading = await screen.findByRole('heading', { level: 2, name: /qBittorrent Primary/ });
	return within(heading.closest('tr, li') as HTMLElement);
}

/** Stubs list GET endpoints and renders the page with a PUT spy. */
async function openEdit() {
	stubGet(listStubs);
	const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({ data: sampleClient } as never);
	renderWithQuery(DownloadClientsPage);
	await fireEvent.click((await sampleCard()).getByRole('button', { name: m.action_edit() }));
	return putSpy;
}

/** Stubs list GET endpoints and renders the page with a POST probe spy and toast spy. */
function renderWithTestProbe() {
	stubGet(listStubs);
	const success = vi.spyOn(toast, 'success');
	const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: sampleStatus } as never);
	renderWithQuery(DownloadClientsPage);
	return { success, postSpy };
}

describe('download-clients management', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('creates a client pinned to one instance, with its secret', async () => {
		stubGet(listStubs);
		const postSpy = vi
			.spyOn(api, 'POST')
			.mockResolvedValue({ data: { ...sampleClient, name: 'SAB' } } as never);
		renderWithQuery(DownloadClientsPage);

		const pin = await screen.findByLabelText(m.dlclients_instance());
		await within(pin).findByRole('option', { name: forkInstance.name });
		await fireEvent.change(screen.getByLabelText(m.dlclients_kind()), {
			target: { value: 'sabnzbd' }
		});
		await fill(m.instance_name(), 'SAB');
		await fill(m.instance_url(), 'http://sab:8080');
		await fill(m.dlclients_secret(), 'sab-api-key');
		await fireEvent.change(pin, { target: { value: forkInstance.id } });
		await fill(m.dlclients_max_active(), '3');
		await fill(m.dlclients_max_queued(), '7');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect(postSpy).toHaveBeenCalledWith('/download-clients', {
			body: {
				kind: 'sabnzbd',
				name: 'SAB',
				base_url: 'http://sab:8080',
				username: '',
				enabled: true,
				max_active: 3,
				max_queued: 7,
				bandwidth_budget_bps: 0,
				secret: 'sab-api-key',
				instance_id: forkInstance.id
			}
		});
	});

	it('leaves an empty secret and an unpinned instance out of a new client', async () => {
		stubGet(listStubs);
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: sampleClient } as never);
		renderWithQuery(DownloadClientsPage);

		await screen.findByLabelText(m.dlclients_instance());
		await fill(m.instance_name(), 'Deluge');
		await fill(m.instance_url(), 'http://deluge:8112');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		const body = (postSpy.mock.calls[0]?.[1] as { body: Record<string, unknown> }).body;
		expect(body.name).toBe('Deluge');
		expect('secret' in body).toBe(false);
		expect('instance_id' in body).toBe(false);
	});

	it('edits a client: prefilled except the secret, which stays out when left empty', async () => {
		const putSpy = await openEdit();

		expect(
			screen.getByRole('heading', {
				level: 2,
				name: m.form_edit_title({ name: sampleClient.name })
			})
		).toBeTruthy();
		expect((screen.getByLabelText(m.instance_name()) as HTMLInputElement).value).toBe(
			sampleClient.name
		);
		expect((screen.getByLabelText(m.username()) as HTMLInputElement).value).toBe('admin');
		const secret = screen.getByLabelText(m.dlclients_secret()) as HTMLInputElement;
		expect(secret.value).toBe('');
		expect(secret.type).toBe('password');
		expect(secret.required).toBe(false);
		expect(screen.getByText(m.secret_keep_hint())).toBeTruthy();

		await fill(m.instance_name(), 'qBittorrent Renamed');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect(putSpy).toHaveBeenCalledWith('/download-clients/{clientId}', {
			params: { path: { clientId: sampleClient.id } },
			body: {
				kind: 'qbittorrent',
				name: 'qBittorrent Renamed',
				base_url: sampleClient.base_url,
				username: 'admin',
				enabled: true,
				max_active: 5,
				max_queued: 0,
				bandwidth_budget_bps: 0
			}
		});
	});

	it('sends a new secret typed while editing', async () => {
		const putSpy = await openEdit();
		await fill(m.dlclients_secret(), 'rotated-password');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		const body = (putSpy.mock.calls[0]?.[1] as { body: Record<string, unknown> }).body;
		expect(body.secret).toBe('rotated-password');
	});

	it('deletes a client only after the in-page confirmation', async () => {
		stubGet(listStubs);
		const deleteSpy = vi.spyOn(api, 'DELETE').mockResolvedValue({ data: undefined } as never);
		renderWithQuery(DownloadClientsPage);

		const card = await sampleCard();
		await fireEvent.click(card.getByRole('button', { name: m.action_delete() }));
		expect(deleteSpy).not.toHaveBeenCalled();
		const question = card.getByRole('group', {
			name: m.confirm_delete({ name: sampleClient.name })
		});
		expect(document.activeElement).toBe(
			within(question).getByRole('button', { name: m.action_cancel() })
		);

		await fireEvent.click(within(question).getByRole('button', { name: m.confirm_delete_yes() }));
		expect(deleteSpy).toHaveBeenCalledWith('/download-clients/{clientId}', {
			params: { path: { clientId: sampleClient.id } }
		});
	});

	it('keeps the client when the confirmation is cancelled', async () => {
		stubGet(listStubs);
		const deleteSpy = vi.spyOn(api, 'DELETE');
		renderWithQuery(DownloadClientsPage);

		const card = await sampleCard();
		await cancelDeleteInCard(card);

		expect(deleteSpy).not.toHaveBeenCalled();
		expect(card.queryByRole('group')).toBeNull();
		expect(card.getByRole('button', { name: m.action_delete() })).toBeTruthy();
	});

	it('tests a stored client and reports the observation', async () => {
		const { success, postSpy } = renderWithTestProbe();

		await fireEvent.click((await sampleCard()).getByRole('button', { name: m.action_test() }));

		expect(postSpy).toHaveBeenCalledWith('/download-clients/{clientId}/test', {
			params: { path: { clientId: sampleClient.id } }
		});
		await waitFor(() =>
			expect(success).toHaveBeenCalledWith(m.dlclients_test_ok({ name: sampleClient.name }))
		);
	});

	it('tests the typed values before saving, and only on the create form', async () => {
		const { success, postSpy } = renderWithTestProbe();

		await screen.findByLabelText(m.dlclients_instance());
		const form = within(screen.getByRole('region', { name: m.dlclients_add() }) as HTMLElement);
		await fireEvent.click(form.getByRole('button', { name: m.action_test() }));
		expect(postSpy).not.toHaveBeenCalled();

		await fill(m.instance_name(), 'Probe');
		await fill(m.instance_url(), 'http://qb:8080');
		await fill(m.dlclients_secret(), 'pw');
		await fireEvent.click(form.getByRole('button', { name: m.action_test() }));
		expect(postSpy).toHaveBeenCalledWith('/download-clients/test', {
			body: expect.objectContaining({ name: 'Probe', base_url: 'http://qb:8080', secret: 'pw' })
		});
		await waitFor(() =>
			expect(success).toHaveBeenCalledWith(m.dlclients_test_ok({ name: 'Probe' }))
		);

		await fireEvent.click((await sampleCard()).getByRole('button', { name: m.action_edit() }));
		const editForm = within(
			screen.getByRole('region', {
				name: m.form_edit_title({ name: sampleClient.name })
			}) as HTMLElement
		);
		expect(editForm.queryByRole('button', { name: m.action_test() })).toBeNull();
	});

	it('shows a 422 validation problem on the form and on the field it names', async () => {
		stubGet(listStubs);
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 422,
				title: 'Unprocessable Entity',
				detail: 'request validation failed',
				invalidParams: [{ name: 'max_active', reason: 'value 5000 greater than maximum 1000' }]
			})
		);
		renderWithQuery(DownloadClientsPage);

		await screen.findByLabelText(m.dlclients_instance());
		await fill(m.instance_name(), 'Too many');
		await fill(m.instance_url(), 'http://qb:8080');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect((await screen.findByRole('alert')).textContent).toContain('request validation failed');
		const field = screen.getByLabelText(m.dlclients_max_active());
		expect(field.getAttribute('aria-invalid')).toBe('true');
		expect(field.getAttribute('aria-describedby')).toBe('dlclient-max-active-error');
		expect(screen.getByText('value 5000 greater than maximum 1000')).toBeTruthy();
	});

	it('shows a 422 validation problem on the max_queued field', async () => {
		stubGet(listStubs);
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 422,
				title: 'Unprocessable Entity',
				detail: 'request validation failed',
				invalidParams: [{ name: 'max_queued', reason: 'value 5000 greater than maximum 1000' }]
			})
		);
		renderWithQuery(DownloadClientsPage);

		await screen.findByLabelText(m.dlclients_instance());
		await fill(m.instance_name(), 'Too many queued');
		await fill(m.instance_url(), 'http://qb:8080');
		await fireEvent.click(screen.getByRole('button', { name: m.action_save() }));

		expect((await screen.findByRole('alert')).textContent).toContain('request validation failed');
		const field = screen.getByLabelText(m.dlclients_max_queued());
		expect(field.getAttribute('aria-invalid')).toBe('true');
		expect(field.getAttribute('aria-describedby')).toBe('dlclient-max-queued-error');
		expect(screen.getByText('value 5000 greater than maximum 1000')).toBeTruthy();
	});

	it('disables edit and delete buttons for configarr download clients', async () => {
		const configarrClient: DownloadClient = {
			...sampleClient,
			id: '00000000-0000-0000-0000-000000000099',
			name: 'Configarr qBit',
			source: 'configarr'
		};
		stubGet({
			'/download-clients': [configarrClient],
			'/download-clients/status': [],
			'/instances': []
		});
		renderWithQuery(DownloadClientsPage);

		const card = within(
			(await screen.findByRole('heading', { level: 2, name: /Configarr qBit/ })).closest(
				'tr, li'
			) as HTMLElement
		);
		const editBtn = card.getByRole('button', { name: m.action_edit() }) as HTMLButtonElement;
		expect(editBtn.disabled).toBe(true);
		const deleteBtn = card.getByRole('button', { name: m.action_delete() }) as HTMLButtonElement;
		expect(deleteBtn.disabled).toBe(true);
	});
});
