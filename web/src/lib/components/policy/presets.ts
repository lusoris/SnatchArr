// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';

export type SnatchPolicy = components['schemas']['SnatchPolicy'];

export interface Preset {
	key: 'gentle' | 'balanced' | 'catch_up' | 'upgrades';
	label: () => string;
	description: () => string;
	values: Partial<SnatchPolicy>;
}

// Values and reasons: docs/adr/0020-snatch-policy-defaults-and-presets.md.
// balanced equals the server defaults (domain.DefaultPolicy).
export const PRESETS: Preset[] = [
	{
		key: 'gentle',
		label: m.preset_gentle_label,
		description: m.preset_gentle_desc,
		values: { missing_per_cycle: 1, upgrade_per_cycle: 0, cycle_interval_s: 900, hourly_cap: 4 }
	},
	{
		key: 'balanced',
		label: m.preset_balanced_label,
		description: m.preset_balanced_desc,
		values: { missing_per_cycle: 2, upgrade_per_cycle: 1, cycle_interval_s: 900, hourly_cap: 15 }
	},
	{
		key: 'catch_up',
		label: m.preset_catch_up_label,
		description: m.preset_catch_up_desc,
		values: {
			missing_per_cycle: 10,
			upgrade_per_cycle: 2,
			cycle_interval_s: 300,
			hourly_cap: 60,
			selection: 'sequential'
		}
	},
	{
		key: 'upgrades',
		label: m.preset_upgrades_label,
		description: m.preset_upgrades_desc,
		values: { missing_per_cycle: 1, upgrade_per_cycle: 5, cycle_interval_s: 600, hourly_cap: 25 }
	}
];

/**
 * Resulting searches/hour estimate: min(hourly_cap, (missing + upgrade) * 3600 / cycle_interval_s).
 */
export function estimateSearchesPerHour(p: {
	hourly_cap?: number;
	missing_per_cycle?: number;
	upgrade_per_cycle?: number;
	cycle_interval_s?: number;
}): number {
	const cap = p.hourly_cap ?? 0;
	const interval = p.cycle_interval_s ?? 0;
	if (interval <= 0 || cap <= 0) return 0;
	const batch = (p.missing_per_cycle ?? 0) + (p.upgrade_per_cycle ?? 0);
	if (batch <= 0) return 0;
	const rate = Math.round((batch * 3600) / interval);
	return Math.min(cap, rate);
}
