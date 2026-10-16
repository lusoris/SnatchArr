// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import { configarrInstance, forkInstance, renderWithQuery } from '#lib/testing/harness.js';
import InstanceEditForm from './InstanceEditForm.svelte';

const putPath = '/instances/{instanceId}';

/** Renders the form for the manual fork instance, with PUT answered by the given stub. */
function renderManual(
	put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: forkInstance } as never)
) {
	renderWithQuery(InstanceEditForm, { instance: forkInstance });
	return put;
}

async function submit(): Promise<void> {
	await fireEvent.click(screen.getByRole('button', { name: m.instance_save() }));
}

describe('InstanceEditForm', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('submits the edited fields as the PUT body', async () => {
		const put = renderManual();

		await fireEvent.input(screen.getByLabelText(m.instance_name()), {
			target: { value: 'Renamed Readarr' }
		});
		await fireEvent.input(screen.getByLabelText(m.instance_url()), {
			target: { value: 'http://readarr.lan:8787' }
		});
		await submit();

		expect(put).toHaveBeenCalledWith(putPath, {
			params: { path: { instanceId: forkInstance.id } },
			body: {
				kind: 'readarr',
				name: 'Renamed Readarr',
				base_url: 'http://readarr.lan:8787',
				enabled: true
			}
		});
	});

	it('never prefills the API key and omits it from the body while empty', async () => {
		const put = renderManual();

		const key = screen.getByLabelText(m.instance_api_key()) as HTMLInputElement;
		expect(key.value).toBe('');
		expect(key.type).toBe('password');
		expect(screen.getByText(m.instance_api_key_keep())).toBeTruthy();
		await submit();

		const body = (put.mock.calls[0]?.[1] as { body: Record<string, unknown> }).body;
		expect(Object.hasOwn(body, 'api_key')).toBe(false);
	});

	it('sends a typed API key', async () => {
		const put = renderManual();

		await fireEvent.input(screen.getByLabelText(m.instance_api_key()), {
			target: { value: 'new-key-0123456789' }
		});
		await submit();

		const body = (put.mock.calls[0]?.[1] as { body: Record<string, unknown> }).body;
		expect(body.api_key).toBe('new-key-0123456789');
	});

	it('shows a Configarr instance read-only with a note on why', async () => {
		const put = vi.spyOn(api, 'PUT');
		renderWithQuery(InstanceEditForm, { instance: configarrInstance });

		expect(screen.getByText(m.instance_configarr_readonly())).toBeTruthy();
		const name = screen.getByLabelText(m.instance_name()) as HTMLInputElement;
		expect(name.value).toBe('Configarr Sonarr');
		expect(name.matches(':disabled')).toBe(true);
		expect((screen.getByLabelText(m.instance_url()) as HTMLInputElement).matches(':disabled')).toBe(
			true
		);
		expect((screen.getByLabelText(m.instance_kind()) as HTMLSelectElement).disabled).toBe(true);
		expect(screen.queryByLabelText(m.instance_api_key())).toBeNull();
		expect(screen.queryByRole('button', { name: m.instance_save() })).toBeNull();
		expect(put).not.toHaveBeenCalled();
	});

	it('announces the problem document when the API refuses the edit', async () => {
		renderManual(
			vi.spyOn(api, 'PUT').mockRejectedValue(
				new ProblemError({
					type: 'about:blank',
					status: 422,
					title: 'Unprocessable Entity',
					detail: 'base_url: must be an http(s) URL'
				})
			)
		);

		await submit();

		expect((await screen.findByRole('alert')).textContent).toContain(
			'base_url: must be an http(s) URL'
		);
	});
});
