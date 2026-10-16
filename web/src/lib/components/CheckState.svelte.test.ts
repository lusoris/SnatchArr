// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it } from 'vitest';
import { m } from '#lib/paraglide/messages.js';
import { failedInstance, forkInstance, untestedInstance } from '#lib/testing/harness.js';
import CheckState from './CheckState.svelte';

describe('CheckState', () => {
	it('renders never tested state when no check has been performed', async () => {
		const { container } = render(CheckState, { instance: untestedInstance });

		expect(screen.getByText(m.instance_never_tested())).toBeTruthy();
		expect(screen.queryByRole('status')).toBeNull();

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('renders passed connection test with formatted date and version', async () => {
		const { container } = render(CheckState, { instance: forkInstance });

		const lastCheck = new RegExp(m.instance_last_check());
		const text = screen.getByText(lastCheck).textContent;
		expect(text).toContain('0.4.21.182');
		expect(screen.queryByRole('status')).toBeNull();

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('renders passed connection test without version when not present', async () => {
		const instWithoutVersion = {
			last_check_at: '2026-09-01T12:00:00Z',
			last_seen_version: undefined
		};
		const { container } = render(CheckState, { instance: instWithoutVersion });

		const lastCheck = new RegExp(m.instance_last_check());
		const text = screen.getByText(lastCheck).textContent;
		expect(text).not.toContain('·');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('renders failed connection test with error and role="status"', async () => {
		const { container } = render(CheckState, { instance: failedInstance });

		const status = screen.getByRole('status');
		expect(status.textContent).toContain('Connection refused (ECONNREFUSED)');
		expect(status.textContent).toContain(m.instance_check_failed());
		expect(container.querySelector('svg[aria-hidden="true"]')).toBeTruthy();

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});
});
