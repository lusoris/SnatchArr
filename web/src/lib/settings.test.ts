// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { afterEach, describe, expect, it, vi } from 'vitest';
import * as sentioQuery from '@sveltesentio/query';
import { toast } from 'svelte-sonner';
import { api } from './api/client.js';
import { m } from './paraglide/messages.js';
import { keys } from './query.js';
import type { PriorityPresetMap, Settings } from './settings.js';
import {
	capsSettingsSchema,
	createSettingsSave,
	generalSettingsSchema,
	mergeCapsSettings,
	mergeGeneralSettings,
	saveMergedSettings
} from './settings.js';

const samplePresetMap: PriorityPresetMap = {
	sonarr: 10,
	radarr: 20,
	lidarr: 30,
	readarr: 40,
	whisparr_v2: 50,
	whisparr_v3: 60
};

const sampleServerSettings: Settings = {
	history_retention_days: 30,
	user_agent: 'SnatchArr/1.0',
	global_hourly_cap: 100,
	search_priority_preset: samplePresetMap,
	download_priority_preset: samplePresetMap,
	llm_enabled: true,
	llm_write_tools: ['snatcharr_create_instance', 'snatcharr_update_instance'],
	updated_at: '2026-10-25T05:00:00Z'
};

function stubSettingsApi(data: Settings) {
	vi.spyOn(api, 'GET').mockResolvedValue({ data } as never);
	return vi.spyOn(api, 'PUT').mockResolvedValue({ data } as never);
}

describe('settings pure helpers and schemas', () => {
	describe('capsSettingsSchema', () => {
		it('positive: accepts valid caps and presets', () => {
			const valid = {
				global_hourly_cap: 150,
				search_priority_preset: samplePresetMap,
				download_priority_preset: samplePresetMap
			};
			const res = capsSettingsSchema.safeParse(valid);
			expect(res.success).toBe(true);
		});

		it('boundary: cap 0 and 5000 accepted', () => {
			for (const cap of [0, 5000]) {
				const res = capsSettingsSchema.safeParse({
					global_hourly_cap: cap,
					search_priority_preset: samplePresetMap,
					download_priority_preset: samplePresetMap
				});
				expect(res.success).toBe(true);
			}
		});

		it('boundary: preset rank 1 and 100 accepted', () => {
			for (const rank of [1, 100]) {
				const res = capsSettingsSchema.safeParse({
					global_hourly_cap: 100,
					search_priority_preset: { ...samplePresetMap, sonarr: rank },
					download_priority_preset: { ...samplePresetMap, radarr: rank }
				});
				expect(res.success).toBe(true);
			}
		});

		it('negative: cap 5001 and negative cap refused by zod schema', () => {
			for (const invalidCap of [-1, 5001]) {
				const res = capsSettingsSchema.safeParse({
					global_hourly_cap: invalidCap,
					search_priority_preset: samplePresetMap,
					download_priority_preset: samplePresetMap
				});
				expect(res.success).toBe(false);
			}
		});

		it('negative: preset rank 0 and 101 refused', () => {
			for (const invalidRank of [0, 101]) {
				const res = capsSettingsSchema.safeParse({
					global_hourly_cap: 100,
					search_priority_preset: { ...samplePresetMap, sonarr: invalidRank },
					download_priority_preset: samplePresetMap
				});
				expect(res.success).toBe(false);
			}
		});

		it('negative: missing required app kind in preset map refused', () => {
			const incomplete = { ...samplePresetMap };
			delete (incomplete as Record<string, number>).whisparr_v3;
			const res = capsSettingsSchema.safeParse({
				global_hourly_cap: 100,
				search_priority_preset: incomplete,
				download_priority_preset: samplePresetMap
			});
			expect(res.success).toBe(false);
		});
	});

	describe('generalSettingsSchema', () => {
		it('positive: accepts valid general settings', () => {
			const res = generalSettingsSchema.safeParse({
				history_retention_days: 90,
				user_agent: 'CustomAgent/2.0',
				llm_enabled: false
			});
			expect(res.success).toBe(true);
		});

		it('boundary: retention 1 and 3650 accepted', () => {
			for (const days of [1, 3650]) {
				const res = generalSettingsSchema.safeParse({
					history_retention_days: days,
					user_agent: 'SnatchArr/1.0',
					llm_enabled: false
				});
				expect(res.success).toBe(true);
			}
		});

		it('negative: retention 0 and 3651 refused', () => {
			for (const invalidDays of [0, 3651]) {
				const res = generalSettingsSchema.safeParse({
					history_retention_days: invalidDays,
					user_agent: 'SnatchArr/1.0',
					llm_enabled: false
				});
				expect(res.success).toBe(false);
			}
		});
	});

	describe('mergeCapsSettings', () => {
		it('updates only caps and presets, preserving all other server fields', () => {
			const updatedPresets: PriorityPresetMap = {
				...samplePresetMap,
				sonarr: 1,
				radarr: 99
			};
			const draft = {
				global_hourly_cap: 250,
				search_priority_preset: updatedPresets,
				download_priority_preset: updatedPresets
			};

			const merged = mergeCapsSettings(sampleServerSettings, draft);

			// Draft values applied
			expect(merged.global_hourly_cap).toBe(250);
			expect(merged.search_priority_preset).toEqual(updatedPresets);
			expect(merged.download_priority_preset).toEqual(updatedPresets);

			// Server values preserved untouched
			expect(merged.history_retention_days).toBe(sampleServerSettings.history_retention_days);
			expect(merged.user_agent).toBe(sampleServerSettings.user_agent);
			expect(merged.llm_enabled).toBe(sampleServerSettings.llm_enabled);
			expect(merged.llm_write_tools).toEqual(sampleServerSettings.llm_write_tools);
			expect(merged.updated_at).toBe(sampleServerSettings.updated_at);
		});
	});

	describe('mergeGeneralSettings', () => {
		it('updates only general settings, preserving caps and presets from server copy', () => {
			const draft = {
				history_retention_days: 180,
				user_agent: 'SnatchArr-Updated/2.0',
				llm_enabled: false,
				llm_write_tools: []
			};

			const merged = mergeGeneralSettings(sampleServerSettings, draft);

			// Draft values applied
			expect(merged.history_retention_days).toBe(180);
			expect(merged.user_agent).toBe('SnatchArr-Updated/2.0');
			expect(merged.llm_enabled).toBe(false);
			expect(merged.llm_write_tools).toEqual([]);

			// Server values preserved untouched
			expect(merged.global_hourly_cap).toBe(sampleServerSettings.global_hourly_cap);
			expect(merged.search_priority_preset).toEqual(sampleServerSettings.search_priority_preset);
			expect(merged.download_priority_preset).toEqual(
				sampleServerSettings.download_priority_preset
			);
			expect(merged.updated_at).toBe(sampleServerSettings.updated_at);
		});
	});

	describe('saveMergedSettings', () => {
		afterEach(() => vi.restoreAllMocks());

		it('positive: puts the fresh server copy merged with the draft', async () => {
			const fresh = { ...sampleServerSettings, user_agent: 'Fresh/2.0' };
			const put = stubSettingsApi(fresh);
			await saveMergedSettings(
				{
					global_hourly_cap: 0,
					search_priority_preset: samplePresetMap,
					download_priority_preset: samplePresetMap
				},
				mergeCapsSettings
			);
			const body = (put.mock.calls[0]?.[1] as { body: Settings }).body;
			expect(body.user_agent).toBe('Fresh/2.0');
			expect(body.global_hourly_cap).toBe(0);
		});

		it('negative: refuses to save when GET /settings returns no body', async () => {
			vi.spyOn(api, 'GET').mockResolvedValue({ data: undefined } as never);
			const put = vi.spyOn(api, 'PUT');
			await expect(
				saveMergedSettings({ user_agent: 'x' }, (server, d) => ({ ...server, ...d }))
			).rejects.toThrow('GET /settings returned no body');
			expect(put).not.toHaveBeenCalled();
		});

		it('negative: a failing GET /settings propagates and nothing is saved', async () => {
			vi.spyOn(api, 'GET').mockRejectedValue(new Error('503'));
			const put = vi.spyOn(api, 'PUT');
			await expect(
				saveMergedSettings({ user_agent: 'x' }, (server, d) => ({ ...server, ...d }))
			).rejects.toThrow('503');
			expect(put).not.toHaveBeenCalled();
		});
	});

	describe('createSettingsSave', () => {
		afterEach(() => vi.restoreAllMocks());

		it('positive: configures mutation and executes save on mutationFn', async () => {
			const mutationSpy = vi
				.spyOn(sentioQuery, 'createSentioMutation')
				.mockImplementation(((options: unknown) => options) as never);
			const toastSuccess = vi.spyOn(toast, 'success').mockReturnValue('' as never);

			const mutation = createSettingsSave(mergeGeneralSettings) as unknown as {
				mutationFn: (d: {
					history_retention_days: number;
					user_agent: string;
					llm_enabled: boolean;
				}) => Promise<unknown>;
				invalidates: unknown[];
				onSuccess: () => void;
				onError: (err: unknown) => void;
			};

			expect(mutationSpy).toHaveBeenCalledOnce();
			expect(mutation.invalidates).toEqual([keys.settings, keys.caps, keys.instances]);

			const fresh = { ...sampleServerSettings, user_agent: 'Fresh/2.0' };
			const put = stubSettingsApi(fresh);

			await mutation.mutationFn({
				history_retention_days: 45,
				user_agent: 'Agent/3.0',
				llm_enabled: true
			});

			const body = (put.mock.calls[0]?.[1] as { body: Settings }).body;
			expect(body.history_retention_days).toBe(45);
			expect(body.user_agent).toBe('Agent/3.0');

			mutation.onSuccess();
			expect(toastSuccess).toHaveBeenCalledWith(m.settings_saved());
		});

		it('negative: triggers toast error on onError and rejects on save failure', async () => {
			vi.spyOn(sentioQuery, 'createSentioMutation').mockImplementation(
				((options: unknown) => options) as never
			);
			const toastError = vi.spyOn(toast, 'error').mockReturnValue('' as never);

			const mutation = createSettingsSave(mergeGeneralSettings) as unknown as {
				mutationFn: (d: unknown) => Promise<unknown>;
				onError: (err: unknown) => void;
			};

			mutation.onError(new Error('save failed'));
			expect(toastError).toHaveBeenCalledWith('save failed');

			vi.spyOn(api, 'GET').mockRejectedValue(new Error('network error'));
			await expect(mutation.mutationFn({ history_retention_days: 10 })).rejects.toThrow(
				'network error'
			);
		});

		it('boundary: preserves unchanged fields across boundary values and invalidates required keys', async () => {
			vi.spyOn(sentioQuery, 'createSentioMutation').mockImplementation(
				((options: unknown) => options) as never
			);

			const mutation = createSettingsSave(mergeCapsSettings) as unknown as {
				mutationFn: (d: unknown) => Promise<unknown>;
				invalidates: unknown[];
			};

			expect(mutation.invalidates).toHaveLength(3);
			expect(mutation.invalidates).toContain(keys.settings);
			expect(mutation.invalidates).toContain(keys.caps);
			expect(mutation.invalidates).toContain(keys.instances);

			const put = stubSettingsApi({ ...sampleServerSettings });

			await mutation.mutationFn({
				global_hourly_cap: 0,
				search_priority_preset: samplePresetMap,
				download_priority_preset: samplePresetMap
			});

			const body = (put.mock.calls[0]?.[1] as { body: Settings }).body;
			expect(body.global_hourly_cap).toBe(0);
		});
	});
});
