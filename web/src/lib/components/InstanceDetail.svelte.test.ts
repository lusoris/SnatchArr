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
	samplePolicy,
	stubGet,
	untestedInstance,
	type Instance
} from '#lib/testing/harness.js';
import InstanceDetail from './InstanceDetail.svelte';

/** Renders the view for one instance and returns the line under its heading. */
async function subtitleOf(instance: Instance): Promise<string | undefined> {
	stubGet({ '/instances/{instanceId}': instance, '/instances/{instanceId}/policy': samplePolicy });
	renderWithQuery(InstanceDetail, { id: instance.id });
	const heading = await screen.findByRole('heading', { level: 1, name: instance.name });
	return heading.nextElementSibling?.textContent?.trim();
}

describe('InstanceDetail', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('shows name, kind with the fork, address and passed connection test of the instance', async () => {
		expect(await subtitleOf(forkInstance)).toBe('readarr · Bookshelf · http://readarr.local:8787');
		const lastCheck = new RegExp(m.instance_last_check());
		expect(screen.getByText(lastCheck).textContent).toContain('0.4.21.182');
		expect(screen.queryByRole('status')).toBeNull();
	});

	it('shows an instance whose connection test failed', async () => {
		expect(await subtitleOf(failedInstance)).toBe('sonarr · http://sonarr.local:8989');
		const failed = await screen.findByRole('status');
		expect(failed.textContent).toContain('Connection refused (ECONNREFUSED)');
	});

	it('shows an instance no connection test has reached', async () => {
		expect(await subtitleOf(untestedInstance)).toBe('radarr · http://radarr.local:7878');
		expect(await screen.findByText(m.instance_never_tested())).toBeTruthy();
		expect(screen.queryByRole('status')).toBeNull();
	});

	it('shows the error state without reload after probe fails and refetches', async () => {
		let currentInstance: Instance = { ...forkInstance };
		vi.spyOn(api, 'GET').mockImplementation(async (path: string) => {
			if (path === '/instances/{instanceId}') {
				return { data: currentInstance } as never;
			}
			if (path === '/instances/{instanceId}/policy') {
				return { data: samplePolicy } as never;
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

		renderWithQuery(InstanceDetail, { id: forkInstance.id });

		const lastCheck = new RegExp(m.instance_last_check());
		expect(await screen.findByText(lastCheck)).toBeTruthy();
		expect(screen.queryByRole('status')).toBeNull();

		const testBtn = screen.getByRole('button', { name: m.instance_test() });
		await fireEvent.click(testBtn);

		const failed = await screen.findByRole('status');
		expect(failed.textContent).toContain('Connection refused (ECONNREFUSED)');
	});

	it('announces the problem document when the instance does not exist', async () => {
		stubGet({
			'/instances/{instanceId}': new ProblemError({
				type: 'about:blank',
				status: 404,
				detail: 'Instance not found'
			}),
			'/instances/{instanceId}/policy': samplePolicy
		});
		renderWithQuery(InstanceDetail, { id: 'missing-id' });

		expect((await screen.findByRole('alert')).textContent).toContain('Instance not found');
		expect(screen.queryByRole('heading', { level: 1 })).toBeNull();
	});

	it('announces the problem document when the policy is refused', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance,
			'/instances/{instanceId}/policy': new ProblemError({
				type: 'about:blank',
				status: 503,
				detail: 'store down'
			})
		});
		renderWithQuery(InstanceDetail, { id: forkInstance.id });

		expect((await screen.findByRole('alert')).textContent).toContain('store down');
		expect(screen.queryByRole('heading', { level: 1 })).toBeNull();
	});
});
