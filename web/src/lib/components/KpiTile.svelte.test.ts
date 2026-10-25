// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

import { render, screen } from '@testing-library/svelte';
import { describe, expect, it } from 'vitest';
import { ProblemError } from '@sveltesentio/core';
import KpiTile from './KpiTile.svelte';

describe('KpiTile component', () => {
	it('positive: renders label, heroValue, subtitle, and status icon', () => {
		render(KpiTile, {
			label: 'Fleet health',
			heroValue: '5 / 5',
			subtitle: 'All instances healthy',
			status: 'ok',
			statusLabel: 'Healthy',
			accentClass: 'text-success'
		});

		expect(screen.getByText('Fleet health')).toBeTruthy();
		expect(screen.getByText('5 / 5')).toBeTruthy();
		expect(screen.getByText('All instances healthy')).toBeTruthy();
		expect(screen.getByRole('img', { name: 'Healthy' })).toBeTruthy();
	});

	it('negative: renders ProblemNote when error is provided', () => {
		const error = new ProblemError({
			type: 'about:blank',
			status: 503,
			title: 'Unavailable',
			detail: 'Database unreachable'
		});

		render(KpiTile, {
			label: 'Fleet health',
			heroValue: '0',
			error
		});

		expect(screen.getByRole('alert')).toBeTruthy();
		expect(screen.getByText(/Database unreachable/)).toBeTruthy();
		expect(screen.queryByText('Fleet health')).toBeNull();
	});

	it('boundary: renders skeleton loader when loading is true', () => {
		render(KpiTile, {
			label: 'Fleet health',
			heroValue: '0',
			loading: true
		});

		const skeleton = screen.getByTestId('skeleton');
		expect(skeleton).toBeTruthy();
		expect(skeleton.getAttribute('aria-busy')).toBe('true');
		expect(screen.queryByText('Fleet health')).toBeNull();
	});

	it('boundary: renders cleanly without subtitle or status icon', () => {
		render(KpiTile, {
			label: 'Simple KPI',
			heroValue: '42'
		});

		expect(screen.getByText('Simple KPI')).toBeTruthy();
		expect(screen.getByText('42')).toBeTruthy();
		expect(screen.queryByRole('img')).toBeNull();
	});
});
