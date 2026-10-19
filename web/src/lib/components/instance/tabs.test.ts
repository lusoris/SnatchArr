// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, it } from 'vitest';
import { getInstanceTabs, isTabActive } from './tabs.js';

describe('getInstanceTabs', () => {
	it('returns the 5 declared subtabs with correct URLs', () => {
		const tabs = getInstanceTabs('inst-42');
		expect(tabs).toHaveLength(5);
		expect(tabs.map((t) => t.key)).toEqual(['overview', 'policy', 'queue', 'activity', 'memory']);
		expect(tabs[0]?.href).toBe('/instances/inst-42');
		expect(tabs[1]?.href).toBe('/instances/inst-42/policy');
		expect(tabs[2]?.href).toBe('/instances/inst-42/queue');
		expect(tabs[3]?.href).toBe('/instances/inst-42/activity');
		expect(tabs[4]?.href).toBe('/instances/inst-42/memory');
	});
});

describe('isTabActive', () => {
	describe('positive tests', () => {
		it('matches overview exactly', () => {
			expect(isTabActive('/instances/inst-1', '/instances/inst-1', true)).toBe(true);
		});

		it('matches overview with trailing slash', () => {
			expect(isTabActive('/instances/inst-1/', '/instances/inst-1', true)).toBe(true);
			expect(isTabActive('/instances/inst-1', '/instances/inst-1/', true)).toBe(true);
		});

		it('matches child tab exactly', () => {
			expect(isTabActive('/instances/inst-1/policy', '/instances/inst-1/policy', false)).toBe(true);
		});

		it('matches child tab subpath prefix', () => {
			expect(isTabActive('/instances/inst-1/policy/edit', '/instances/inst-1/policy', false)).toBe(
				true
			);
		});
	});

	describe('negative tests', () => {
		it('does not match overview when on child route', () => {
			expect(isTabActive('/instances/inst-1/policy', '/instances/inst-1', true)).toBe(false);
			expect(isTabActive('/instances/inst-1/queue', '/instances/inst-1', true)).toBe(false);
		});

		it('does not match different tab', () => {
			expect(isTabActive('/instances/inst-1/queue', '/instances/inst-1/policy', false)).toBe(false);
		});

		it('does not match different instance id', () => {
			expect(isTabActive('/instances/inst-2', '/instances/inst-1', true)).toBe(false);
			expect(isTabActive('/instances/inst-2/policy', '/instances/inst-1/policy', false)).toBe(
				false
			);
		});

		it('does not match partial non-boundary segment', () => {
			expect(isTabActive('/instances/inst-1/policy-extra', '/instances/inst-1/policy', false)).toBe(
				false
			);
		});
	});

	describe('boundary tests', () => {
		it('returns false when currentPath is empty', () => {
			expect(isTabActive('', '/instances/inst-1', true)).toBe(false);
		});

		it('returns false when tabHref is empty', () => {
			expect(isTabActive('/instances/inst-1', '', true)).toBe(false);
		});

		it('returns false when both are empty', () => {
			expect(isTabActive('', '', true)).toBe(false);
		});

		it('handles root path gracefully', () => {
			expect(isTabActive('/', '/', true)).toBe(true);
			expect(isTabActive('/', '/instances/inst-1', true)).toBe(false);
		});
	});
});
