// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { samplePolicy, type SnatchPolicy } from '#lib/testing/harness.js';
import SearchSection from './SearchSection.svelte';

describe('SearchSection', () => {
	it('renders group legend and all general search controls', () => {
		const policy: SnatchPolicy = { ...samplePolicy };
		render(SearchSection, { policy, kind: 'sonarr' });

		expect(screen.getByText(m.policy_group_search())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_missing_per_cycle())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_upgrade_per_cycle())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_release_delay())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_selection())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_monitored_only())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_skip_future())).toBeTruthy();
		expect(screen.queryByLabelText(m.policy_skip_unavailable())).toBeNull();
	});

	it('renders skip_unavailable_movies only when kind is radarr', () => {
		const policy: SnatchPolicy = { ...samplePolicy };
		const { unmount } = render(SearchSection, { policy, kind: 'radarr' });

		expect(screen.getByLabelText(m.policy_skip_unavailable())).toBeTruthy();
		unmount();

		render(SearchSection, { policy, kind: 'lidarr' });
		expect(screen.queryByLabelText(m.policy_skip_unavailable())).toBeNull();
	});

	it('updates draft fields on user interaction and adheres to bounds', async () => {
		const policy: SnatchPolicy = { ...samplePolicy };
		render(SearchSection, { policy, kind: 'radarr' });

		const missingInput = screen.getByLabelText(m.policy_missing_per_cycle()) as HTMLInputElement;
		expect(missingInput.min).toBe('0');
		expect(missingInput.max).toBe('100');
		await fireEvent.input(missingInput, { target: { value: '42' } });
		expect(policy.missing_per_cycle).toBe(42);

		const releaseDelayInput = screen.getByLabelText(m.policy_release_delay()) as HTMLInputElement;
		expect(releaseDelayInput.min).toBe('0');
		expect(releaseDelayInput.max).toBe('8760');
		await fireEvent.input(releaseDelayInput, { target: { value: '72' } });
		expect(policy.release_delay_h).toBe(72);

		const selectionSelect = screen.getByLabelText(m.policy_selection()) as HTMLSelectElement;
		await fireEvent.change(selectionSelect, { target: { value: 'recent' } });
		expect(policy.selection).toBe('recent');

		const monitoredCheckbox = screen.getByLabelText(m.policy_monitored_only()) as HTMLInputElement;
		await fireEvent.click(monitoredCheckbox);
		expect(policy.monitored_only).toBe(false);
	});
});
