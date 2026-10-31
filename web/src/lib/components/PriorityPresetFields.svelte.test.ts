// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, within } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import { ARR_KINDS } from '#lib/kinds.js';
import PriorityPresetFields from './PriorityPresetFields.svelte';

describe('PriorityPresetFields', () => {
	it('positive: names the search-priority readarr row "Readarr (legacy)" exactly', () => {
		const { container } = render(PriorityPresetFields, {
			searchPreset: { readarr: 30 },
			downloadPreset: {},
			onReset: () => {}
		});
		const row = container.querySelector(
			'[data-field="search_priority_preset"] li[data-kind="readarr"]'
		);
		expect(row).toBeInstanceOf(HTMLLIElement);
		expect(within(row as HTMLElement).getByText('Readarr (legacy)', { exact: true })).toBeTruthy();
		expect(container.querySelector('#search-priority-readarr')?.getAttribute('data-rank')).toBe(
			'30'
		);
	});

	it('negative: no label contains "priority preset ("', () => {
		const fullPreset = {
			sonarr: 10,
			radarr: 10,
			lidarr: 20,
			readarr: 30,
			whisparr_v2: 40,
			whisparr_v3: 40
		};
		const { container } = render(PriorityPresetFields, {
			searchPreset: fullPreset,
			downloadPreset: fullPreset,
			onReset: () => {}
		});
		const labels = Array.from(container.querySelectorAll('label'));
		expect(labels.length).toBeGreaterThan(0);
		for (const label of labels) {
			expect(label.textContent).not.toContain('priority preset (');
		}
	});

	it('boundary: every kind on one rank renders one row per kind, all after the first tied', () => {
		const sameRank = Object.fromEntries(ARR_KINDS.map((kind) => [kind, 10]));
		const { container } = render(PriorityPresetFields, {
			searchPreset: { ...sameRank },
			downloadPreset: { ...sameRank },
			onReset: () => {}
		});
		const search = container.querySelector('[data-field="search_priority_preset"]') as HTMLElement;
		const rows = Array.from(search.querySelectorAll('li[data-kind]'));
		expect(rows).toHaveLength(ARR_KINDS.length);
		const ties = rows.map(
			(row) => row.querySelector<HTMLInputElement>('input[type="checkbox"]')?.checked ?? false
		);
		expect(ties).toEqual(ARR_KINDS.map((_, index) => index > 0));
		expect(container.querySelectorAll('[data-rank="10"]')).toHaveLength(2 * ARR_KINDS.length);
	});
});
