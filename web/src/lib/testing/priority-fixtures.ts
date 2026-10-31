// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from '#lib/api/schema.js';

type PriorityPreset = components['schemas']['PriorityPreset'];
type Settings = components['schemas']['Settings'];

/** Ranks of the shipped media_first preset, shared by the settings test fixtures. */
export const mediaFirstRanks = {
	sonarr: 10,
	radarr: 10,
	lidarr: 20,
	readarr: 30,
	whisparr_v2: 40,
	whisparr_v3: 40
};

/** The shipped preset list as GET /priority-presets returns it; fresh copies per call. */
export function shippedPresets(): PriorityPreset[] {
	return [
		{
			name: 'media_first',
			builtin: true,
			search: { ...mediaFirstRanks },
			download: { ...mediaFirstRanks }
		}
	];
}

/** Settings on the shipped preset; each test file adds the fields its form reads. */
export function settingsOnShippedPreset(extra: Partial<Settings>): Settings {
	return {
		history_retention_days: 30,
		user_agent: 'SnatchArr/1.0',
		global_hourly_cap: 100,
		search_priority_preset: { ...mediaFirstRanks },
		download_priority_preset: { ...mediaFirstRanks },
		llm_enabled: false,
		llm_write_tools: [],
		...extra
	};
}

/** GET stubs for the settings form: the given settings plus the shipped preset list. */
export function settingsStubs(settings: Settings): Record<string, unknown> {
	return { '/settings': settings, '/priority-presets': shippedPresets() };
}
