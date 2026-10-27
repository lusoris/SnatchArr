// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { createSentioMutation } from '@sveltesentio/query';
import { toast } from 'svelte-sonner';
import { z } from 'zod';
import { api, describeError } from './api/client.js';
import type { components } from './api/schema.js';
import { APP_KINDS, type AppKind } from './kinds.js';
import { m } from './paraglide/messages.js';
import { keys } from './query.js';

export type Settings = components['schemas']['Settings'];
export type PriorityPresetMap = components['schemas']['PriorityPresetMap'];
export type LlmWriteTool = components['schemas']['LlmWriteTool'];
export type BrandMark = components['schemas']['BrandMark'];
export type SfwMode = components['schemas']['SfwMode'];

export const rankSchema = z.number().int().min(1).max(100);

export const presetFields = Object.fromEntries(APP_KINDS.map((k) => [k, rankSchema])) as Record<
	AppKind,
	typeof rankSchema
>;

export const presetMapSchema = z.object(presetFields) satisfies z.ZodType<PriorityPresetMap>;

export const capsSettingsSchema = z.object({
	global_hourly_cap: z.number().int().min(0).max(5000),
	search_priority_preset: presetMapSchema,
	download_priority_preset: presetMapSchema
}) satisfies z.ZodType<
	Pick<Settings, 'global_hourly_cap' | 'search_priority_preset' | 'download_priority_preset'>
>;

export type CapsSettingsDraft = {
	global_hourly_cap: number;
	search_priority_preset: PriorityPresetMap;
	download_priority_preset: PriorityPresetMap;
};

export const generalSettingsSchema = z.object({
	history_retention_days: z.number().int().min(1).max(3650),
	user_agent: z.string().min(1).max(200),
	llm_enabled: z.boolean(),
	llm_write_tools: z.array(z.string() as z.ZodType<LlmWriteTool>).optional(),
	brand_mark: z.enum(['vulva', 'penis', 'hand']).optional(),
	sfw_mode: z.enum(['auto', 'on', 'off']).optional()
}) satisfies z.ZodType<
	Pick<Settings, 'history_retention_days' | 'user_agent' | 'llm_enabled'> & {
		llm_write_tools?: LlmWriteTool[];
		brand_mark?: BrandMark;
		sfw_mode?: SfwMode;
	}
>;

export type GeneralSettingsDraft = {
	history_retention_days: number;
	user_agent: string;
	llm_enabled: boolean;
	llm_write_tools?: LlmWriteTool[];
	brand_mark?: BrandMark;
	sfw_mode?: SfwMode;
};

/** Merges caps and preset draft into server settings, preserving all other fields. */
export function mergeCapsSettings(server: Settings, draft: CapsSettingsDraft): Settings {
	return {
		...server,
		global_hourly_cap: draft.global_hourly_cap,
		search_priority_preset: { ...draft.search_priority_preset },
		download_priority_preset: { ...draft.download_priority_preset }
	};
}

/** Merges general settings draft into server settings, preserving caps and presets. */
export function mergeGeneralSettings(server: Settings, draft: GeneralSettingsDraft): Settings {
	return {
		...server,
		history_retention_days: draft.history_retention_days,
		user_agent: draft.user_agent,
		llm_enabled: draft.llm_enabled,
		llm_write_tools: draft.llm_write_tools ? [...draft.llm_write_tools] : undefined,
		brand_mark: draft.brand_mark,
		sfw_mode: draft.sfw_mode
	};
}

/** Reads the current settings, applies one form's draft with `merge` and saves the result, so a
 *  form never writes back stale values of fields it does not own. */
export async function saveMergedSettings<D>(
	draft: D,
	merge: (server: Settings, draft: D) => Settings
): Promise<Settings | undefined> {
	const server = (await api.GET('/settings')).data;
	if (!server) throw new Error('GET /settings returned no body');
	return (await api.PUT('/settings', { body: merge(server, draft) })).data;
}

/** Creates a mutation that applies one form's draft with `merge` and saves merged settings. */
export function createSettingsSave<D>(merge: (server: Settings, draft: D) => Settings) {
	return createSentioMutation({
		mutationFn: (d: D) => saveMergedSettings(d, merge),
		invalidates: [keys.settings, keys.caps, keys.instances],
		onSuccess: () => toast.success(m.settings_saved()),
		onError: (err) => toast.error(describeError(err))
	});
}
