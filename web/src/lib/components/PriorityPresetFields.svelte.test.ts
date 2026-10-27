// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import PriorityPresetFields from './PriorityPresetFields.svelte';

describe('PriorityPresetFields', () => {
	it('positive: labels search-priority-readarr input as "Readarr (legacy)" exactly', () => {
		render(PriorityPresetFields, {
			searchPreset: { readarr: 30 },
			downloadPreset: {},
			onReset: () => {}
		});
		const input = screen.getByLabelText('Readarr (legacy)', { exact: true });
		expect(input).toBeInstanceOf(HTMLInputElement);
		expect(input.id).toBe('search-priority-readarr');
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

	it('boundary: kind absent from preset renders no input', () => {
		const { container } = render(PriorityPresetFields, {
			searchPreset: { sonarr: 10 },
			downloadPreset: { radarr: 10 },
			onReset: () => {}
		});
		expect(container.querySelector('#search-priority-sonarr')).toBeInstanceOf(HTMLInputElement);
		expect(container.querySelector('#download-priority-sonarr')).toBeNull();
		expect(container.querySelector('#search-priority-radarr')).toBeNull();
		expect(container.querySelector('#download-priority-radarr')).toBeInstanceOf(HTMLInputElement);
		expect(container.querySelector('#search-priority-readarr')).toBeNull();
		expect(container.querySelector('#download-priority-readarr')).toBeNull();
	});
});
