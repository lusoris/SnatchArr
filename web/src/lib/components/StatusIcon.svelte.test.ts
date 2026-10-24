// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { describe, expect, it } from 'vitest';
import { axe } from 'vitest-axe';
import StatusIcon, { type StatusKind } from './StatusIcon.svelte';

const ALL_STATES: readonly { status: StatusKind; glyph: string; colorClass: string }[] = [
	{ status: 'ok', glyph: 'circle-check', colorClass: 'text-success' },
	{ status: 'warn', glyph: 'triangle-alert', colorClass: 'text-warning' },
	{ status: 'danger', glyph: 'circle-x', colorClass: 'text-danger' },
	{ status: 'info', glyph: 'info', colorClass: 'text-info' },
	{ status: 'running', glyph: 'loader-circle', colorClass: 'text-running' },
	{ status: 'idle', glyph: 'circle-dashed', colorClass: 'text-subtle' },
	{ status: 'disabled', glyph: 'circle-slash', colorClass: 'text-subtle' }
] as const;

describe('StatusIcon', () => {
	it.each(ALL_STATES)(
		'positive: $status renders distinct glyph ($glyph) and accessible name',
		async ({ status, glyph, colorClass }) => {
			const label = `Status: ${status}`;
			const { container } = render(StatusIcon, {
				props: { status, label }
			});

			const img = screen.getByRole('img', { name: label });
			expect(img).toBeTruthy();
			expect(img.getAttribute('data-status')).toBe(status);
			expect(img.getAttribute('data-glyph')).toBe(glyph);
			expect(img.classList.contains(colorClass)).toBe(true);

			const svg = img.querySelector('svg');
			expect(svg).toBeTruthy();
			expect(svg?.getAttribute('aria-hidden')).toBe('true');

			const srSpan = img.querySelector('.sr-only');
			expect(srSpan).toBeTruthy();
			expect(srSpan?.textContent).toBe(label);

			const results = await axe(container, {
				runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
			});
			expect(results.violations).toEqual([]);
		}
	);

	it('positive: all 7 states have unique glyph attributes', () => {
		const glyphs = new Set(ALL_STATES.map((s) => s.glyph));
		expect(glyphs.size).toBe(7);
	});

	it('negative: non-running states carry no animation class or attribute', () => {
		const nonRunning = ALL_STATES.filter((s) => s.status !== 'running');
		for (const { status } of nonRunning) {
			const { container } = render(StatusIcon, {
				props: { status, label: `Status: ${status}` }
			});
			const svg = container.querySelector('svg');
			expect(svg?.classList.contains('status-icon-running')).toBe(false);
			expect(container.innerHTML).not.toContain('animate-');
		}
	});

	it('positive: running state carries status-icon-running animation class', () => {
		const { container } = render(StatusIcon, {
			props: { status: 'running', label: 'Executing run' }
		});
		const svg = container.querySelector('svg');
		expect(svg?.classList.contains('status-icon-running')).toBe(true);
	});

	it('boundary: showLabel true displays visible text and hides icon from a11y tree', async () => {
		const label = 'All systems healthy';
		const { container } = render(StatusIcon, {
			props: { status: 'ok', label, showLabel: true }
		});

		// Accessible text is visible in the document
		expect(screen.getByText(label)).toBeTruthy();

		// Root is not an img role; icon is purely decorative
		expect(screen.queryByRole('img')).toBeNull();

		const svg = container.querySelector('svg');
		expect(svg).toBeTruthy();
		expect(svg?.getAttribute('aria-hidden')).toBe('true');

		// No .sr-only class used when showLabel is true
		expect(container.querySelector('.sr-only')).toBeNull();

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});
});
