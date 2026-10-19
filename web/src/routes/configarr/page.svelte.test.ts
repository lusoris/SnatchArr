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
import ConfigarrPage from './+page.svelte';

type ConfigarrStatus = components['schemas']['ConfigarrStatus'];
type ConfigarrImportResult = components['schemas']['ConfigarrImportResult'];

const lastResult: ConfigarrImportResult = {
	imported: 2,
	updated: 1,
	disabled: 0,
	clients_imported: 0,
	clients_updated: 0,
	clients_removed: 0,
	warnings: ['sonarr4k: base_url missing, skipped'],
	at: '2026-09-01T12:00:00Z'
};

const linkedStatus: ConfigarrStatus = {
	linked: true,
	config_path: '/config/config.yml',
	secrets_path: '/config/secrets.yml',
	watching: true,
	last_import_at: '2026-09-01T12:00:00Z',
	last_result: lastResult,
	last_error: 'secrets.yml: permission denied'
};

const unlinkedStatus: ConfigarrStatus = { linked: false, watching: false };

async function fill(label: string, value: string) {
	await fireEvent.input(screen.getByLabelText(label), { target: { value } });
}

describe('configarr', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('shows the linked-mode status with the last import, its warnings and error', async () => {
		stubGet({ '/configarr/status': linkedStatus });
		renderWithQuery(ConfigarrPage);

		expect(
			await screen.findByRole('heading', { level: 1, name: m.configarr_title() })
		).toBeTruthy();
		const status = within(
			screen.getByRole('region', { name: m.configarr_status_title() }) as HTMLElement
		);
		expect(
			await status.findByText(m.configarr_linked({ path: '/config/config.yml' }))
		).toBeTruthy();
		expect(
			status.getByText(m.configarr_secrets_linked({ path: '/config/secrets.yml' }))
		).toBeTruthy();
		expect(status.getByText(m.configarr_watching())).toBeTruthy();
		expect(
			status.getByText(m.configarr_result({ imported: 2, updated: 1, disabled: 0 }))
		).toBeTruthy();
		expect(status.getByText('sonarr4k: base_url missing, skipped')).toBeTruthy();
		expect(
			status.getByText(m.configarr_last_error({ detail: 'secrets.yml: permission denied' }))
		).toBeTruthy();
	});

	it('explains unlinked mode', async () => {
		stubGet({ '/configarr/status': unlinkedStatus });
		renderWithQuery(ConfigarrPage);

		expect(await screen.findByText(m.configarr_not_linked())).toBeTruthy();
		expect(screen.getByText(m.configarr_not_watching())).toBeTruthy();
		expect(screen.getByText(m.configarr_never_imported())).toBeTruthy();
	});

	it('announces the problem document when the status query fails', async () => {
		stubGet({
			'/configarr/status': new ProblemError({
				type: 'about:blank',
				status: 403,
				detail: 'admin role required'
			})
		});
		renderWithQuery(ConfigarrPage);

		expect((await screen.findByRole('alert')).textContent).toContain('admin role required');
	});

	it('re-imports the linked configuration without a body when no path is given', async () => {
		stubGet({ '/configarr/status': linkedStatus });
		const success = vi.spyOn(toast, 'success');
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: lastResult } as never);
		renderWithQuery(ConfigarrPage);

		await fireEvent.click(await screen.findByRole('button', { name: m.configarr_import() }));

		expect(postSpy).toHaveBeenCalledWith('/configarr/import', {});
		await waitFor(() =>
			expect(success).toHaveBeenCalledWith(
				m.configarr_result({ imported: 2, updated: 1, disabled: 0 })
			)
		);
	});

	it('imports a given config file once, leaving an empty secrets path out', async () => {
		stubGet({ '/configarr/status': unlinkedStatus });
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: lastResult } as never);
		renderWithQuery(ConfigarrPage);

		await screen.findByText(m.configarr_not_linked());
		await fill(m.configarr_config_path(), '/srv/configarr/config.yml');
		await fireEvent.click(screen.getByRole('button', { name: m.configarr_import() }));

		expect(postSpy).toHaveBeenCalledWith('/configarr/import', {
			body: { config_path: '/srv/configarr/config.yml' }
		});
	});

	it('shows a 422 problem on the form and on the field it names', async () => {
		stubGet({ '/configarr/status': unlinkedStatus });
		vi.spyOn(api, 'POST').mockRejectedValue(
			new ProblemError({
				type: 'about:blank',
				status: 422,
				detail: 'no Configarr configuration linked',
				invalidParams: [{ name: 'config_path', reason: 'required when not linked' }]
			})
		);
		renderWithQuery(ConfigarrPage);

		await fireEvent.click(await screen.findByRole('button', { name: m.configarr_import() }));

		expect((await screen.findByRole('alert')).textContent).toContain(
			'no Configarr configuration linked'
		);
		expect(screen.getByLabelText(m.configarr_config_path()).getAttribute('aria-invalid')).toBe(
			'true'
		);
		expect(screen.getByText('required when not linked')).toBeTruthy();
	});
});
