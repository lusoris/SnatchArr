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
		expect(svg?.getAttribute('data-mark')).toBe('vulva');
		expect(svg?.getAttribute('viewBox')).toBe('50.25 37.1 56 56');
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

	describe('penis mark', () => {
		it('renders data-mark="penis" and 0 0 100 100 viewBox', () => {
			const { container } = render(BrandMark, { props: { mark: 'penis', size: 32 } });
			const svg = container.querySelector('svg');
			expect(svg).toBeTruthy();
			expect(svg?.getAttribute('data-mark')).toBe('penis');
			expect(svg?.getAttribute('viewBox')).toBe('0 0 100 100');
		});

		it('full at 40 and cut at 39 (data-cut)', () => {
			const { container: full } = render(BrandMark, { props: { mark: 'penis', size: 40 } });
			const fullSvg = full.querySelector('svg');
			expect(fullSvg?.hasAttribute('data-cut')).toBe(false);
			expect(full.querySelector('.pn')).toBeTruthy();
			expect(full.querySelector('.c')).toBeNull();
			expect(full.querySelector('.pn-p')).toBeTruthy();
			expect(full.querySelector('.pn-s')).toBeTruthy();

			const { container: cut } = render(BrandMark, { props: { mark: 'penis', size: 39 } });
			const cutSvg = cut.querySelector('svg');
			expect(cutSvg?.hasAttribute('data-cut')).toBe(true);
			expect(cut.querySelector('.pn-o.c')).toBeTruthy();
			expect(cut.querySelector('.pn-s.c')).toBeTruthy();
		});

		it('gives each instance its own unique gradient id', () => {
			const { container: c1 } = render(BrandMark, { props: { mark: 'penis', size: 56 } });
			const { container: c2 } = render(BrandMark, { props: { mark: 'penis', size: 24 } });
			const id1 = c1.querySelector('radialGradient')?.id;
			const id2 = c2.querySelector('radialGradient')?.id;
			expect(id1).toBeTruthy();
			expect(id2).toBeTruthy();
			expect(id1).not.toBe(id2);
			expect(c1.querySelector('.pn-s')?.getAttribute('fill')).toBe(`url(#${id1})`);
			expect(c2.querySelector('.pn-s')?.getAttribute('fill')).toBe(`url(#${id2})`);
		});
	});

	describe('hand mark', () => {
		it('renders data-mark="hand" and 0 0 100 100 viewBox', () => {
			const { container } = render(BrandMark, { props: { mark: 'hand', size: 32 } });
			const svg = container.querySelector('svg');
			expect(svg).toBeTruthy();
			expect(svg?.getAttribute('data-mark')).toBe('hand');
			expect(svg?.getAttribute('viewBox')).toBe('0 0 100 100');
		});

		it('full at 40 and cut at 39 (data-cut)', () => {
			const { container: full } = render(BrandMark, { props: { mark: 'hand', size: 40 } });
			const fullSvg = full.querySelector('svg');
			expect(fullSvg?.hasAttribute('data-cut')).toBe(false);
			expect(full.querySelector('.hd')).toBeTruthy();
			expect(full.querySelector('.c')).toBeNull();
			expect(full.querySelector('.hd-p')).toBeTruthy();
			expect(full.querySelector('.hd-s')).toBeTruthy();

			const { container: cut } = render(BrandMark, { props: { mark: 'hand', size: 39 } });
			const cutSvg = cut.querySelector('svg');
			expect(cutSvg?.hasAttribute('data-cut')).toBe(true);
			expect(cut.querySelector('.hd-o.c')).toBeTruthy();
			expect(cut.querySelector('.hd-s.c')).toBeTruthy();
		});

		it('gives each instance its own unique gradient id', () => {
			const { container: c1 } = render(BrandMark, { props: { mark: 'hand', size: 56 } });
			const { container: c2 } = render(BrandMark, { props: { mark: 'hand', size: 24 } });
			const id1 = c1.querySelector('radialGradient')?.id;
			const id2 = c2.querySelector('radialGradient')?.id;
			expect(id1).toBeTruthy();
			expect(id2).toBeTruthy();
			expect(id1).not.toBe(id2);
			expect(c1.querySelector('.hd-s')?.getAttribute('fill')).toBe(`url(#${id1})`);
			expect(c2.querySelector('.hd-s')?.getAttribute('fill')).toBe(`url(#${id2})`);
		});
	});
});
