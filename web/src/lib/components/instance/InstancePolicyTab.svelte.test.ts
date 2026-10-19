// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import {
	forkInstance,
	renderWithQuery,
	sampleFilterOptions,
	samplePolicy,
	stubGet,
	untestedInstance,
	type SnatchPolicy
} from '#lib/testing/harness.js';
import InstancePolicyTab from './InstancePolicyTab.svelte';

function savePolicyButton(): HTMLElement {
	const form = screen.getByRole('form', { name: m.policy_title() });
	return within(form).getByRole('button', { name: m.instance_save() });
}

describe('InstancePolicyTab', () => {
	afterEach(() => {
		vi.restoreAllMocks();
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
		renderWithQuery(InstancePolicyTab, { id: forkInstance.id });

		expect((await screen.findByRole('alert')).textContent).toContain('store down');
		expect(screen.queryByRole('heading', { level: 2 })).toBeNull();
	});

	it('saves the picked filters and the release delay with the policy', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: samplePolicy } as never);
		stubGet({
			'/instances/{instanceId}': forkInstance,
			'/instances/{instanceId}/policy': samplePolicy,
			'/instances/{instanceId}/policy/filter-options': sampleFilterOptions
		});
		renderWithQuery(InstancePolicyTab, { id: forkInstance.id });

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
			})
		});
		renderWithQuery(InstancePolicyTab, { id: forkInstance.id });

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
			'/instances/{instanceId}/policy/filter-options': { tags: [], quality_profiles: [] }
		});
		renderWithQuery(InstancePolicyTab, { id: forkInstance.id });

		expect(await screen.findAllByText(m.policy_filters_none())).toHaveLength(4);
		expect(screen.queryByRole('listbox')).toBeNull();
		expect(screen.getByLabelText(m.policy_release_delay())).toBeTruthy();
	});

	it('toggles and saves the dry_run policy setting', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: samplePolicy } as never);
		stubGet({
			'/instances/{instanceId}': forkInstance,
			'/instances/{instanceId}/policy': samplePolicy,
			'/instances/{instanceId}/policy/filter-options': sampleFilterOptions
		});
		renderWithQuery(InstancePolicyTab, { id: forkInstance.id });

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

	it('offers the availability switch on a Radarr instance and saves it off', async () => {
		const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: samplePolicy } as never);
		stubGet({
			'/instances/{instanceId}': untestedInstance,
			'/instances/{instanceId}/policy': samplePolicy,
			'/instances/{instanceId}/policy/filter-options': sampleFilterOptions
		});
		renderWithQuery(InstancePolicyTab, { id: untestedInstance.id });

		const checkbox = (await screen.findByRole('checkbox', {
			name: m.policy_skip_unavailable()
		})) as HTMLInputElement;
		expect(checkbox.checked).toBe(true);

		await fireEvent.click(checkbox);
		await fireEvent.click(savePolicyButton());

		expect(put).toHaveBeenCalledOnce();
		const body = (put.mock.calls[0]![1] as { body: SnatchPolicy }).body;
		expect(body.skip_unavailable_movies).toBe(false);
	});

	it('hides the availability switch on other kinds', async () => {
		stubGet({
			'/instances/{instanceId}': forkInstance,
			'/instances/{instanceId}/policy': samplePolicy,
			'/instances/{instanceId}/policy/filter-options': sampleFilterOptions
		});
		renderWithQuery(InstancePolicyTab, { id: forkInstance.id });

		await screen.findByRole('checkbox', { name: m.policy_dry_run() });
		expect(screen.queryByRole('checkbox', { name: m.policy_skip_unavailable() })).toBeNull();
	});
});
