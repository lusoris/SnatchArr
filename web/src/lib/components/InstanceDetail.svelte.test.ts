// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import {
	failedInstance,
	forkInstance,
	renderWithQuery,
	sampleFilterOptions,
	samplePolicy,
	stubGet,
	untestedInstance,
	type Instance,
	type SnatchPolicy
} from '#lib/testing/harness.js';
import InstanceDetail from './InstanceDetail.svelte';

/** Renders the view for one instance and returns the line under its heading. */
async function subtitleOf(instance: Instance): Promise<string | undefined> {
	stubGet({
		'/instances/{instanceId}': instance,
		'/instances/{instanceId}/policy': samplePolicy,
		'/instances/{instanceId}/policy/filter-options': sampleFilterOptions,
		'/runs': []
	});
	renderWithQuery(InstanceDetail, { id: instance.id });
	const heading = await screen.findByRole('heading', { level: 1, name: instance.name });
	return heading.nextElementSibling?.textContent?.trim();
}

/** The save button of the policy form; the edit form above it has one too. */
function savePolicyButton(): HTMLElement {
	const form = screen.getByRole('form', { name: m.policy_title() });
	return within(form).getByRole('button', { name: m.instance_save() });
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
			if (path === '/instances/{instanceId}/policy/filter-options') {
				return { data: sampleFilterOptions } as never;
			}
			if (path === '/runs') {
				return { data: [] } as never;
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

	it('shows the edit form, the run list and the memory reset of the instance', async () => {
		await subtitleOf(forkInstance);

		expect(screen.getByRole('form', { name: m.instance_edit_title() })).toBeTruthy();
		expect((screen.getByLabelText(m.instance_name()) as HTMLInputElement).value).toBe(
			'Library Readarr'
		);
		expect(await screen.findByText(m.runs_empty())).toBeTruthy();
		expect(screen.getByRole('button', { name: m.memory_reset_one() })).toBeTruthy();
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

	it('saves the picked filters and the release delay with the policy', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: samplePolicy } as never);
		await subtitleOf(forkInstance);

		const include = (await screen.findByLabelText(m.policy_include_tags())) as HTMLSelectElement;
		const kids = [...include.options].find((o) => o.textContent === 'kids');
		if (!kids) throw new Error('tag kids is not offered');
		kids.selected = true;
		await fireEvent.change(include);
		const profiles = screen.getByLabelText(m.policy_exclude_profiles()) as HTMLSelectElement;
		profiles.options[0]!.selected = true;
		await fireEvent.change(profiles);
		await fireEvent.input(screen.getByLabelText(m.policy_release_delay()), {
			target: { value: '12' }
		});
		await fireEvent.click(savePolicyButton());

		expect(put).toHaveBeenCalledOnce();
		const body = (put.mock.calls[0]![1] as { body: SnatchPolicy }).body;
		expect(body.include_tags).toEqual([5]);
		expect(body.exclude_tags).toEqual([]);
		expect(body.include_quality_profiles).toEqual([]);
		expect(body.exclude_quality_profiles).toEqual([4]);
		expect(body.release_delay_h).toBe(12);
	});

	it('keeps the stored filters when the instance cannot list its tags', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: samplePolicy } as never);
		stubGet({
			'/instances/{instanceId}': forkInstance,
			'/instances/{instanceId}/policy': { ...samplePolicy, exclude_tags: [9], release_delay_h: 6 },
			'/instances/{instanceId}/policy/filter-options': new ProblemError({
				type: 'about:blank',
				status: 502,
				detail: 'instance unreachable'
			}),
			'/runs': []
		});
		renderWithQuery(InstanceDetail, { id: forkInstance.id });

		expect((await screen.findByRole('alert')).textContent).toContain('instance unreachable');
		expect(screen.queryByLabelText(m.policy_include_tags())).toBeNull();
		await fireEvent.click(savePolicyButton());

		const body = (put.mock.calls[0]![1] as { body: SnatchPolicy }).body;
		expect(body.exclude_tags).toEqual([9]);
		expect(body.include_tags).toEqual([]);
		expect(body.release_delay_h).toBe(6);
	});

	it('says so when the instance has no tags or quality profiles', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance,
			'/instances/{instanceId}/policy': samplePolicy,
			'/instances/{instanceId}/policy/filter-options': { tags: [], quality_profiles: [] },
			'/runs': []
		});
		renderWithQuery(InstanceDetail, { id: forkInstance.id });

		expect(await screen.findAllByText(m.policy_filters_none())).toHaveLength(4);
		expect(screen.queryByRole('listbox')).toBeNull();
		expect(screen.getByLabelText(m.policy_release_delay())).toBeTruthy();
	});

	it('toggles and saves the dry_run policy setting', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: samplePolicy } as never);
		await subtitleOf(forkInstance);

		const checkbox = (await screen.findByRole('checkbox', {
			name: m.policy_dry_run()
		})) as HTMLInputElement;
		expect(checkbox.checked).toBe(false);

		await fireEvent.click(checkbox);
		expect(checkbox.checked).toBe(true);

		await fireEvent.click(savePolicyButton());

		expect(put).toHaveBeenCalledOnce();
		const body = (put.mock.calls[0]![1] as { body: SnatchPolicy }).body;
		expect(body.dry_run).toBe(true);
	});
});
