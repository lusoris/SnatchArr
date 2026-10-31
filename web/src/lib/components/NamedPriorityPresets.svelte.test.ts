// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import { m } from '#lib/paraglide/messages.js';
import { namedPriorityPresetSchema, type NamedPriorityPreset } from '#lib/priority-presets.js';
import { renderWithQuery } from '#lib/testing/harness.js';
import NamedPriorityPresets from './NamedPriorityPresets.svelte';

const ranks = { sonarr: 10, radarr: 10, lidarr: 20, readarr: 30, whisparr_v2: 40, whisparr_v3: 40 };
const builtin: NamedPriorityPreset = {
	name: 'media_first',
	builtin: true,
	search: ranks,
	download: ranks
};
const night: NamedPriorityPreset = {
	...builtin,
	id: 'e4afc611-fdfb-4810-a711-77a0940d91d1',
	name: 'night',
	builtin: false
};
const draft = {
	global_hourly_cap: 100,
	search_priority_preset: { ...ranks, lidarr: 1 },
	download_priority_preset: { ...ranks, sonarr: 100 }
};

describe('named priority presets', () => {
	afterEach(() => vi.restoreAllMocks());

	it('saves both draft maps without applying them', async () => {
		const post = vi.spyOn(api, 'POST').mockResolvedValue({ data: night } as never);
		const onApply = vi.fn();
		renderWithQuery(NamedPriorityPresets, { presets: [builtin], draft, onApply });
		await fireEvent.input(screen.getByLabelText(m.settings_named_preset_name()), {
			target: { value: 'night' }
		});
		await fireEvent.click(screen.getByRole('button', { name: m.settings_named_preset_save() }));
		await waitFor(() =>
			expect(post).toHaveBeenCalledWith('/priority-presets', {
				body: {
					name: 'night',
					search: draft.search_priority_preset,
					download: draft.download_priority_preset
				}
			})
		);
		expect(onApply).not.toHaveBeenCalled();
	});

	it('applies the saved snapshot and updates the editor from returned settings', async () => {
		const settings = {
			...draft,
			search_priority_preset: night.search,
			download_priority_preset: night.download
		};
		const post = vi.spyOn(api, 'POST').mockResolvedValue({ data: settings } as never);
		const onApply = vi.fn();
		renderWithQuery(NamedPriorityPresets, { presets: [builtin, night], draft, onApply });
		await fireEvent.click(
			screen.getByRole('button', { name: m.settings_named_preset_apply_label({ name: 'night' }) })
		);
		await waitFor(() => expect(onApply).toHaveBeenCalledWith(settings));
		expect(post).toHaveBeenCalledWith('/priority-presets/{presetId}/apply', {
			params: { path: { presetId: night.id } }
		});
	});

	it('renames the stored snapshot without replacing it with the current draft', async () => {
		const put = vi
			.spyOn(api, 'PUT')
			.mockResolvedValue({ data: { ...night, name: 'evening' } } as never);
		renderWithQuery(NamedPriorityPresets, { presets: [builtin, night], draft, onApply: vi.fn() });
		await fireEvent.click(
			screen.getByRole('button', { name: m.settings_named_preset_rename_label({ name: 'night' }) })
		);
		const inputs = screen.getAllByLabelText(m.settings_named_preset_name());
		await fireEvent.input(inputs[1], { target: { value: 'evening' } });
		await fireEvent.click(
			screen.getByRole('button', { name: m.settings_named_preset_rename_save() })
		);
		await waitFor(() =>
			expect(put).toHaveBeenCalledWith('/priority-presets/{presetId}', {
				params: { path: { presetId: night.id } },
				body: { name: 'evening', search: night.search, download: night.download }
			})
		);
	});

	it('keeps builtin read-only and stops saving at 20 user presets', () => {
		const presets = [
			builtin,
			...Array.from({ length: 20 }, (_, index) => ({
				...night,
				id: String(index),
				name: `night-${index}`
			}))
		];
		renderWithQuery(NamedPriorityPresets, { presets, draft, onApply: vi.fn() });
		const builtinRow = screen.getByText('media_first').closest('li')!;
		expect(within(builtinRow).queryByRole('button', { name: /Rename/ })).toBeNull();
		expect(within(builtinRow).queryByRole('button', { name: /Delete/ })).toBeNull();
		expect(
			(screen.getByRole('button', { name: m.settings_named_preset_save() }) as HTMLButtonElement)
				.disabled
		).toBe(true);
	});

	it.each([
		['', false],
		['a', true],
		['a'.repeat(40), true],
		['a'.repeat(41), false]
	])('validates name length: %s', (name, valid) => {
		expect(
			namedPriorityPresetSchema.safeParse({ name, search: ranks, download: ranks }).success
		).toBe(valid);
	});
});
