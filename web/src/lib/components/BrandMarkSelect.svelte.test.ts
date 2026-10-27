// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import BrandMarkSelect from './BrandMarkSelect.svelte';
import SfwModeSelect from './SfwModeSelect.svelte';

describe('BrandMarkSelect', () => {
	it('positive: offers every brand mark option and passes WCAG', async () => {
		const { container } = render(BrandMarkSelect, { value: 'vulva' });
		const options = screen.getAllByRole<HTMLOptionElement>('option');
		expect(options.map((o) => o.value)).toEqual(['vulva', 'penis', 'hand']);
		expect(options.map((o) => o.textContent)).toEqual([
			m.brand_mark_vulva(),
			m.brand_mark_penis(),
			m.brand_mark_hand()
		]);

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('positive: offers follow default option when allowDefault is true', () => {
		render(BrandMarkSelect, { value: '', allowDefault: true });
		const options = screen.getAllByRole<HTMLOptionElement>('option');
		expect(options.map((o) => o.value)).toEqual(['', 'vulva', 'penis', 'hand']);
		expect(options[0].textContent).toBe(m.preferences_follow_default());
	});
});

describe('SfwModeSelect', () => {
	it('positive: offers every SFW mode option and passes WCAG', async () => {
		const { container } = render(SfwModeSelect, { value: 'auto' });
		const options = screen.getAllByRole<HTMLOptionElement>('option');
		expect(options.map((o) => o.value)).toEqual(['auto', 'on', 'off']);
		expect(options.map((o) => o.textContent)).toEqual([
			m.sfw_mode_auto(),
			m.sfw_mode_on(),
			m.sfw_mode_off()
		]);

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('positive: offers follow default option when allowDefault is true', () => {
		render(SfwModeSelect, { value: '', allowDefault: true });
		const options = screen.getAllByRole<HTMLOptionElement>('option');
		expect(options.map((o) => o.value)).toEqual(['', 'auto', 'on', 'off']);
		expect(options[0].textContent).toBe(m.preferences_follow_default());
	});
});
