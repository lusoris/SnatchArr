// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { formatWhen, staminaLevel, staminaPercent } from './stamina.js';

describe('staminaLevel', () => {
	it('reads fresh, tired and spent', () => {
		expect(staminaLevel(0, 20)).toBe('fresh');
		expect(staminaLevel(14, 20)).toBe('fresh');
		expect(staminaLevel(15, 20)).toBe('tired');
		expect(staminaLevel(20, 20)).toBe('spent');
		expect(staminaLevel(25, 20)).toBe('spent');
		expect(staminaLevel(0, 0)).toBe('spent');
	});
});

describe('staminaPercent', () => {
	it('clamps to the gauge range', () => {
		expect(staminaPercent(5, 20)).toBe(25);
		expect(staminaPercent(40, 20)).toBe(100);
		expect(staminaPercent(-1, 20)).toBe(0);
		expect(staminaPercent(1, 0)).toBe(100);
	});
});

describe('formatWhen', () => {
	it('formats timestamps and tolerates junk', () => {
		expect(formatWhen(undefined, 'en')).toBe('');
		expect(formatWhen('not a date', 'en')).toBe('not a date');
		expect(formatWhen('2026-09-21T14:00:00Z', 'en')).toMatch(/2026/);
	});
});
