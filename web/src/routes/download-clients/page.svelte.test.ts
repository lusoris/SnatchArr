// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { forkInstance, renderWithQuery, stubGet } from '#lib/testing/harness.js';
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
		const option = await screen.findByRole('option', { name: forkInstance.name });
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
		expect(screen.queryByRole('heading', { level: 2 })).toBeNull();
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
