// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { describe, expect, expectTypeOf, it } from 'vitest';
import { samplePolicy } from '#lib/testing/harness.js';
import {
	isAppSpecificKind,
	type AppKind,
	type PolicySectionProps,
	type SnatchPolicy
} from './section.js';

describe('section policy types', () => {
	it('conforms to PolicySectionProps interface', () => {
		const propsWithKind: PolicySectionProps = {
			policy: samplePolicy,
			kind: 'radarr'
		};
		expectTypeOf(propsWithKind.policy).toEqualTypeOf<SnatchPolicy>();
		expectTypeOf(propsWithKind.kind).toEqualTypeOf<AppKind | undefined>();
		expect(propsWithKind.policy).toBe(samplePolicy);
		expect(propsWithKind.kind).toBe('radarr');

		const propsWithoutKind: PolicySectionProps = {
			policy: samplePolicy
		};
		expect(propsWithoutKind.kind).toBeUndefined();
	});
});

describe('isAppSpecificKind', () => {
	describe('positive tests', () => {
		it('returns true for radarr', () => {
			expect(isAppSpecificKind('radarr')).toBe(true);
		});

		it('returns true for sonarr', () => {
			expect(isAppSpecificKind('sonarr')).toBe(true);
		});

		it('returns true for lidarr', () => {
			expect(isAppSpecificKind('lidarr')).toBe(true);
		});
	});

	describe('negative tests', () => {
		it('returns false for readarr', () => {
			expect(isAppSpecificKind('readarr')).toBe(false);
		});

		it('returns false for whisparr_v2', () => {
			expect(isAppSpecificKind('whisparr_v2')).toBe(false);
		});
	});

	describe('boundary tests', () => {
		it('returns false for undefined', () => {
			expect(isAppSpecificKind(undefined)).toBe(false);
		});

		it('returns false for empty string as AppKind', () => {
			expect(isAppSpecificKind('' as AppKind)).toBe(false);
		});
	});
});
