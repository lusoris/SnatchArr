// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { samplePolicy } from '#lib/testing/harness.js';
import PresetPicker from './PresetPicker.svelte';
import { PRESETS } from './presets.js';

describe('PresetPicker', () => {
	it('renders preset title, all preset buttons, and estimate text', () => {
		const onSelect = vi.fn();
		render(PresetPicker, { draft: samplePolicy, onSelect });

		expect(screen.getByText(m.policy_presets_title())).toBeTruthy();
		for (const preset of PRESETS) {
			expect(screen.getByRole('button', { name: new RegExp(preset.label()) })).toBeTruthy();
		}

		const estimateEl = screen.getByTestId('estimate-text');
		expect(estimateEl).toBeTruthy();
		expect(estimateEl.textContent).toContain(String(samplePolicy.hourly_cap));
	});

	it('invokes onSelect with preset values when a preset button is clicked', async () => {
		const onSelect = vi.fn();
		render(PresetPicker, { draft: samplePolicy, onSelect });

		const gentleBtn = screen.getByRole('button', { name: new RegExp(m.preset_gentle_label()) });
		await fireEvent.click(gentleBtn);

		expect(onSelect).toHaveBeenCalledOnce();
		expect(onSelect).toHaveBeenCalledWith({
			missing_per_cycle: 1,
			upgrade_per_cycle: 0,
			cycle_interval_s: 900,
			hourly_cap: 4
		});
	});

	it('updates estimate text when draft values vary', () => {
		const onSelect = vi.fn();
		const customDraft = {
			...samplePolicy,
			missing_per_cycle: 10,
			upgrade_per_cycle: 0,
			cycle_interval_s: 600,
			hourly_cap: 60
		};
		render(PresetPicker, { draft: customDraft, onSelect });

		const estimateEl = screen.getByTestId('estimate-text');
		// (10 + 0) * 3600 / 600 = 60; min(60, 60) = 60
		expect(estimateEl.textContent).toContain('60');
	});
});
