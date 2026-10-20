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

	it('draws every part at full size: labia, hood, glans, urethra, shaded opening', () => {
		const { container } = render(BrandMark, { props: { size: 40 } });
		const svg = container.querySelector('svg');
		expect(svg?.hasAttribute('data-cut')).toBe(false);
		for (const part of ['ol', 'ln', 'gl', 'ur', 'op']) {
			expect(container.querySelector(`.${part}`), part).toBeTruthy();
		}
	});

	it('switches to the simplified cut below 40 px, without the urethra or the hood folds', () => {
		const { container } = render(BrandMark, { props: { size: 39 } });
		expect(container.querySelector('svg')?.hasAttribute('data-cut')).toBe(true);
		expect(container.querySelector('.ur')).toBeNull();
		expect(container.querySelectorAll('.ln')).toHaveLength(1);
		expect(container.querySelector('.gl')).toBeTruthy();
		expect(container.querySelector('.op')).toBeTruthy();
	});

	it('gives each instance its own gradient id, so two marks never share one fill', () => {
		const { container } = render(BrandMark, { props: { size: 56 } });
		const { container: other } = render(BrandMark, { props: { size: 24 } });
		const ids = [container, other].map((c) => c.querySelector('radialGradient')?.id);
		expect(ids[0]).toBeTruthy();
		expect(ids[0]).not.toBe(ids[1]);
		for (const [i, c] of [container, other].entries()) {
			expect(c.querySelector('.op')?.getAttribute('fill')).toBe(`url(#${ids[i]})`);
		}
	});
});
