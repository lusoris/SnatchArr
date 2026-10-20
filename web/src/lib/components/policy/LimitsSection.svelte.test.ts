// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { samplePolicy, type SnatchPolicy } from '#lib/testing/harness.js';
import LimitsSection from './LimitsSection.svelte';

describe('LimitsSection', () => {
	it('renders group legend and all indexer limit controls', () => {
		const policy: SnatchPolicy = { ...samplePolicy };
		render(LimitsSection, { policy });

		expect(screen.getByText(m.policy_group_limits())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_hourly_cap())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_max_queue_size())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_page_size())).toBeTruthy();
		expect(screen.getByLabelText(m.policy_await_command())).toBeTruthy();
	});

	it('updates fields adhering to min and max boundaries', async () => {
		const policy: SnatchPolicy = { ...samplePolicy };
		render(LimitsSection, { policy });

		const capInput = screen.getByLabelText(m.policy_hourly_cap()) as HTMLInputElement;
		expect(capInput.min).toBe('1');
		expect(capInput.max).toBe('500');
		await fireEvent.input(capInput, { target: { value: '75' } });
		expect(policy.hourly_cap).toBe(75);

		const queueInput = screen.getByLabelText(m.policy_max_queue_size()) as HTMLInputElement;
		expect(queueInput.min).toBe('-1');
		expect(queueInput.max).toBe('100000');
		// -1 signifies no limit
		await fireEvent.input(queueInput, { target: { value: '-1' } });
		expect(policy.max_queue_size).toBe(-1);

		const pageInput = screen.getByLabelText(m.policy_page_size()) as HTMLInputElement;
		expect(pageInput.min).toBe('10');
		expect(pageInput.max).toBe('1000');
		await fireEvent.input(pageInput, { target: { value: '250' } });
		expect(policy.page_size).toBe(250);

		const awaitCheckbox = screen.getByLabelText(m.policy_await_command()) as HTMLInputElement;
		await fireEvent.click(awaitCheckbox);
		expect(policy.await_command).toBe(true);
	});
});
