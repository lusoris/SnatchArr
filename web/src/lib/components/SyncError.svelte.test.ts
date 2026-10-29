// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it } from 'vitest';
import SyncError from './SyncError.svelte';

const timeout = 'dial tcp 203.0.113.7:443: i/o timeout';

describe('SyncError', () => {
	it('hints at blocked egress on a dial timeout and keeps the raw error behind an expander', async () => {
		const { container } = render(SyncError, { error: timeout });
		expect(screen.getByTestId('egress-hint').textContent).toContain('network policy');
		const details = container.querySelector('details');
		expect(details?.open).toBe(false);
		expect(details?.querySelector('code')?.textContent).toBe(timeout);
		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('shows any other error as it is, through the formatter', () => {
		const { container } = render(SyncError, {
			error: 'dial tcp 203.0.113.7:443: connect: connection refused',
			format: (e: string) => `Sync error: ${e}`
		});
		expect(screen.queryByTestId('egress-hint')).toBeNull();
		expect(container.querySelector('details')).toBeNull();
		expect(container.textContent).toContain('Sync error: dial tcp 203.0.113.7:443: connect');
	});
});
