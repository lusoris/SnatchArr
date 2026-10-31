// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import Preferences from './Preferences.svelte';

type UserPreferences = components['schemas']['UserPreferences'];

const samplePrefs: UserPreferences = {
	brand_mark: 'hand',
	sfw_mode: 'on'
};

const baseStubs = {
	'/me/preferences': samplePrefs
};

interface PreferencesControls {
	container: HTMLElement;
	brandMarkSelect: HTMLSelectElement;
	sfwModeSelect: HTMLSelectElement;
}

async function renderPreferences(): Promise<PreferencesControls> {
	stubGet(baseStubs);
	const { container } = renderWithQuery(Preferences);
	const brandMarkSelect = (await screen.findByLabelText(
		m.preferences_brand_mark()
	)) as HTMLSelectElement;
	const sfwModeSelect = screen.getByLabelText(m.preferences_sfw_mode()) as HTMLSelectElement;
	return { container, brandMarkSelect, sfwModeSelect };
}

describe('Preferences component', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('positive: renders selects with loaded values and passes WCAG', async () => {
		const { container, brandMarkSelect, sfwModeSelect } = await renderPreferences();

		expect(brandMarkSelect.value).toBe('hand');
		expect(sfwModeSelect.value).toBe('on');

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});

	it('positive: updates preferences to new values on submit', async () => {
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: { brand_mark: 'penis', sfw_mode: 'off' }
		} as never);

		const { brandMarkSelect, sfwModeSelect } = await renderPreferences();

		await fireEvent.change(brandMarkSelect, { target: { value: 'penis' } });
		await fireEvent.change(sfwModeSelect, { target: { value: 'off' } });

		const submitBtn = screen.getByRole('button', { name: m.preferences_save() });
		await fireEvent.click(submitBtn);

		expect(putSpy).toHaveBeenCalledWith('/me/preferences', {
			body: { brand_mark: 'penis', sfw_mode: 'off' }
		});
	});

	it('boundary: follow default option sends null', async () => {
		const putSpy = vi.spyOn(api, 'PUT').mockResolvedValue({
			data: { brand_mark: null, sfw_mode: null }
		} as never);

		const { brandMarkSelect, sfwModeSelect } = await renderPreferences();

		await fireEvent.change(brandMarkSelect, { target: { value: '' } });
		await fireEvent.change(sfwModeSelect, { target: { value: '' } });

		const submitBtn = screen.getByRole('button', { name: m.preferences_save() });
		await fireEvent.click(submitBtn);

		expect(putSpy).toHaveBeenCalledWith('/me/preferences', {
			body: { brand_mark: null, sfw_mode: null }
		});
	});
});
