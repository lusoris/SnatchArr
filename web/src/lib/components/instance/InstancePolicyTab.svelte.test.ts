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
	type Instance,
	type SnatchPolicy
} from '#lib/testing/harness.js';
import InstancePolicyTab from './InstancePolicyTab.svelte';

function savePolicyButton(): HTMLElement {
	const form = screen.getByRole('form', { name: m.policy_title() });
	return within(form).getByRole('button', { name: m.instance_save() });
}

function setupPolicyTab(instance: Instance = forkInstance) {
	const put = vi.spyOn(api, 'PUT').mockResolvedValue({ data: samplePolicy } as never);
	stubGet({
		'/instances/{instanceId}': instance,
		'/instances/{instanceId}/policy': samplePolicy,
		'/instances/{instanceId}/policy/filter-options': sampleFilterOptions
	});
	const renderResult = renderWithQuery(InstancePolicyTab, { id: instance.id });
	return { put, ...renderResult };
}

async function saveAndExpectBody(put: ReturnType<typeof vi.spyOn>): Promise<SnatchPolicy> {
	await fireEvent.click(savePolicyButton());
	expect(put).toHaveBeenCalledOnce();
	return (put.mock.calls[0]![1] as { body: SnatchPolicy }).body;
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
		const { put } = setupPolicyTab();

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
		const body = await saveAndExpectBody(put);
		expect(body.include_tags).toEqual([5]);
		expect(body.exclude_tags).toEqual([]);
		expect(body.include_quality_profiles).toEqual([]);
		expect(body.exclude_quality_profiles).toEqual([4]);
		expect(body.release_delay_h).toBe(12);
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
		const { put } = setupPolicyTab();

		const checkbox = (await screen.findByRole('checkbox', {
			name: m.policy_dry_run()
		})) as HTMLInputElement;
		expect(checkbox.checked).toBe(false);

		await fireEvent.click(checkbox);
		expect(checkbox.checked).toBe(true);

		const body = await saveAndExpectBody(put);
		expect(body.dry_run).toBe(true);
	});

	it('offers the availability switch on a Radarr instance and saves it off', async () => {
		const { put } = setupPolicyTab(untestedInstance);

		const checkbox = (await screen.findByRole('checkbox', {
			name: m.policy_skip_unavailable()
		})) as HTMLInputElement;
		expect(checkbox.checked).toBe(true);

		await fireEvent.click(checkbox);
		const body = await saveAndExpectBody(put);
		expect(body.skip_unavailable_movies).toBe(false);
	});

	it('hides the availability switch on other kinds', async () => {
		setupPolicyTab();

		await screen.findByRole('checkbox', { name: m.policy_dry_run() });
		expect(screen.queryByRole('checkbox', { name: m.policy_skip_unavailable() })).toBeNull();
	});

	it('fills draft from a chosen preset without saving until save button is clicked', async () => {
		const { put } = setupPolicyTab();

		const gentleBtn = await screen.findByRole('button', {
			name: new RegExp(m.preset_gentle_label())
		});
		await fireEvent.click(gentleBtn);

		// Before clicking save, PUT has not been called
		expect(put).not.toHaveBeenCalled();

		// Check that form input reflects preset: missing=1
		const missingInput = screen.getByLabelText(m.policy_missing_per_cycle()) as HTMLInputElement;
		expect(missingInput.value).toBe('1');

		const body = await saveAndExpectBody(put);
		expect(body.missing_per_cycle).toBe(1);
		expect(body.upgrade_per_cycle).toBe(0);
		expect(body.cycle_interval_s).toBe(900);
		expect(body.hourly_cap).toBe(4);
	});

	it('shows app-specific options for Sonarr and Lidarr and saves them', async () => {
		const sonarrInstance = { ...forkInstance, id: 'sonarr-1', kind: 'sonarr' as const };
		const { put, unmount } = setupPolicyTab(sonarrInstance);

		const missingSelect = (await screen.findByLabelText(
			m.policy_sonarr_missing_mode()
		)) as HTMLSelectElement;
		await fireEvent.change(missingSelect, { target: { value: 'shows' } });
		await fireEvent.click(savePolicyButton());

		expect(put).toHaveBeenCalledOnce();
		const sonarrBody = (put.mock.calls[0]![1] as { body: SnatchPolicy }).body;
		expect(sonarrBody.sonarr_missing_mode).toBe('shows');
		unmount();

		const lidarrInstance = { ...forkInstance, id: 'lidarr-1', kind: 'lidarr' as const };
		stubGet({
			'/instances/{instanceId}': lidarrInstance,
			'/instances/{instanceId}/policy': samplePolicy,
			'/instances/{instanceId}/policy/filter-options': sampleFilterOptions
		});
		renderWithQuery(InstancePolicyTab, { id: lidarrInstance.id });

		const lidarrSelect = (await screen.findByLabelText(
			m.policy_lidarr_missing_mode()
		)) as HTMLSelectElement;
		await fireEvent.change(lidarrSelect, { target: { value: 'album' } });
		await fireEvent.click(savePolicyButton());

		expect(put).toHaveBeenCalledTimes(2);
		const lidarrBody = (put.mock.calls[1]![1] as { body: SnatchPolicy }).body;
		expect(lidarrBody.lidarr_missing_mode).toBe('album');
	});
});
