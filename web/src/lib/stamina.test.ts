// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { formatRelativeTime, formatWhen, staminaLevel, staminaPercent } from './stamina.js';

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

describe('formatRelativeTime', () => {
	const fixedNow = new Date('2026-10-25T12:00:00Z');

	it('positive: formats relative time across units', () => {
		expect(formatRelativeTime('2026-10-25T11:59:58Z', 'en', fixedNow)).toBe('now');
		expect(formatRelativeTime('2026-10-25T11:59:30Z', 'en', fixedNow)).toBe('30 seconds ago');
		expect(formatRelativeTime('2026-10-25T11:50:00Z', 'en', fixedNow)).toBe('10 minutes ago');
		expect(formatRelativeTime('2026-10-25T10:00:00Z', 'en', fixedNow)).toBe('2 hours ago');
		expect(formatRelativeTime('2026-10-22T12:00:00Z', 'en', fixedNow)).toBe('3 days ago');
	});

	it('positive: formats relative time in German locale', () => {
		expect(formatRelativeTime('2026-10-25T11:59:58Z', 'de', fixedNow)).toBe('jetzt');
		expect(formatRelativeTime('2026-10-25T11:59:30Z', 'de', fixedNow)).toBe('vor 30 Sekunden');
		expect(formatRelativeTime('2026-10-25T11:50:00Z', 'de', fixedNow)).toBe('vor 10 Minuten');
		expect(formatRelativeTime('2026-10-25T10:00:00Z', 'de', fixedNow)).toBe('vor 2 Stunden');
	});

	it('negative: handles undefined or invalid input gracefully', () => {
		expect(formatRelativeTime(undefined, 'en', fixedNow)).toBe('');
		expect(formatRelativeTime('invalid-date', 'en', fixedNow)).toBe('invalid-date');
	});

	it('boundary: diff around threshold boundaries', () => {
		expect(formatRelativeTime('2026-10-25T12:00:00Z', 'en', fixedNow)).toBe('now');
		expect(formatRelativeTime('2026-10-25T12:00:04Z', 'en', fixedNow)).toBe('now');
		expect(formatRelativeTime('2026-10-25T11:59:55Z', 'en', fixedNow)).toBe('5 seconds ago');
	});
});
