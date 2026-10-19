// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import BrandMark from './BrandMark.svelte';

describe('BrandMark', () => {
	it('renders unlabelled with aria-hidden="true" and no role', () => {
		const { container } = render(BrandMark, { props: { size: 32 } });
		const svg = container.querySelector('svg');
		expect(svg).toBeTruthy();
		expect(svg?.getAttribute('aria-hidden')).toBe('true');
		expect(svg?.getAttribute('role')).toBeNull();
		expect(svg?.getAttribute('aria-label')).toBeNull();
		expect(svg?.getAttribute('width')).toBe('32');
		expect(svg?.getAttribute('height')).toBe('32');
	});

	it('renders labelled with role="img" and aria-label', () => {
		const { container } = render(BrandMark, { props: { size: 24, label: 'SnatchArr Logo' } });
		const svg = container.querySelector('svg');
		expect(svg).toBeTruthy();
		expect(svg?.getAttribute('role')).toBe('img');
		expect(svg?.getAttribute('aria-label')).toBe('SnatchArr Logo');
		expect(svg?.getAttribute('aria-hidden')).toBeNull();
		expect(svg?.getAttribute('width')).toBe('24');
		expect(svg?.getAttribute('height')).toBe('24');
	});
});
