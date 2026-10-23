// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { formatVersionLabel } from './system.js';

describe('formatVersionLabel', () => {
	it('positive: formats version and first 7 characters of commit', () => {
		expect(formatVersionLabel('0.1.0', 'abcdef123456')).toBe('v0.1.0 (abcdef1)');
		expect(formatVersionLabel('v1.2.3', 'fedcba654321')).toBe('v1.2.3 (fedcba6)');
	});

	it('negative: missing version renders nothing', () => {
		expect(formatVersionLabel(undefined, undefined)).toBe('');
		expect(formatVersionLabel(null, null)).toBe('');
		expect(formatVersionLabel('', 'abcdef123456')).toBe('');
	});

	it('boundary: empty or missing commit renders without commit part and dev build as-is', () => {
		expect(formatVersionLabel('dev', '')).toBe('dev');
		expect(formatVersionLabel('dev', undefined)).toBe('dev');
		expect(formatVersionLabel('0.1.0', '')).toBe('v0.1.0');
		expect(formatVersionLabel('0.1.0', undefined)).toBe('v0.1.0');
	});
});
