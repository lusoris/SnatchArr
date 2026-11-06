// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';

export type SnatchPolicy = components['schemas']['SnatchPolicy'];
export type SnatchPolicyPreset = components['schemas']['SnatchPolicyPreset'];

export type PresetKey = 'gentle' | 'balanced' | 'catch_up' | 'upgrades';

export interface Preset {
	key: PresetKey;
	label: () => string;
	description: () => string;
	values: Partial<SnatchPolicy>;
}

const LABELS: Record<PresetKey, { label: () => string; description: () => string }> = {
	gentle: { label: m.preset_gentle_label, description: m.preset_gentle_desc },
	balanced: { label: m.preset_balanced_label, description: m.preset_balanced_desc },
	catch_up: { label: m.preset_catch_up_label, description: m.preset_catch_up_desc },
	upgrades: { label: m.preset_upgrades_label, description: m.preset_upgrades_desc }
};

/** Build display presets from the API response, enriched with i18n labels. */
export function toPresets(apiPresets: SnatchPolicyPreset[]): Preset[] {
	return apiPresets
		.filter((p): p is SnatchPolicyPreset & { key: PresetKey } => p.key in LABELS)
		.map((p) => {
			const values: Partial<SnatchPolicy> = {
				missing_per_cycle: p.missing_per_cycle,
				upgrade_per_cycle: p.upgrade_per_cycle,
				cycle_interval_s: p.cycle_interval_s,
				hourly_cap: p.hourly_cap
			};
			if (p.selection) {
				values.selection = p.selection;
			}
			return { key: p.key, ...LABELS[p.key], values };
		});
}

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
