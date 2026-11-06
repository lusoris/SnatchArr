// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, samplePolicy, stubGet } from '#lib/testing/harness.js';
import PresetPicker from './PresetPicker.svelte';

const apiPresets = [
	{
		key: 'gentle',
		missing_per_cycle: 1,
		upgrade_per_cycle: 0,
		cycle_interval_s: 900,
		hourly_cap: 4
	},
	{
		key: 'balanced',
		missing_per_cycle: 2,
		upgrade_per_cycle: 1,
		cycle_interval_s: 900,
		hourly_cap: 15
	},
	{
		key: 'catch_up',
		missing_per_cycle: 10,
		upgrade_per_cycle: 2,
		cycle_interval_s: 300,
		hourly_cap: 60,
		selection: 'sequential'
	},
	{
		key: 'upgrades',
		missing_per_cycle: 1,
		upgrade_per_cycle: 5,
		cycle_interval_s: 600,
		hourly_cap: 25
	}
];

describe('PresetPicker', () => {
	it('renders preset title, all preset buttons, and estimate text', async () => {
		const onSelect = vi.fn();
		stubGet({ '/snatch-policy-presets': apiPresets });
		renderWithQuery(PresetPicker, { draft: samplePolicy, onSelect });

		expect(await screen.findByText(m.policy_presets_title())).toBeTruthy();
		expect(
			await screen.findByRole('button', { name: new RegExp(m.preset_gentle_label()) })
		).toBeTruthy();
		expect(
			screen.getByRole('button', { name: new RegExp(m.preset_balanced_label()) })
		).toBeTruthy();
		expect(
			screen.getByRole('button', { name: new RegExp(m.preset_catch_up_label()) })
		).toBeTruthy();
		expect(
			screen.getByRole('button', { name: new RegExp(m.preset_upgrades_label()) })
		).toBeTruthy();

		const estimateEl = screen.getByTestId('estimate-text');
		expect(estimateEl).toBeTruthy();
		expect(estimateEl.textContent).toContain(String(samplePolicy.hourly_cap));
	});

	it('invokes onSelect with preset values when a preset button is clicked', async () => {
		const onSelect = vi.fn();
		stubGet({ '/snatch-policy-presets': apiPresets });
		renderWithQuery(PresetPicker, { draft: samplePolicy, onSelect });

		const gentleBtn = await screen.findByRole('button', {
			name: new RegExp(m.preset_gentle_label())
		});
		await fireEvent.click(gentleBtn);

		expect(onSelect).toHaveBeenCalledOnce();
		expect(onSelect).toHaveBeenCalledWith({
			missing_per_cycle: 1,
			upgrade_per_cycle: 0,
			cycle_interval_s: 900,
			hourly_cap: 4
		});
	});

	it('updates estimate text when draft values vary', async () => {
		const onSelect = vi.fn();
		stubGet({ '/snatch-policy-presets': apiPresets });
		const customDraft = {
			...samplePolicy,
			missing_per_cycle: 10,
			upgrade_per_cycle: 0,
			cycle_interval_s: 600,
			hourly_cap: 60
		};
		renderWithQuery(PresetPicker, { draft: customDraft, onSelect });

		await screen.findByRole('button', { name: new RegExp(m.preset_gentle_label()) });
		const estimateEl = screen.getByTestId('estimate-text');
		// (10 + 0) * 3600 / 600 = 60; min(60, 60) = 60
		expect(estimateEl.textContent).toContain('60');
	});
});
