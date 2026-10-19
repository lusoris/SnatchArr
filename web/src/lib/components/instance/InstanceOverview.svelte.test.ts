// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import {
	failedInstance,
	forkInstance,
	renderWithQuery,
	stubGet,
	untestedInstance,
	type Instance
} from '#lib/testing/harness.js';
import InstanceOverview from './InstanceOverview.svelte';

describe('InstanceOverview', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('shows CheckState and passed connection test of the instance', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance
		});
		renderWithQuery(InstanceOverview, { id: forkInstance.id });

		const lastCheck = new RegExp(m.instance_last_check());
		expect((await screen.findByText(lastCheck)).textContent).toContain('0.4.21.182');
		expect(screen.queryByRole('status')).toBeNull();
	});

	it('shows an instance whose connection test failed', async () => {
		stubGet({
			'/instances/{instanceId}': failedInstance
		});
		renderWithQuery(InstanceOverview, { id: failedInstance.id });

		const failed = await screen.findByRole('status');
		expect(failed.textContent).toContain('Connection refused (ECONNREFUSED)');
	});

	it('shows an instance no connection test has reached', async () => {
		stubGet({
			'/instances/{instanceId}': untestedInstance
		});
		renderWithQuery(InstanceOverview, { id: untestedInstance.id });

		expect(await screen.findByText(m.instance_never_tested())).toBeTruthy();
		expect(screen.queryByRole('status')).toBeNull();
	});

	it('shows the error state without reload after probe fails and refetches', async () => {
		let currentInstance: Instance = { ...forkInstance };
		vi.spyOn(api, 'GET').mockImplementation(async (path: string) => {
			if (path === '/instances/{instanceId}') {
				return { data: currentInstance } as never;
			}
			throw new Error(`GET ${path} has no stubbed answer`);
		});
		vi.spyOn(api, 'POST').mockImplementation(async (path: string) => {
			if (path === '/instances/{instanceId}/test') {
				currentInstance = {
					...currentInstance,
					last_error: 'Connection refused (ECONNREFUSED)',
					last_check_at: '2026-09-01T12:05:00Z'
				};
				throw new ProblemError({
					type: 'about:blank',
					status: 502,
					detail: 'Connection refused (ECONNREFUSED)'
				});
			}
			throw new Error(`POST ${path} has no stubbed answer`);
		});

		renderWithQuery(InstanceOverview, { id: forkInstance.id });

		const lastCheck = new RegExp(m.instance_last_check());
		expect(await screen.findByText(lastCheck)).toBeTruthy();
		expect(screen.queryByRole('status')).toBeNull();

		const testBtn = screen.getByRole('button', { name: m.instance_test() });
		await fireEvent.click(testBtn);

		const failed = await screen.findByRole('status');
		expect(failed.textContent).toContain('Connection refused (ECONNREFUSED)');
	});

	it('shows the edit form of the instance', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance
		});
		renderWithQuery(InstanceOverview, { id: forkInstance.id });

		expect(await screen.findByRole('form', { name: m.instance_edit_title() })).toBeTruthy();
		expect((screen.getByLabelText(m.instance_name()) as HTMLInputElement).value).toBe(
			'Library Readarr'
		);
	});
});
