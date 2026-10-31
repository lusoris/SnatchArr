// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { z } from 'zod';
import { api } from './api/client.js';
import type { components } from './api/schema.js';
import { presetMapSchema } from './settings.js';

export type NamedPriorityPreset = components['schemas']['PriorityPreset'];
export type PriorityPresetInput = components['schemas']['PriorityPresetInput'];

export const namedPriorityPresetSchema = z.object({
	name: z.string().trim().min(1).max(40),
	search: presetMapSchema,
	download: presetMapSchema
}) satisfies z.ZodType<PriorityPresetInput>;

/** Creates a named snapshot; applying it remains an explicit separate action. */
export async function savePriorityPreset(body: PriorityPresetInput) {
	return (await api.POST('/priority-presets', { body })).data;
}

/** Renaming preserves the snapshot's maps, including when they differ from the editor. */
export async function renamePriorityPreset(preset: NamedPriorityPreset, name: string) {
	if (!preset.id || preset.builtin) return;
	return (
		await api.PUT('/priority-presets/{presetId}', {
			params: { path: { presetId: preset.id } },
			body: { name, search: preset.search, download: preset.download }
		})
	).data;
}

/** Copies both maps into settings; media_first is the documented builtin reference. */
export async function applyPriorityPreset(preset: NamedPriorityPreset) {
	return (
		await api.POST('/priority-presets/{presetId}/apply', {
			params: { path: { presetId: preset.id ?? preset.name } }
		})
	).data;
}

/** Deletes a user snapshot without changing active settings. */
export async function deletePriorityPreset(preset: NamedPriorityPreset) {
	if (!preset.id || preset.builtin) return;
	await api.DELETE('/priority-presets/{presetId}', { params: { path: { presetId: preset.id } } });
}
