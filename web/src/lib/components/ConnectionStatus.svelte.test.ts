// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { describe, expect, it } from 'vitest';
import { axe } from 'vitest-axe';
import { m } from '#lib/paraglide/messages.js';
import ConnectionStatus from './ConnectionStatus.svelte';

describe('ConnectionStatus', () => {
	it('positive: renders connected status with brand indicator and role=status', async () => {
		const { container } = render(ConnectionStatus, { props: { connected: true } });

		const status = screen.getByRole('status');
		expect(status).toBeTruthy();
		expect(status.textContent).toContain(m.live_connected());
		expect(status.textContent).not.toContain(m.live_disconnected());

		const dot = container.querySelector('span[aria-hidden="true"]');
		expect(dot).toBeTruthy();
		expect(dot?.classList.contains('bg-brand')).toBe(true);
		expect(dot?.classList.contains('bg-warning')).toBe(false);

		expect(container.innerHTML).not.toContain('bg-green-600');
		expect(container.innerHTML).not.toContain('bg-amber-500');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('negative: renders disconnected status with warning indicator and role=status', async () => {
		const { container } = render(ConnectionStatus, { props: { connected: false } });

		const status = screen.getByRole('status');
		expect(status).toBeTruthy();
		expect(status.textContent).toContain(m.live_disconnected());
		expect(status.textContent).not.toContain(m.live_connected());

		const dot = container.querySelector('span[aria-hidden="true"]');
		expect(dot).toBeTruthy();
		expect(dot?.classList.contains('bg-warning')).toBe(true);
		expect(dot?.classList.contains('bg-brand')).toBe(false);

		expect(container.innerHTML).not.toContain('bg-green-600');
		expect(container.innerHTML).not.toContain('bg-amber-500');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('boundary: isolates design tokens and avoids raw palette classes across configurations', () => {
		const { container } = render(ConnectionStatus, { props: { connected: false } });

		expect(container.querySelector('[role="status"]')).toBeTruthy();
		expect(container.innerHTML).not.toMatch(/bg-(green|amber|red|blue)-\d+/);
	});
});
