// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { fireEvent, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { axe } from 'vitest-axe';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import AdultPerformersPage from './+page.svelte';

type AdultPerformer = components['schemas']['AdultPerformer'];

const samplePerformers: AdultPerformer[] = [
	{
		id: '018f0000-0000-7000-8000-000000000001',
		name: 'Performer One',
		canonical_name: 'Performer One',
		disambiguation: 'Series A',
		urls: ['https://example.com/p1'],
		is_favorite: true,
		ref_count: 2,
		created_at: '2026-10-27T10:00:00Z',
		updated_at: '2026-10-27T10:00:00Z'
	},
	{
		id: '018f0000-0000-7000-8000-000000000002',
		name: 'Performer Two',
		canonical_name: 'Performer Two',
		urls: [],
		is_favorite: false,
		ref_count: 0,
		created_at: '2026-10-27T10:00:00Z',
		updated_at: '2026-10-27T10:00:00Z'
	}
];

describe('adult performers (/adult/performers)', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('positive: renders performers with name, favourite flag, ref count, and passes axe audit', async () => {
		stubGet({
			'/adult/performers': {
				items: samplePerformers
			}
		});

		const { container } = renderWithQuery(AdultPerformersPage);

		expect(
			await screen.findByRole('heading', { level: 1, name: m.adult_performers_title() })
		).toBeTruthy();
		expect(await screen.findByText('Performer One')).toBeTruthy();
		expect(screen.getByText('(Series A)')).toBeTruthy();
		expect(screen.getByText('Performer Two')).toBeTruthy();
		expect(screen.getByText(m.adult_performers_favorite_badge())).toBeTruthy();
		expect(screen.getByText(m.adult_performers_ref_count({ count: 2 }))).toBeTruthy();
		expect(screen.getByText(m.adult_performers_ref_count({ count: 0 }))).toBeTruthy();

		// Filter toggle checkbox exists and has accessible label
		const checkbox = screen.getByRole('checkbox', {
			name: m.adult_performers_favorite_only()
		});
		expect(checkbox).toBeTruthy();
		await fireEvent.click(checkbox);

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});

	it('boundary: renders empty state when no performers exist and passes axe audit', async () => {
		stubGet({
			'/adult/performers': {
				items: []
			}
		});

		const { container } = renderWithQuery(AdultPerformersPage);

		expect(await screen.findByText(m.adult_performers_none())).toBeTruthy();

		const a11y = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(a11y.violations).toEqual([]);
	});
});
