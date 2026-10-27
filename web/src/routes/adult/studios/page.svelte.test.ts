// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { expectNoAxeViolations, renderWithQuery, stubGet } from '#lib/testing/harness.js';
import AdultStudiosPage from './+page.svelte';

type AdultStudio = components['schemas']['AdultStudio'];

const sampleStudios: AdultStudio[] = [
	{
		id: '018f0000-0000-7000-8000-000000000011',
		name: 'Studio Alpha',
		canonical_name: 'Studio Alpha',
		urls: ['https://example.com/s1'],
		is_favorite: true,
		ref_count: 3,
		created_at: '2026-10-27T10:00:00Z',
		updated_at: '2026-10-27T10:00:00Z'
	},
	{
		id: '018f0000-0000-7000-8000-000000000012',
		name: 'Studio Beta',
		canonical_name: 'Studio Beta',
		urls: [],
		is_favorite: false,
		ref_count: 1,
		created_at: '2026-10-27T10:00:00Z',
		updated_at: '2026-10-27T10:00:00Z'
	}
];

describe('adult studios (/adult/studios)', () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('positive: renders studios with name, favourite flag, ref count, and passes axe audit', async () => {
		stubGet({
			'/adult/studios': {
				items: sampleStudios
			}
		});

		const { container } = renderWithQuery(AdultStudiosPage);

		expect(
			await screen.findByRole('heading', { level: 1, name: m.adult_studios_title() })
		).toBeTruthy();
		expect(await screen.findByText('Studio Alpha')).toBeTruthy();
		expect(screen.getByText('Studio Beta')).toBeTruthy();
		expect(screen.getByText(m.adult_studios_favorite_badge())).toBeTruthy();
		expect(screen.getByText(m.adult_studios_ref_count({ count: 3 }))).toBeTruthy();
		expect(screen.getByText(m.adult_studios_ref_count({ count: 1 }))).toBeTruthy();

		// Filter toggle checkbox exists and has accessible label
		const checkbox = screen.getByRole('checkbox', {
			name: m.adult_studios_favorite_only()
		});
		expect(checkbox).toBeTruthy();
		await fireEvent.click(checkbox);

		await expectNoAxeViolations(container);
	});

	it('boundary: renders empty state when no studios exist and passes axe audit', async () => {
		stubGet({
			'/adult/studios': {
				items: []
			}
		});

		const { container } = renderWithQuery(AdultStudiosPage);

		expect(await screen.findByText(m.adult_studios_none())).toBeTruthy();

		await expectNoAxeViolations(container);
	});
});
