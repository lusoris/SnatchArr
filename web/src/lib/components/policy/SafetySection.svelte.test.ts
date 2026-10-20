// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { fireEvent, render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { samplePolicy, type SnatchPolicy } from '#lib/testing/harness.js';
import SafetySection from './SafetySection.svelte';

describe('SafetySection', () => {
	it('renders group legend and dry_run control', () => {
		const policy: SnatchPolicy = { ...samplePolicy, dry_run: false };
		render(SafetySection, { policy });

		expect(screen.getByText(m.policy_group_safety())).toBeTruthy();
		const checkbox = screen.getByLabelText(m.policy_dry_run()) as HTMLInputElement;
		expect(checkbox.checked).toBe(false);
	});

	it('toggles dry_run state on click', async () => {
		const policy: SnatchPolicy = { ...samplePolicy, dry_run: false };
		render(SafetySection, { policy });

		const checkbox = screen.getByLabelText(m.policy_dry_run()) as HTMLInputElement;
		await fireEvent.click(checkbox);
		expect(policy.dry_run).toBe(true);
	});
});
