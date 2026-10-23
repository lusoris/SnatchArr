// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { m } from '#lib/paraglide/messages.js';
import * as runtime from '#lib/paraglide/runtime.js';
import LocaleSelect from './LocaleSelect.svelte';

vi.mock('#lib/paraglide/runtime.js', async (importOriginal) => {
	const actual = await importOriginal<typeof import('#lib/paraglide/runtime.js')>();
	return {
		...actual,
		setLocale: vi.fn(),
		getLocale: vi.fn(() => 'en'),
		locales: ['en', 'de']
	};
});

describe('LocaleSelect', () => {
	afterEach(() => {
		vi.clearAllMocks();
	});

	it('positive: renders label and select matching the passed id with current locale selected', async () => {
		const { container } = render(LocaleSelect, { props: { id: 'desktop-lang' } });

		const label = screen.getByText(m.language());
		expect(label.getAttribute('for')).toBe('desktop-lang');

		const select = screen.getByRole('combobox') as HTMLSelectElement;
		expect(select.id).toBe('desktop-lang');
		expect(select.value).toBe('en');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('positive: changing select value triggers setLocale', async () => {
		render(LocaleSelect, { props: { id: 'mobile-lang' } });

		const select = screen.getByRole('combobox');
		await fireEvent.change(select, { target: { value: 'de' } });

		expect(runtime.setLocale).toHaveBeenCalledWith('de');
	});

	it('negative: does not call setLocale without change event', () => {
		render(LocaleSelect, { props: { id: 'mobile-lang' } });
		expect(runtime.setLocale).not.toHaveBeenCalled();
	});

	it('boundary: renders all available locales as options', () => {
		render(LocaleSelect, { props: { id: 'desktop-lang' } });

		const options = screen.getAllByRole('option');
		expect(options.map((o) => (o as HTMLOptionElement).value)).toEqual(['en', 'de']);
	});
});
