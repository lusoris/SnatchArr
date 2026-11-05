// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { QueryClient } from '@tanstack/svelte-query';
import { keys } from '#lib/query.js';
import { ProblemError } from '@sveltesentio/core';
import { screen, waitFor, within } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '#lib/api/client.js';
import type { components } from '#lib/api/schema.js';
import { m } from '#lib/paraglide/messages.js';
import { renderWithQuery, stubGet } from '#lib/testing/harness.js';
import ForeignSearchPreflight from './ForeignSearchPreflight.svelte';

type Preflight = components['schemas']['ForeignSearchPreflight'];
type Verdict = components['schemas']['ForeignSearchVerdict'];

const instanceId = 'radarr-1';
const endpoint = '/instances/{instanceId}/foreign-search/preflight';
const report: Preflight = {
	applies: true,
	languages: ['de', 'fr'],
	profiles: [
		{
			id: 1,
			name: 'HD-1080p',
			profile_language: { id: 4, name: 'German' },
			// Deliberately reversed: cells follow the response's languages, not verdict order.
			verdicts: [
				{ language: 'fr', verdict: 'skipped', reason: 'profile_language_mismatch' },
				{ language: 'de', verdict: 'allowed', reason: 'profile_language_match' }
			]
		},
		{
			id: 2,
			name: 'Original',
			profile_language: { id: -2, name: 'Original' },
			verdicts: [
				{ language: 'de', verdict: 'original_only', reason: 'profile_original' },
				{ language: 'fr', verdict: 'original_only', reason: 'profile_original' }
			]
		}
	]
};

function setup(data: Preflight | Error = report) {
	stubGet({ [endpoint]: data });
	return renderWithQuery(ForeignSearchPreflight, { instanceId });
}

describe('ForeignSearchPreflight', () => {
	afterEach(() => vi.restoreAllMocks());

	it('renders language columns with allowed, original-only and skipped explanations', async () => {
		setup();
		const table = await screen.findByRole('table', { name: m.foreign_search_preflight_title() });
		expect(
			within(table)
				.getAllByRole('columnheader')
				.map((cell) => cell.textContent)
		).toEqual([m.foreign_search_profile(), m.foreign_search_profile_language(), 'DE', 'FR']);
		const row = screen.getByRole('row', { name: /HD-1080p/ });
		const cells = within(row).getAllByRole('cell');
		expect(cells[0]?.textContent).toBe('German');
		expect(cells[1]?.textContent).toContain(m.foreign_search_verdict_allowed());
		expect(cells[1]?.textContent).toContain('Profile wants this language.');
		expect(cells[2]?.textContent).toContain(m.foreign_search_verdict_skipped());
		expect(cells[2]?.textContent).toContain(
			'Profile wants another language; foreign search skipped.'
		);
		const original = screen.getByRole('row', { name: /Original Original/ });
		expect(within(original).getAllByText(m.foreign_search_verdict_original_only())).toHaveLength(2);
		expect(within(original).getAllByText(m.foreign_search_reason_profile_original())).toHaveLength(
			2
		);
		expect(api.GET).toHaveBeenCalledWith(endpoint, { params: { path: { instanceId } } });
	});

	it.each<[Verdict['reason'], string]>([
		['profile_any', 'Profile accepts any language.'],
		['profile_language_match', 'Profile wants this language.'],
		[
			'profile_original',
			'Profile wants the original language: searched only for movies originally in this language.'
		],
		['profile_language_mismatch', 'Profile wants another language; foreign search skipped.'],
		['profile_language_missing', 'Radarr returned no profile language; foreign search skipped.'],
		['language_unmapped', 'Radarr has no matching language; foreign search skipped.'],
		['original_language_match', 'Movie is originally in this language.'],
		[
			'original_language_mismatch',
			'Movie is originally in another language; foreign search skipped.'
		]
	])('explains %s', async (reason, text) => {
		setup({
			...report,
			languages: ['de'],
			profiles: [
				{ ...report.profiles[0]!, verdicts: [{ language: 'de', verdict: 'skipped', reason }] }
			]
		});

		expect(await screen.findByText(text)).toBeTruthy();
	});

	it('renders nothing when the check does not apply, even with profiles', async () => {
		const { container } = setup({ ...report, applies: false });

		await waitFor(() => expect(container.childElementCount).toBe(0));
		expect(container.textContent).toBe('');
	});

	it('shows the empty line when there are no quality profiles', async () => {
		setup({ ...report, profiles: [] });

		expect(await screen.findByText(m.foreign_search_preflight_empty())).toBeTruthy();
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('shows a dash for a null profile language and explains the skipped search', async () => {
		setup({
			...report,
			languages: ['de'],
			profiles: [
				{
					...report.profiles[0]!,
					profile_language: null,
					verdicts: [{ language: 'de', verdict: 'skipped', reason: 'profile_language_missing' }]
				}
			]
		});

		expect(await screen.findByRole('cell', { name: '—' })).toBeTruthy();
		expect(screen.getByText(m.foreign_search_reason_profile_language_missing())).toBeTruthy();
	});

	it('announces a ProblemNote when the request fails', async () => {
		setup(new ProblemError({ type: 'about:blank', status: 503, detail: 'Radarr unavailable' }));

		expect((await screen.findByRole('alert')).textContent).toContain('Radarr unavailable');
		expect(screen.queryByRole('table')).toBeNull();
	});

	it('refetches a just-cached verdict on mount', async () => {
		const client = new QueryClient({
			defaultOptions: { queries: { retry: false, staleTime: 15_000 } }
		});
		client.setQueryData(keys.foreignSearchPreflight(instanceId), report);
		const get = stubGet({ [endpoint]: { ...report, profiles: [] } });
		renderWithQuery(ForeignSearchPreflight, { instanceId }, client);
		await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
		expect(await screen.findByText(m.foreign_search_preflight_empty())).toBeTruthy();
		client.clear();
	});

	it('announces loading while the request is pending', () => {
		vi.spyOn(api, 'GET').mockImplementation(() => new Promise(() => {}));
		renderWithQuery(ForeignSearchPreflight, { instanceId });

		expect(screen.getByText(m.loading()).getAttribute('aria-live')).toBe('polite');
	});
});
