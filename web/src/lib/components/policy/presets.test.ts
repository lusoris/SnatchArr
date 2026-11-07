// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { toPresets, estimateSearchesPerHour, type SnatchPolicyPreset } from './presets.js';

const API_PRESETS: SnatchPolicyPreset[] = [
	{
		key: 'gentle',
		missing_per_cycle: 1,
		upgrade_per_cycle: 0,
		cycle_interval_s: 900,
		hourly_cap: 4,
		selection: 'random',
		sonarr_missing_mode: 'dynamic',
		sonarr_upgrade_mode: 'episodes'
	},
	{
		key: 'balanced',
		missing_per_cycle: 2,
		upgrade_per_cycle: 1,
		cycle_interval_s: 900,
		hourly_cap: 15,
		selection: 'random',
		sonarr_missing_mode: 'dynamic',
		sonarr_upgrade_mode: 'episodes'
	},
	{
		key: 'catch_up',
		missing_per_cycle: 10,
		upgrade_per_cycle: 2,
		cycle_interval_s: 300,
		hourly_cap: 60,
		selection: 'sequential',
		sonarr_missing_mode: 'dynamic',
		sonarr_upgrade_mode: 'episodes'
	},
	{
		key: 'upgrades',
		missing_per_cycle: 1,
		upgrade_per_cycle: 5,
		cycle_interval_s: 600,
		hourly_cap: 25,
		selection: 'random',
		sonarr_missing_mode: 'dynamic',
		sonarr_upgrade_mode: 'episodes'
	}
];

describe('toPresets', () => {
	it('builds all 4 presets from API data with valid labels', () => {
		const presets = toPresets(API_PRESETS);
		expect(presets).toHaveLength(4);
		const keys = presets.map((p) => p.key);
		expect(keys).toEqual(['gentle', 'balanced', 'catch_up', 'upgrades']);

		for (const preset of presets) {
			expect(preset.label()).toBeTruthy();
			expect(preset.description()).toBeTruthy();
			expect(preset.values.cycle_interval_s).toBeGreaterThanOrEqual(60);
			expect(preset.values.hourly_cap).toBeGreaterThanOrEqual(1);
			expect(preset.values.missing_per_cycle).toBeGreaterThanOrEqual(0);
			expect(preset.values.upgrade_per_cycle).toBeGreaterThanOrEqual(0);
			expect(preset.values.sonarr_missing_mode).toBe('dynamic');
			expect(preset.values.sonarr_upgrade_mode).toBe('episodes');
		}
	});

	it('includes selection for every preset', () => {
		const presets = toPresets(API_PRESETS);
		const catchUp = presets.find((p) => p.key === 'catch_up')!;
		expect(catchUp.values.selection).toBe('sequential');
		const gentle = presets.find((p) => p.key === 'gentle')!;
		expect(gentle.values.selection).toBe('random');
	});

	it('skips unknown keys', () => {
		const extra: SnatchPolicyPreset[] = [
			...API_PRESETS,
			{
				key: 'unknown',
				missing_per_cycle: 1,
				upgrade_per_cycle: 1,
				cycle_interval_s: 60,
				hourly_cap: 1,
				sonarr_missing_mode: 'dynamic',
				sonarr_upgrade_mode: 'episodes'
			}
		];
		expect(toPresets(extra)).toHaveLength(4);
	});
});

describe('estimateSearchesPerHour', () => {
	const presets = toPresets(API_PRESETS);

	it('computes expected searches per hour for each preset', () => {
		// gentle: (1 + 0) * 3600 / 900 = 4; min(4, 4) = 4
		const gentle = presets.find((p) => p.key === 'gentle')!;
		expect(estimateSearchesPerHour(gentle.values)).toBe(4);

		// balanced: (2 + 1) * 3600 / 900 = 12; min(15, 12) = 12
		const balanced = presets.find((p) => p.key === 'balanced')!;
		expect(estimateSearchesPerHour(balanced.values)).toBe(12);

		// catch_up: (10 + 2) * 3600 / 300 = 144; min(60, 144) = 60
		const catchUp = presets.find((p) => p.key === 'catch_up')!;
		expect(estimateSearchesPerHour(catchUp.values)).toBe(60);

		// upgrades: (1 + 5) * 3600 / 600 = 36; min(25, 36) = 25
		const upgrades = presets.find((p) => p.key === 'upgrades')!;
		expect(estimateSearchesPerHour(upgrades.values)).toBe(25);
	});

	it('handles boundary and negative cases safely', () => {
		expect(
			estimateSearchesPerHour({ hourly_cap: 30, missing_per_cycle: 5, cycle_interval_s: 0 })
		).toBe(0);
		expect(
			estimateSearchesPerHour({ hourly_cap: 30, missing_per_cycle: 5, cycle_interval_s: -100 })
		).toBe(0);
		expect(
			estimateSearchesPerHour({ hourly_cap: 0, missing_per_cycle: 5, cycle_interval_s: 900 })
		).toBe(0);
		expect(
			estimateSearchesPerHour({ hourly_cap: -5, missing_per_cycle: 5, cycle_interval_s: 900 })
		).toBe(0);
		expect(
			estimateSearchesPerHour({
				hourly_cap: 50,
				missing_per_cycle: 0,
				upgrade_per_cycle: 0,
				cycle_interval_s: 900
			})
		).toBe(0);
		expect(
			estimateSearchesPerHour({
				hourly_cap: 10,
				missing_per_cycle: 50,
				upgrade_per_cycle: 50,
				cycle_interval_s: 60
			})
		).toBe(10);
		expect(
			estimateSearchesPerHour({
				hourly_cap: 50,
				missing_per_cycle: undefined,
				upgrade_per_cycle: 2,
				cycle_interval_s: 3600
			})
		).toBe(2);
	});
});
