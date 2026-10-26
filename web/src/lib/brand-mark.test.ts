// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import {
	DEFAULT_BRAND_MARK,
	DEFAULT_SFW_MODE,
	resolveBrandMark,
	type BrandMarkKind,
	type SfwMode
} from './brand-mark.js';

interface TestCase {
	brandMark: BrandMarkKind;
	sfwMode: SfwMode;
	adultModuleEnabled: boolean;
	userAdultAllowed: boolean;
	expected: BrandMarkKind;
}

const ALL_36_COMBINATIONS: readonly TestCase[] = [
	// brandMark: 'vulva' (12 combinations)
	{
		brandMark: 'vulva',
		sfwMode: 'auto',
		adultModuleEnabled: true,
		userAdultAllowed: true,
		expected: 'vulva'
	},
	{
		brandMark: 'vulva',
		sfwMode: 'auto',
		adultModuleEnabled: true,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'vulva',
		sfwMode: 'auto',
		adultModuleEnabled: false,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'vulva',
		sfwMode: 'auto',
		adultModuleEnabled: false,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'vulva',
		sfwMode: 'on',
		adultModuleEnabled: true,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'vulva',
		sfwMode: 'on',
		adultModuleEnabled: true,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'vulva',
		sfwMode: 'on',
		adultModuleEnabled: false,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'vulva',
		sfwMode: 'on',
		adultModuleEnabled: false,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'vulva',
		sfwMode: 'off',
		adultModuleEnabled: true,
		userAdultAllowed: true,
		expected: 'vulva'
	},
	{
		brandMark: 'vulva',
		sfwMode: 'off',
		adultModuleEnabled: true,
		userAdultAllowed: false,
		expected: 'vulva'
	},
	{
		brandMark: 'vulva',
		sfwMode: 'off',
		adultModuleEnabled: false,
		userAdultAllowed: true,
		expected: 'vulva'
	},
	{
		brandMark: 'vulva',
		sfwMode: 'off',
		adultModuleEnabled: false,
		userAdultAllowed: false,
		expected: 'vulva'
	},

	// brandMark: 'penis' (12 combinations)
	{
		brandMark: 'penis',
		sfwMode: 'auto',
		adultModuleEnabled: true,
		userAdultAllowed: true,
		expected: 'penis'
	},
	{
		brandMark: 'penis',
		sfwMode: 'auto',
		adultModuleEnabled: true,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'penis',
		sfwMode: 'auto',
		adultModuleEnabled: false,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'penis',
		sfwMode: 'auto',
		adultModuleEnabled: false,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'penis',
		sfwMode: 'on',
		adultModuleEnabled: true,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'penis',
		sfwMode: 'on',
		adultModuleEnabled: true,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'penis',
		sfwMode: 'on',
		adultModuleEnabled: false,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'penis',
		sfwMode: 'on',
		adultModuleEnabled: false,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'penis',
		sfwMode: 'off',
		adultModuleEnabled: true,
		userAdultAllowed: true,
		expected: 'penis'
	},
	{
		brandMark: 'penis',
		sfwMode: 'off',
		adultModuleEnabled: true,
		userAdultAllowed: false,
		expected: 'penis'
	},
	{
		brandMark: 'penis',
		sfwMode: 'off',
		adultModuleEnabled: false,
		userAdultAllowed: true,
		expected: 'penis'
	},
	{
		brandMark: 'penis',
		sfwMode: 'off',
		adultModuleEnabled: false,
		userAdultAllowed: false,
		expected: 'penis'
	},

	// brandMark: 'hand' (12 combinations)
	{
		brandMark: 'hand',
		sfwMode: 'auto',
		adultModuleEnabled: true,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'hand',
		sfwMode: 'auto',
		adultModuleEnabled: true,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'hand',
		sfwMode: 'auto',
		adultModuleEnabled: false,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'hand',
		sfwMode: 'auto',
		adultModuleEnabled: false,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'hand',
		sfwMode: 'on',
		adultModuleEnabled: true,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'hand',
		sfwMode: 'on',
		adultModuleEnabled: true,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'hand',
		sfwMode: 'on',
		adultModuleEnabled: false,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'hand',
		sfwMode: 'on',
		adultModuleEnabled: false,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'hand',
		sfwMode: 'off',
		adultModuleEnabled: true,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'hand',
		sfwMode: 'off',
		adultModuleEnabled: true,
		userAdultAllowed: false,
		expected: 'hand'
	},
	{
		brandMark: 'hand',
		sfwMode: 'off',
		adultModuleEnabled: false,
		userAdultAllowed: true,
		expected: 'hand'
	},
	{
		brandMark: 'hand',
		sfwMode: 'off',
		adultModuleEnabled: false,
		userAdultAllowed: false,
		expected: 'hand'
	}
];

describe('resolveBrandMark', () => {
	it('exports expected defaults', () => {
		expect(DEFAULT_BRAND_MARK).toBe('vulva');
		expect(DEFAULT_SFW_MODE).toBe('auto');
	});

	it('covers all 36 combinations', () => {
		expect(ALL_36_COMBINATIONS).toHaveLength(36);
	});

	it.each(ALL_36_COMBINATIONS)(
		'resolves ($brandMark, sfw=$sfwMode, adult=$adultModuleEnabled, user=$userAdultAllowed) -> $expected',
		({ brandMark, sfwMode, adultModuleEnabled, userAdultAllowed, expected }) => {
			const actual = resolveBrandMark(brandMark, sfwMode, adultModuleEnabled, userAdultAllowed);
			expect(actual).toBe(expected);
		}
	);

	it('negative: auto with adult on but user not allowed -> hand', () => {
		expect(resolveBrandMark('vulva', 'auto', true, false)).toBe('hand');
		expect(resolveBrandMark('penis', 'auto', true, false)).toBe('hand');
	});

	it('boundary: off with hand -> hand', () => {
		expect(resolveBrandMark('hand', 'off', false, false)).toBe('hand');
		expect(resolveBrandMark('hand', 'off', true, true)).toBe('hand');
	});
});
