// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { WCAG_22_AA_TAGS } from '@sveltesentio/testing/a11y';
import { axe } from 'vitest-axe';
import { describe, expect, it } from 'vitest';
import StaminaGauge from './StaminaGauge.svelte';

describe('StaminaGauge', () => {
	it('exposes an accessible meter and the level', async () => {
		const { container } = render(StaminaGauge, { label: 'FakeTV', used: 16, cap: 20 });
		const meter = screen.getByRole('meter', { name: 'FakeTV' });
		expect(meter.getAttribute('aria-valuenow')).toBe('16');
		expect(meter.getAttribute('aria-valuemax')).toBe('20');
		expect(container.querySelector('.stamina')?.getAttribute('data-level')).toBe('tired');
		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('never overflows the bar', () => {
		const { container } = render(StaminaGauge, { label: 'x', used: 99, cap: 20 });
		expect(container.querySelector<HTMLElement>('.fill')?.style.width).toBe('100%');
		expect(container.querySelector('.stamina')?.getAttribute('data-level')).toBe('spent');
	});

	it('carries data-label-value and gap-x-3 on the header row', () => {
		const { container } = render(StaminaGauge, { label: 'FakeTV', used: 16, cap: 20 });
		const header = container.querySelector('[data-label-value]');
		expect(header).not.toBeNull();
		expect(header?.classList.contains('gap-x-3')).toBe(true);
	});

	it('positive: hideLabel sets sr-only class on label while keeping aria-label', async () => {
		const { container } = render(StaminaGauge, {
			label: 'FakeTV',
			used: 16,
			cap: 20,
			hideLabel: true
		});
		const labelSpan = container.querySelector('[data-label-value] span');
		expect(labelSpan).not.toBeNull();
		expect(labelSpan?.textContent).toBe('FakeTV');
		expect(labelSpan?.classList.contains('sr-only')).toBe(true);
		expect(labelSpan?.classList.contains('font-medium')).toBe(false);

		const meter = screen.getByRole('meter', { name: 'FakeTV' });
		expect(meter.getAttribute('aria-label')).toBe('FakeTV');

		const results = await axe(container, {
			runOnly: { type: 'tag', values: [...WCAG_22_AA_TAGS] }
		});
		expect(results.violations).toEqual([]);
	});

	it('negative: default leaves label visible without sr-only class', () => {
		const { container } = render(StaminaGauge, { label: 'FakeTV', used: 16, cap: 20 });
		const labelSpan = container.querySelector('[data-label-value] span');
		expect(labelSpan).not.toBeNull();
		expect(labelSpan?.textContent).toBe('FakeTV');
		expect(labelSpan?.classList.contains('sr-only')).toBe(false);
		expect(labelSpan?.classList.contains('font-medium')).toBe(true);
	});

	it('boundary: handles empty label string gracefully', () => {
		const { container } = render(StaminaGauge, { label: '', used: 0, cap: 10 });
		const labelSpan = container.querySelector('[data-label-value] span');
		expect(labelSpan).not.toBeNull();
		expect(labelSpan?.textContent).toBe('');
		const meter = container.querySelector('[role="meter"]');
		expect(meter?.getAttribute('aria-label')).toBe('');
	});
});
