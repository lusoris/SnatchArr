// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, within } from '@testing-library/svelte';
import { ProblemError } from '@sveltesentio/core';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { kindLabel } from '#lib/kinds.js';
import { m } from '#lib/paraglide/messages.js';
import { session } from '#lib/session.svelte.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import CapsPresetsForm from './CapsPresetsForm.svelte';

type Settings = components['schemas']['Settings'];
type PriorityPreset = components['schemas']['PriorityPreset'];

const WCAG_22_AA_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

const mediaFirstRanks = {
	sonarr: 10,
	radarr: 10,
	lidarr: 20,
	readarr: 30,
	whisparr_v2: 40,
	whisparr_v3: 40
};

const samplePresets: PriorityPreset[] = [
	{
		name: 'media_first',
		search: mediaFirstRanks,
		download: mediaFirstRanks
	}
];

const sampleSettings: Settings = {
	history_retention_days: 30,
	user_agent: 'SnatchArr/1.0',
	global_hourly_cap: 100,
	search_priority_preset: { ...mediaFirstRanks },
	download_priority_preset: { ...mediaFirstRanks },
	llm_enabled: false,
	llm_write_tools: [],
	updated_at: '2026-10-25T05:00:00Z'
};

const baseStubs = {
	'/settings': sampleSettings,
	'/priority-presets': samplePresets
};

describe('CapsPresetsForm', () => {
	afterEach(() => {
		vi.restoreAllMocks();
		session.user = { id: 'u1', username: 'admin', role: 'admin' } as never;
	});

	it('renders cap field, priority preset sliders, and context help panel', async () => {
		stubGet(baseStubs);
		renderWithQuery(CapsPresetsForm);

		const cap = (await screen.findByLabelText(m.settings_global_cap())) as HTMLInputElement;
		expect(cap.value).toBe('100');
		expect(screen.getByRole('button', { name: m.instance_save() })).toBeTruthy();
		expect(screen.getByText(m.automation_caps_help_title())).toBeTruthy();
		expect(screen.getByText(m.automation_caps_help_desc())).toBeTruthy();
	});

	it('announces the problem document when the settings query fails', async () => {
		stubGet({
			...baseStubs,
			'/settings': new ProblemError({
				type: 'about:blank',
				status: 500,
				detail: 'failed to read settings'
			})
		});
		renderWithQuery(CapsPresetsForm);

		expect((await screen.findByRole('alert')).textContent).toContain('failed to read settings');
		expect(screen.queryByRole('button', { name: m.instance_save() })).toBeNull();
	});

	it('submits updated cap to /settings merging with server copy', async () => {
		stubGet(baseStubs);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: { ...sampleSettings, global_hourly_cap: 200 }
		} as never);
		renderWithQuery(CapsPresetsForm);

		const cap = await screen.findByLabelText(m.settings_global_cap());
		await fireEvent.input(cap, { target: { value: '200' } });

		const button = screen.getByRole('button', { name: m.instance_save() });
		await fireEvent.click(button);

		expect(putSpy).toHaveBeenCalledWith('/settings', {
			body: {
				...sampleSettings,
				global_hourly_cap: 200
			}
		});
	});

	it('resets priority presets to shipped media_first preset when reset button is clicked', async () => {
		stubGet({
			...baseStubs,
			'/settings': {
				...sampleSettings,
				search_priority_preset: { ...mediaFirstRanks, sonarr: 55 },
				download_priority_preset: { ...mediaFirstRanks, whisparr_v3: 5 }
			}
		});
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: sampleSettings
		} as never);
		renderWithQuery(CapsPresetsForm);

		const searchFieldset = (await screen.findByRole('group', {
			name: m.settings_search_priority_preset()
		})) as HTMLElement;
		const downloadFieldset = screen.getByRole('group', {
			name: m.settings_download_priority_preset()
		}) as HTMLElement;

		const sonarrSearch = within(searchFieldset).getByLabelText(
			kindLabel('sonarr')
		) as HTMLInputElement;
		const whisparrDownload = within(downloadFieldset).getByLabelText(
			kindLabel('whisparr_v3')
		) as HTMLInputElement;
		expect(sonarrSearch.value).toBe('55');
		expect(whisparrDownload.value).toBe('5');

		const resetBtn = await screen.findByRole('button', { name: m.settings_priority_reset() });
		await fireEvent.click(resetBtn);

		expect(sonarrSearch.value).toBe('10');
		expect(whisparrDownload.value).toBe('40');

		await fireEvent.click(screen.getByRole('button', { name: m.instance_save() }));
		expect(putSpy).toHaveBeenCalledWith('/settings', {
			body: {
				...sampleSettings,
				search_priority_preset: mediaFirstRanks,
				download_priority_preset: mediaFirstRanks
			}
		});
	});

	it('rejects out-of-bounds priority rank (0 and 101) and accepts boundary ranks (1 and 100) under axe', async () => {
		stubGet(baseStubs);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: sampleSettings
		} as never);
		const { container } = renderWithQuery(CapsPresetsForm);

		const searchFieldset = (await screen.findByRole('group', {
			name: m.settings_search_priority_preset()
		})) as HTMLElement;
		const downloadFieldset = screen.getByRole('group', {
			name: m.settings_download_priority_preset()
		}) as HTMLElement;

		const sonarrSearch = within(searchFieldset).getByLabelText(
			kindLabel('sonarr')
		) as HTMLInputElement;
		const radarrDownload = within(downloadFieldset).getByLabelText(
			kindLabel('radarr')
		) as HTMLInputElement;

		const axeResults = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(axeResults.violations).toEqual([]);

		// Negative: 0 and 101 refused
		for (const invalid of ['0', '101']) {
			await fireEvent.input(sonarrSearch, { target: { value: invalid } });
			expect(sonarrSearch.checkValidity()).toBe(false);
			await fireEvent.click(screen.getByRole('button', { name: m.instance_save() }));
			expect(putSpy).not.toHaveBeenCalled();
		}

		// Boundary: 1 and 100 accepted
		await fireEvent.input(sonarrSearch, { target: { value: '1' } });
		await fireEvent.input(radarrDownload, { target: { value: '100' } });
		expect(sonarrSearch.checkValidity()).toBe(true);
		expect(radarrDownload.checkValidity()).toBe(true);

		await fireEvent.click(screen.getByRole('button', { name: m.instance_save() }));
		expect(putSpy).toHaveBeenCalledWith('/settings', {
			body: {
				...sampleSettings,
				search_priority_preset: { ...mediaFirstRanks, sonarr: 1 },
				download_priority_preset: { ...mediaFirstRanks, radarr: 100 }
			}
		});
	});

	it('validates boundaries on global_hourly_cap: 0 and 5000 accepted, negative and 5001 refused', async () => {
		stubGet(baseStubs);
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: sampleSettings
		} as never);
		renderWithQuery(CapsPresetsForm);

		const cap = (await screen.findByLabelText(m.settings_global_cap())) as HTMLInputElement;

		// Boundary: 0 accepted
		await fireEvent.input(cap, { target: { value: '0' } });
		expect(cap.checkValidity()).toBe(true);

		// Boundary: 5000 accepted
		await fireEvent.input(cap, { target: { value: '5000' } });
		expect(cap.checkValidity()).toBe(true);

		// Negative: 5001 refused
		await fireEvent.input(cap, { target: { value: '5001' } });
		expect(cap.checkValidity()).toBe(false);

		// Negative: -1 refused
		await fireEvent.input(cap, { target: { value: '-1' } });
		expect(cap.checkValidity()).toBe(false);

		// Try saving with 5001
		await fireEvent.input(cap, { target: { value: '5001' } });
		await fireEvent.click(screen.getByRole('button', { name: m.instance_save() }));
		expect(putSpy).not.toHaveBeenCalled();
	});

	it('renders read-only view for viewer role: inputs and reset disabled, save button omitted', async () => {
		session.user = { id: 'u2', username: 'viewer', role: 'viewer' } as never;
		stubGet(baseStubs);
		renderWithQuery(CapsPresetsForm);

		const cap = (await screen.findByLabelText(m.settings_global_cap())) as HTMLInputElement;
		expect(cap.disabled).toBe(true);

		const searchFieldset = (await screen.findByRole('group', {
			name: m.settings_search_priority_preset()
		})) as HTMLElement;
		const sonarrSearch = within(searchFieldset).getByLabelText(
			kindLabel('sonarr')
		) as HTMLInputElement;
		expect(sonarrSearch.disabled).toBe(true);

		const resetBtn = screen.getByRole('button', {
			name: m.settings_priority_reset()
		}) as HTMLButtonElement;
		expect(resetBtn.disabled).toBe(true);

		expect(screen.queryByRole('button', { name: m.instance_save() })).toBeNull();
	});
});
