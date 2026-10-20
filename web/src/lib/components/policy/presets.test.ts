// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { PRESETS, estimateSearchesPerHour } from './presets.js';

describe('presets', () => {
	it('exports all 4 declared presets with valid values and labels', () => {
		expect(PRESETS).toHaveLength(4);
		const keys = PRESETS.map((p) => p.key);
		expect(keys).toEqual(['gentle', 'balanced', 'catch_up', 'upgrades']);

		for (const preset of PRESETS) {
			expect(preset.label()).toBeTruthy();
			expect(preset.description()).toBeTruthy();
			expect(preset.values.cycle_interval_s).toBeGreaterThanOrEqual(60);
			expect(preset.values.hourly_cap).toBeGreaterThanOrEqual(1);
			expect(preset.values.missing_per_cycle).toBeGreaterThanOrEqual(0);
			expect(preset.values.upgrade_per_cycle).toBeGreaterThanOrEqual(0);
		}
	});

	it('computes expected searches per hour for each preset', () => {
		// gentle: (1 + 0) * 3600 / 900 = 4; min(4, 4) = 4
		const gentle = PRESETS.find((p) => p.key === 'gentle')!;
		expect(estimateSearchesPerHour(gentle.values)).toBe(4);

		// balanced: (2 + 1) * 3600 / 900 = 12; min(15, 12) = 12
		const balanced = PRESETS.find((p) => p.key === 'balanced')!;
		expect(estimateSearchesPerHour(balanced.values)).toBe(12);

		// catch_up: (10 + 2) * 3600 / 300 = 144; min(60, 144) = 60
		const catchUp = PRESETS.find((p) => p.key === 'catch_up')!;
		expect(estimateSearchesPerHour(catchUp.values)).toBe(60);

		// upgrades: (1 + 5) * 3600 / 600 = 36; min(25, 36) = 25
		const upgrades = PRESETS.find((p) => p.key === 'upgrades')!;
		expect(estimateSearchesPerHour(upgrades.values)).toBe(25);
	});

	it('handles boundary and negative cases safely', () => {
		// Zero or negative interval returns 0
		expect(
			estimateSearchesPerHour({ hourly_cap: 30, missing_per_cycle: 5, cycle_interval_s: 0 })
		).toBe(0);
		expect(
			estimateSearchesPerHour({ hourly_cap: 30, missing_per_cycle: 5, cycle_interval_s: -100 })
		).toBe(0);

		// Zero or negative cap returns 0
		expect(
			estimateSearchesPerHour({ hourly_cap: 0, missing_per_cycle: 5, cycle_interval_s: 900 })
		).toBe(0);
		expect(
			estimateSearchesPerHour({ hourly_cap: -5, missing_per_cycle: 5, cycle_interval_s: 900 })
		).toBe(0);

		// Zero missing and upgrades returns 0
		expect(
			estimateSearchesPerHour({
				hourly_cap: 50,
				missing_per_cycle: 0,
				upgrade_per_cycle: 0,
				cycle_interval_s: 900
			})
		).toBe(0);

		// Cap constrains when calculated rate exceeds cap
		expect(
			estimateSearchesPerHour({
				hourly_cap: 10,
				missing_per_cycle: 50,
				upgrade_per_cycle: 50,
				cycle_interval_s: 60
			})
		).toBe(10);

		// Missing undefined defaults to 0
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
