// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import {
	configarrInstance,
	failedInstance,
	forkInstance,
	renderWithQuery,
	stubGet,
	untestedInstance,
	type Instance
} from '#lib/testing/harness.js';
import InstanceOverview from './InstanceOverview.svelte';

type InstancePriority = components['schemas']['InstancePriority'];

const samplePriority: InstancePriority = {
	search_priority: null,
	download_priority: null,
	effective_search_priority: 30,
	effective_download_priority: 30,
	search_source: 'preset',
	download_source: 'preset'
};

describe('InstanceOverview', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('shows CheckState and passed connection test of the instance', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance,
			'/instances/{instanceId}/priority': samplePriority
		});
		renderWithQuery(InstanceOverview, { id: forkInstance.id });

		const lastCheck = new RegExp(m.instance_last_check());
		expect((await screen.findByText(lastCheck)).textContent).toContain('0.4.21.182');
		expect(screen.queryByRole('status')).toBeNull();
	});

	it('shows an instance whose connection test failed', async () => {
		stubGet({
			'/instances/{instanceId}': failedInstance,
			'/instances/{instanceId}/priority': samplePriority
		});
		renderWithQuery(InstanceOverview, { id: failedInstance.id });

		const failed = await screen.findByRole('status');
		expect(failed.textContent).toContain('Connection refused (ECONNREFUSED)');
	});

	it('shows an instance no connection test has reached', async () => {
		stubGet({
			'/instances/{instanceId}': untestedInstance,
			'/instances/{instanceId}/priority': samplePriority
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
			if (path === '/instances/{instanceId}/priority') {
				return { data: samplePriority } as never;
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
			'/instances/{instanceId}': forkInstance,
			'/instances/{instanceId}/priority': samplePriority
		});
		renderWithQuery(InstanceOverview, { id: forkInstance.id });

		expect(await screen.findByRole('form', { name: m.instance_edit_title() })).toBeTruthy();
		expect((screen.getByLabelText(m.instance_name()) as HTMLInputElement).value).toBe(
			'Library Readarr'
		);
	});

	it('saves and clears priority overrides on the priority card, including Configarr instances', async () => {
		let currentPriority: InstancePriority = {
			search_priority: 5,
			download_priority: 15,
			effective_search_priority: 5,
			effective_download_priority: 15,
			search_source: 'instance',
			download_source: 'instance'
		};
		vi.spyOn(api, 'GET').mockImplementation(async (path: string) => {
			if (path === '/instances/{instanceId}') {
				return { data: configarrInstance } as never;
			}
			if (path === '/instances/{instanceId}/priority') {
				return { data: currentPriority } as never;
			}
			throw new Error(`GET ${path} has no stubbed answer`);
		});
		const putSpy = vi
			.spyOn(api, 'PUT')
			.mockImplementation(async (path: string, init?: { body?: unknown }) => {
				if (path === '/instances/{instanceId}/priority') {
					const body = init?.body as {
						search_priority: number | null;
						download_priority: number | null;
					};
					currentPriority = {
						search_priority: body.search_priority,
						download_priority: body.download_priority,
						effective_search_priority: body.search_priority ?? 10,
						effective_download_priority: body.download_priority ?? 10,
						search_source: body.search_priority !== null ? 'instance' : 'preset',
						download_source: body.download_priority !== null ? 'instance' : 'preset'
					};
					return { data: currentPriority } as never;
				}
				throw new Error(`PUT ${path} has no stubbed answer`);
			});

		renderWithQuery(InstanceOverview, { id: configarrInstance.id });

		const searchInput = (await screen.findByLabelText(
			m.instance_priority_search()
		)) as HTMLInputElement;
		const downloadInput = screen.getByLabelText(m.instance_priority_download()) as HTMLInputElement;

		expect(searchInput.value).toBe('5');
		expect(downloadInput.value).toBe('15');
		expect(
			await screen.findByText(
				m.instance_priority_effective_search({
					rank: 5,
					source: m.instance_priority_source_instance()
				})
			)
		).toBeTruthy();

		// Negative: 0 and 101 refused
		await fireEvent.input(searchInput, { target: { value: '0' } });
		expect(searchInput.checkValidity()).toBe(false);
		await fireEvent.click(screen.getByRole('button', { name: m.instance_priority_save() }));
		expect(putSpy).not.toHaveBeenCalled();

		// Boundary: 1 and 100 accepted
		await fireEvent.input(searchInput, { target: { value: '1' } });
		await fireEvent.input(downloadInput, { target: { value: '100' } });
		await fireEvent.click(screen.getByRole('button', { name: m.instance_priority_save() }));
		expect(putSpy).toHaveBeenCalledWith('/instances/{instanceId}/priority', {
			params: { path: { instanceId: configarrInstance.id } },
			body: { search_priority: 1, download_priority: 100 }
		});

		const clearBtn = screen.getByRole('button', { name: m.instance_priority_clear() });
		await fireEvent.click(clearBtn);

		expect(putSpy).toHaveBeenLastCalledWith('/instances/{instanceId}/priority', {
			params: { path: { instanceId: configarrInstance.id } },
			body: { search_priority: null, download_priority: null }
		});
		expect(searchInput.value).toBe('');
		expect(downloadInput.value).toBe('');
		expect(
			await screen.findByText(
				m.instance_priority_effective_search({
					rank: 10,
					source: m.instance_priority_source_preset()
				})
			)
		).toBeTruthy();
	});

	it('renders the priority card without axe violations', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance,
			'/instances/{instanceId}/priority': samplePriority
		});
		const { container } = renderWithQuery(InstanceOverview, { id: forkInstance.id });
		await screen.findByRole('heading', { name: m.instance_priority_title() });

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});
});
