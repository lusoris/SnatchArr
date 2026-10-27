// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it } from 'vitest';
import KindSelect from './KindSelect.svelte';

const select = (): HTMLSelectElement => screen.getByLabelText<HTMLSelectElement>('Kind');

describe('KindSelect', () => {
	it('offers every kind and labels Readarr as legacy and Stash as Stash', () => {
		render(KindSelect, { value: 'sonarr' });
		const options = screen.getAllByRole<HTMLOptionElement>('option');
		expect(options.map((o) => o.value)).toEqual([
			'sonarr',
			'radarr',
			'lidarr',
			'readarr',
			'whisparr_v2',
			'whisparr_v3',
			'stash'
		]);
		expect(options.map((o) => o.textContent)).toEqual([
			'Sonarr',
			'Radarr',
			'Lidarr',
			'Readarr (legacy)',
			'Whisparr v2',
			'Whisparr v3',
			'Stash'
		]);
	});

	it('shows no note for a kind that is not legacy', () => {
		const { container } = render(KindSelect, { value: 'sonarr' });
		expect(container.querySelector('#kind-hint')).toBeNull();
		expect(select().hasAttribute('aria-describedby')).toBe(false);
	});

	it('describes the control with the note once Readarr is chosen, and drops it again', async () => {
		const { container } = render(KindSelect, { value: 'sonarr' });
		await fireEvent.change(select(), { target: { value: 'readarr' } });
		expect(container.querySelector('#kind-hint')?.textContent).toContain('retired');
		expect(select().getAttribute('aria-describedby')).toBe('kind-hint');
		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);

		await fireEvent.change(select(), { target: { value: 'whisparr_v3' } });
		expect(container.querySelector('#kind-hint')).toBeNull();
		expect(select().hasAttribute('aria-describedby')).toBe(false);
	});
});
