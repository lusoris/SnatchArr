// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { samplePolicy, type SnatchPolicy } from '#lib/testing/harness.js';
import TimingSection from './TimingSection.svelte';

describe('TimingSection', () => {
	it('renders group legend and all timing controls', () => {
		const policy: SnatchPolicy = { ...samplePolicy, cycle_interval_s: 900 };
		render(TimingSection, { policy });

		expect(screen.getByText(m.policy_group_timing())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_cycle_interval())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_processed_ttl())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_recent_grab_window())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_afterglow_max())).toBeTruthy();

		const intervalInput = screen.getByLabelText(m.policy_cycle_interval()) as HTMLInputElement;
		// 900 seconds should display as 15 minutes
		expect(intervalInput.value).toBe('15');
	});

	it('converts entered minutes to seconds in the policy object', async () => {
		const policy: SnatchPolicy = { ...samplePolicy, cycle_interval_s: 900 };
		render(TimingSection, { policy });

		const intervalInput = screen.getByLabelText(m.policy_cycle_interval()) as HTMLInputElement;
		expect(intervalInput.min).toBe('1');
		expect(intervalInput.max).toBe('10080');

		await fireEvent.input(intervalInput, { target: { value: '30' } });
		// 30 minutes = 1800 seconds
		expect(policy.cycle_interval_s).toBe(1800);
	});

	it('updates ttl, recent grab window, and afterglow max adhering to bounds', async () => {
		const policy: SnatchPolicy = { ...samplePolicy };
		render(TimingSection, { policy });

		const ttlInput = screen.getByLabelText(m.policy_processed_ttl()) as HTMLInputElement;
		expect(ttlInput.min).toBe('1');
		expect(ttlInput.max).toBe('8760');
		await fireEvent.input(ttlInput, { target: { value: '48' } });
		expect(policy.processed_ttl_h).toBe(48);

		const grabInput = screen.getByLabelText(m.policy_recent_grab_window()) as HTMLInputElement;
		expect(grabInput.min).toBe('0');
		expect(grabInput.max).toBe('720');
		await fireEvent.input(grabInput, { target: { value: '24' } });
		expect(policy.recent_grab_window_h).toBe(24);

		const afterglowInput = screen.getByLabelText(m.policy_afterglow_max()) as HTMLInputElement;
		expect(afterglowInput.min).toBe('1');
		expect(afterglowInput.max).toBe('8760');
		await fireEvent.input(afterglowInput, { target: { value: '168' } });
		expect(policy.afterglow_max_h).toBe(168);
	});
});
