// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, waitFor } from '@testing-library/svelte';
import { toast } from 'svelte-sonner';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { forkInstance, renderWithQuery, sampleQueuePolicy, stubGet } from '#lib/testing/harness.js';
import InstanceQueueTab from './InstanceQueueTab.svelte';

describe('InstanceQueueTab', () => {
	beforeEach(() => {
		session.user = { id: 'u1', username: 'admin', role: 'admin' };
	});

	afterEach(() => {
		session.user = null;
		vi.restoreAllMocks();
	});

	it('renders queue policy and strikes components', async () => {
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy,
			'/instances/{instanceId}/queue-strikes': []
		});
		renderWithQuery(InstanceQueueTab, { id: forkInstance.id });

		expect(
			await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() })
		).toBeTruthy();
		expect(screen.getByRole('heading', { level: 2, name: m.queue_strikes_title() })).toBeTruthy();
	});

	it('admin sees Sweep now button and clicking calls sweep endpoint', async () => {
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy,
			'/instances/{instanceId}/queue-strikes': []
		});
		const postSpy = vi.spyOn(api, 'POST').mockResolvedValue({ data: undefined } as never);
		const toastSpy = vi.spyOn(toast, 'success');
		renderWithQuery(InstanceQueueTab, { id: forkInstance.id });

		const btn = await screen.findByRole('button', { name: m.queue_sweep_now() });
		await fireEvent.click(btn);

		await waitFor(() => {
			expect(postSpy).toHaveBeenCalledWith('/instances/{instanceId}/queue/sweep', {
				params: { path: { instanceId: forkInstance.id } }
			});
		});
		expect(toastSpy).toHaveBeenCalledWith(m.queue_sweep_done());
	});

	it('shows error toast when sweep fails', async () => {
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy,
			'/instances/{instanceId}/queue-strikes': []
		});
		vi.spyOn(api, 'POST').mockRejectedValue(new Error('upstream unreachable'));
		const errToastSpy = vi.spyOn(toast, 'error');
		renderWithQuery(InstanceQueueTab, { id: forkInstance.id });

		const btn = await screen.findByRole('button', { name: m.queue_sweep_now() });
		await fireEvent.click(btn);

		await waitFor(() => {
			expect(errToastSpy).toHaveBeenCalledWith('upstream unreachable');
		});
	});

	it('viewer does not see Sweep now button', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' };
		stubGet({
			'/instances/{instanceId}/queue-policy': sampleQueuePolicy,
			'/instances/{instanceId}/queue-strikes': []
		});
		renderWithQuery(InstanceQueueTab, { id: forkInstance.id });

		await screen.findByRole('heading', { level: 2, name: m.queue_policy_title() });
		expect(screen.queryByRole('button', { name: m.queue_sweep_now() })).toBeNull();
	});
});
